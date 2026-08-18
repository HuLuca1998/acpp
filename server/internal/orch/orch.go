package orch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"gorm.io/gorm"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
	"acpp/server/internal/service"
	"acpp/server/internal/stream"
	"acpp/server/internal/transcript"
)

// Service 是编排功能的业务核心（adr-006）：主会话挂载系统 MCP、
// 经 spawn_agent 雇佣角色子会话并同步等结果。与 ChatService 刻意分离
// ——隔离契约要求编排整体可删而不影响普通会话；broker/重建器/acp 层
// 这些无会话业务语义的构件按包内共享复用。
type Service struct {
	db          *gorm.DB
	roles       *RoleService
	manager     *acp.Manager
	transcripts *transcript.Store
	skillUsage  *service.SkillUsageService

	// dataDir 派生编排专属的 codex home（<dataDir>/orch/...）；
	// skillpackDir 让编排会话保持与普通会话相同的技能隔离基座。
	dataDir      string
	skillpackDir string
	// mcpBase 是本机 MCP 端点前缀（http://127.0.0.1:<port>/api/mcp/），
	// agent 子进程从本机回连，host 固定 127.0.0.1。
	mcpBase string
	// sources 由装配层注入，把 @ 数据库引用展开成 prompt 内容。可为 nil。
	// 编排会话与普通会话在这件事上不该有差别（编排是升级不是降级）。
	sources service.DataSources

	mu sync.Mutex
	// brokers 按流 key 索引：主会话 orchKey(id)、任务 orchTaskKey(id)。
	brokers map[string]*stream.Broker
	// runningTasks 记录每条主会话在跑的任务数（并发护栏）。
	runningTasks map[uint]int
	// stopped 标记被急停的主会话：在途 spawn 返回错误，不再接受新 spawn。
	stopped map[uint]bool
}

// SetDataSources 装上数据源能力面（@ 数据库引用）。装配期调用一次。
func (s *Service) SetDataSources(d service.DataSources) { s.sources = d }

// 编排护栏的默认值：并发任务数是「一个人的注意力」尺度而不是资源上限。
// 任务不设硬超时（用户拍板：长程任务跑几个小时是正常使用方式，与
// ACP_TURN_TIMEOUT 默认不限时同哲学）——失控的兜底是界面上的急停。
const (
	orchMaxConcurrentTasks = 4
	// mcpToolTimeoutMS 是注给 runtime 侧的工具调用超时（约 24 小时 ≈
	// 实际不限）。claude 的 MCP_TOOL_TIMEOUT env 有一层 5 分钟 clamp
	// 兜不住长任务，真正的入口是 per-server timeout 字段（options 注入）。
	mcpToolTimeoutMS = 86_400_000
)

func NewService(db *gorm.DB, roles *RoleService, manager *acp.Manager, transcripts *transcript.Store, skillUsage *service.SkillUsageService, dataDir, skillpackDir, addr string) *Service {
	return &Service{
		db:           db,
		roles:        roles,
		manager:      manager,
		transcripts:  transcripts,
		skillUsage:   skillUsage,
		dataDir:      dataDir,
		skillpackDir: skillpackDir,
		mcpBase:      mcpBaseURL(addr),
		brokers:      make(map[string]*stream.Broker),
		runningTasks: make(map[uint]int),
		stopped:      make(map[uint]bool),
	}
}

// mcpBaseURL 从监听地址推导 agent 回连的 MCP 前缀：子进程在本机，
// host 一律用回环地址，只取端口。
func mcpBaseURL(addr string) string {
	port := "48080"
	if i := strings.LastIndex(addr, ":"); i >= 0 && i < len(addr)-1 {
		port = addr[i+1:]
	}
	return "http://127.0.0.1:" + port + "/api/mcp/"
}

// orchKey / orchTaskKey 是 acp.Manager 与转录的 key 空间，与普通会话的
// 纯数字 key 天然隔离。
func orchKey(id uint) string     { return fmt.Sprintf("orch-%d", id) }
func orchTaskKey(id uint) string { return fmt.Sprintf("orchtask-%d", id) }

// SessionInput 是创建编排会话的入参。
type SessionInput struct {
	AgentID uint   `json:"agentId"`
	Cwd     string `json:"cwd"`
	Title   string `json:"title"`
}

// Create 新建编排主会话：生成专属 MCP token，进程与握手推迟到首次发送
// （与普通会话的懒连接一致）。
func (s *Service) Create(ctx context.Context, in SessionInput) (*model.OrchSession, error) {
	if in.AgentID == 0 {
		return nil, fmt.Errorf("%w: agentId is required", service.ErrInvalid)
	}
	var agent model.Agent
	if err := s.db.WithContext(ctx).First(&agent, in.AgentID).Error; err != nil {
		return nil, fmt.Errorf("%w: agent %d", service.ErrInvalid, in.AgentID)
	}

	token := make([]byte, 24)
	if _, err := rand.Read(token); err != nil {
		return nil, fmt.Errorf("generate mcp token: %w", err)
	}

	orch := model.OrchSession{
		AgentID:  in.AgentID,
		Cwd:      in.Cwd,
		Title:    strings.TrimSpace(in.Title),
		State:    model.SessionIdle,
		MCPToken: hex.EncodeToString(token),
	}
	if err := s.db.WithContext(ctx).Create(&orch).Error; err != nil {
		return nil, fmt.Errorf("create orch session: %w", err)
	}
	return &orch, nil
}

// List 按页取编排会话。orderBy 由 handler 从白名单拼好传进来（那是不能用
// 占位符的位置），空则按最近更新倒序。
func (s *Service) List(ctx context.Context, page, pageSize int, orderBy string) ([]model.OrchSession, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	var total int64
	if err := s.db.WithContext(ctx).Model(&model.OrchSession{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count orch sessions: %w", err)
	}
	if orderBy == "" {
		orderBy = "updated_at desc"
	}
	var items []model.OrchSession
	err := s.db.WithContext(ctx).Order(orderBy).
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	if err != nil {
		return nil, 0, fmt.Errorf("list orch sessions: %w", err)
	}
	return items, total, nil
}

func (s *Service) Get(ctx context.Context, id uint) (*model.OrchSession, error) {
	var orch model.OrchSession
	err := s.db.WithContext(ctx).First(&orch, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("orch session %d: %w", id, service.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get orch session %d: %w", id, err)
	}
	return &orch, nil
}

// byMCPToken 是 MCP 端点的身份解析：token 即凭证。
func (s *Service) byMCPToken(ctx context.Context, token string) (*model.OrchSession, error) {
	if token == "" {
		return nil, fmt.Errorf("orch token: %w", service.ErrNotFound)
	}
	var orch model.OrchSession
	err := s.db.WithContext(ctx).Where("mcp_token = ?", token).First(&orch).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("orch token: %w", service.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get orch session by token: %w", err)
	}
	return &orch, nil
}

// Delete 删除编排会话与其全部任务：先急停在跑的一切，再删记录与转录。
func (s *Service) Delete(ctx context.Context, id uint) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	s.Stop(ctx, id)

	var tasks []model.OrchTask
	if err := s.db.WithContext(ctx).Where("orch_session_id = ?", id).Find(&tasks).Error; err == nil {
		for _, t := range tasks {
			_ = s.manager.Close(orchTaskKey(t.ID))
			s.transcripts.Close(orchTaskKey(t.ID))
			s.dropBroker(orchTaskKey(t.ID))
		}
	}
	_ = s.manager.Close(orchKey(id))
	s.transcripts.Close(orchKey(id))
	s.dropBroker(orchKey(id))

	if err := s.db.WithContext(ctx).Where("orch_session_id = ?", id).Delete(&model.OrchTask{}).Error; err != nil {
		return fmt.Errorf("delete orch tasks: %w", err)
	}
	if err := s.db.WithContext(ctx).Delete(&model.OrchSession{}, id).Error; err != nil {
		return fmt.Errorf("delete orch session %d: %w", id, err)
	}

	s.mu.Lock()
	delete(s.runningTasks, id)
	delete(s.stopped, id)
	s.mu.Unlock()
	return nil
}

// Stop 急停：中止主会话 turn 与全部在跑任务的子会话 turn。在途的
// spawn 调用会以错误返回给主会话（若其 turn 还活着）。
func (s *Service) Stop(ctx context.Context, id uint) {
	s.mu.Lock()
	s.stopped[id] = true
	s.mu.Unlock()

	_ = s.manager.Cancel(orchKey(id))
	var tasks []model.OrchTask
	if err := s.db.WithContext(ctx).
		Where("orch_session_id = ? AND state = ?", id, model.OrchTaskRunning).
		Find(&tasks).Error; err == nil {
		for _, t := range tasks {
			_ = s.manager.Cancel(orchTaskKey(t.ID))
		}
	}
}

// clearStopped 在新一轮用户消息时解除急停标记——急停针对「当前这摊事」。
func (s *Service) clearStopped(id uint) {
	s.mu.Lock()
	delete(s.stopped, id)
	s.mu.Unlock()
}

func (s *Service) isStopped(id uint) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped[id]
}

// brokerFor 按流 key 取/建广播器（主会话与任务共用一套机制）。
func (s *Service) brokerFor(key string) *stream.Broker {
	s.mu.Lock()
	defer s.mu.Unlock()
	if br, ok := s.brokers[key]; ok {
		return br
	}
	br := stream.NewBroker()
	s.brokers[key] = br
	return br
}

func (s *Service) dropBroker(key string) {
	s.mu.Lock()
	delete(s.brokers, key)
	s.mu.Unlock()
}

// Subscribe 订阅主会话事件流（含 task_update）。
func (s *Service) Subscribe(id uint) (<-chan stream.Event, func()) {
	return s.brokerFor(orchKey(id)).Subscribe()
}

// SubscribeTask 订阅一条任务子会话的事件流（拖出面板时用）。
func (s *Service) SubscribeTask(taskID uint) (<-chan stream.Event, func()) {
	return s.brokerFor(orchTaskKey(taskID)).Subscribe()
}

// Tasks 列出一条编排会话的全部任务，老的在前（派发流顺序）。
func (s *Service) Tasks(ctx context.Context, orchID uint) ([]model.OrchTask, error) {
	var tasks []model.OrchTask
	err := s.db.WithContext(ctx).Where("orch_session_id = ?", orchID).
		Order("id asc").Find(&tasks).Error
	if err != nil {
		return nil, fmt.Errorf("list orch tasks: %w", err)
	}
	return tasks, nil
}

func (s *Service) task(ctx context.Context, taskID uint) (*model.OrchTask, error) {
	var task model.OrchTask
	err := s.db.WithContext(ctx).First(&task, taskID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("orch task %d: %w", taskID, service.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get orch task %d: %w", taskID, err)
	}
	return &task, nil
}

// TranscriptPath 返回主会话转录文件路径（logs 面板与原始日志下载）。
func (s *Service) TranscriptPath(id uint) string {
	return s.transcripts.Path(orchKey(id))
}

// Messages 从转录重建主会话消息（分页语义与 ChatService.Messages 一致）。
func (s *Service) Messages(id uint, limit int, before uint) ([]model.Message, int, error) {
	return s.rebuildFor(orchKey(id), id, limit, before)
}

// TaskMessages 重建任务子会话的消息。
func (s *Service) TaskMessages(taskID uint, limit int, before uint) ([]model.Message, int, error) {
	return s.rebuildFor(orchTaskKey(taskID), taskID, limit, before)
}

func (s *Service) rebuildFor(key string, id uint, limit int, before uint) ([]model.Message, int, error) {
	entries, err := s.transcripts.Read(key)
	if err != nil {
		return nil, 0, fmt.Errorf("read transcript: %w", err)
	}
	all := service.RebuildMessages(id, entries)
	total := len(all)
	if before > 0 {
		cut := len(all)
		for i, m := range all {
			if m.ID >= before {
				cut = i
				break
			}
		}
		all = all[:cut]
	}
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all, total, nil
}
