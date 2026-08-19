package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"gorm.io/gorm"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
	"acpp/server/internal/stream"
	"acpp/server/internal/transcript"
)

// ChatService 把 ACP 会话、转录留存与浏览器推流接在一起。
// 对话内容唯一的持久化形态是转录 JSONL；数据库只保管会话元数据。
type ChatService struct {
	db          *gorm.DB
	sessions    *SessionService
	manager     *acp.Manager
	transcripts *transcript.Store
	// skillUsage 记录技能被 AI 调用的次数，从 tool_call 事件计。可为 nil
	//（不启用统计）。
	skillUsage *SkillUsageService

	// sources 由装配层注入的数据源能力面：为会话挂 MCP 工具、把 @ 数据库
	// 引用展开成 prompt 内容。用接口而不是直接 import 那个业务包——依赖
	// 只允许单向，而它反过来要用 SessionService 解析 token。
	// 可为 nil（没有数据库能力，会话照常可用）。
	sources DataSources

	// titler 生成会话标题；nil 或未启用时退回首句派生。
	titler Titler

	mu      sync.Mutex
	brokers map[uint]*stream.Broker
}

// DataSources 是数据源能力的注入口。
//
// MountsFor 为一条会话算出要挂载的 MCP server 清单与 _meta 追加内容
// （两个返回值对应 acp.OpenOptions 的 MCPServers 与 MetaExtra，哪个有值
// 取决于 runtime 方言）；Reference 把 @ 数据库引用展开成可嵌进 prompt 的
// 文本。两者都按 cwd 所属项目过滤，会话看不见别的项目的库。
type DataSources interface {
	MountsFor(ctx context.Context, sessionID uint, cwd, flavor string) ([]any, map[string]any, error)
	Reference(ctx context.Context, cwd string, refs []string) ([]DBReference, error)
}

// DBReference 是一条展开后的 @ 数据库引用，形状对齐 @ 文件引用的
// resource 块。类型定义在这一侧而不是数据源包里：数据源包已经 import
// 了本包（借哨兵错误），反过来再 import 就成环了。
type DBReference struct {
	// URI 进 resource 块的 uri，让 AI 看得出这段内容的出处
	// （`mysql://<项目>/<环境>/<库>/<表>`）。
	URI  string
	Text string
}

// SetDataSources 装上数据源能力面。装配期调用一次，之后只读。
func (s *ChatService) SetDataSources(d DataSources) { s.sources = d }

// Titler 是会话标题生成能力的注入口（实现在 internal/titler）。可为 nil：
// 没装或没启用时会话沿用首句派生的标题，功能不受影响。
// Enabled 单独暴露是为了在开销之前就短路——不必为了发现「没开」而先构造
// 一次请求，也免得本包为判断一个哨兵错误去 import 实现包。
type Titler interface {
	Enabled() bool
	Generate(ctx context.Context, user, assistant string) (string, error)
}

// SetTitler 装上标题生成能力。装配期调用一次，之后只读。
func (s *ChatService) SetTitler(t Titler) { s.titler = t }

func NewChatService(db *gorm.DB, sessions *SessionService, manager *acp.Manager, transcripts *transcript.Store, skillUsage *SkillUsageService) *ChatService {
	return &ChatService{
		db:          db,
		sessions:    sessions,
		manager:     manager,
		transcripts: transcripts,
		skillUsage:  skillUsage,
		brokers:     make(map[uint]*stream.Broker),
	}
}

// Peek 返回会话视图但绝不拉起进程——「查看记录」是零成本读操作，
// 只有真的发消息才值得连接 agent。进程恰好活着时顺带附上统一设置。
//
// 本文件里对会话的读取一律用 OwnerScope：归属校验在 HTTP 入口一次性做完
// （adr-007），到这里已经是「确认属于调用者」的会话 id，再过一次租户条件
// 只会让内部流程（空闲回收、错误标记）无从下手。

func (s *ChatService) Peek(ctx context.Context, sessionID uint) (*SessionView, error) {
	view, err := s.sessions.Get(ctx, OwnerScope(), sessionID)
	if err != nil {
		return nil, err
	}
	key := sessionKey(sessionID)
	if _, ok := s.manager.Get(key); !ok {
		// 进程不在也要给出与连接时结构一致的设置视图：维度清单来自 agent
		// 探测缓存（模型 + 骨架），当前值来自会话的最后设置快照——
		// 恢复会话的工具栏因此与进行中的会话显示一致。
		var agent model.Agent
		if err := s.db.WithContext(ctx).First(&agent, view.AgentID).Error; err == nil {
			settings := DegradedSettings(&agent, view.LastSettings)
			if len(settings.Models) > 0 {
				view.Settings = settings
			}
			for _, c := range agent.Commands {
				if c.Disabled {
					continue
				}
				view.Commands = append(view.Commands, acp.Command{
					Name: c.Name, Description: c.Description,
				})
			}
		}
		return view, nil
	}
	view.Running = true
	cat := s.catalogFor(ctx, sessionID)
	if settings, err := s.manager.Settings(key); err == nil {
		cat.filterSettings(&settings)
		view.Settings = &settings
		s.saveSettingsSnapshot(sessionID, &settings)
	}
	view.Commands = cat.filterCommands(s.manager.Commands(key))
	return view, nil
}

// Open 为一条已存在的数据库会话拉起 agent 并完成 ACP 握手。
// 会话已经开着时直接返回，重复调用是安全的。
func (s *ChatService) Open(ctx context.Context, sessionID uint) (*SessionView, error) {
	view, err := s.sessions.Get(ctx, OwnerScope(), sessionID)
	if err != nil {
		return nil, err
	}

	key := sessionKey(sessionID)
	if _, ok := s.manager.Get(key); ok {
		cat := s.catalogFor(ctx, sessionID)
		if settings, err := s.manager.Settings(key); err == nil {
			cat.filterSettings(&settings)
			view.Settings = &settings
		}
		view.Commands = cat.filterCommands(s.manager.Commands(key))
		return view, nil
	}

	var agent model.Agent
	if err := s.db.WithContext(ctx).First(&agent, view.AgentID).Error; err != nil {
		return nil, fmt.Errorf("load agent %d: %w", view.AgentID, err)
	}

	cwd := view.Cwd
	if cwd == "" {
		cwd = agent.Cwd
	}

	// 工具面按工作目录现算：数据库数据源是按项目隔离的，会话开在哪个
	// 项目就只挂哪个项目的。算不出来不算失败——没有数据库工具的会话
	// 照样是一条正常会话。
	var mcpServers []any
	var metaExtra map[string]any
	if s.sources != nil {
		flavor := agent.Flavor
		if flavor == "" {
			flavor = string(acp.FlavorOf(agent.Name, agent.Command))
		}
		mcpServers, metaExtra, err = s.sources.MountsFor(ctx, sessionID, cwd, flavor)
		if err != nil {
			slog.Warn("mount session mcp", "session", sessionID, "err", err)
			mcpServers, metaExtra = nil, nil
		}
	}

	br := s.brokerFor(sessionID)
	sess, err := s.manager.Open(ctx, acp.OpenOptions{
		Key:     key,
		Runtime: acp.RuntimeFor(agent.Command, agent.Args, agent.Env),
		Cwd:     cwd,
		OnEvent: func(ev acp.Event) { s.handleEvent(sessionID, br, ev) },
		// 全量线级消息进转录，这是对话内容唯一的持久化。
		WireTap: func(dir string, msg json.RawMessage) { s.transcripts.Append(key, dir, msg) },
		// 进程重启后优先恢复 agent 侧的同一条会话，保住上下文。
		ResumeACPSessionID: view.ACPSessionID,
		MCPServers:         mcpServers,
		MetaExtra:          metaExtra,
	})
	if err != nil {
		// 一次都没连上过、也没有任何消息的会话是个空壳。留着它只会在列表里
		// 变成一条点开就报错的死记录——agent 命令不在 PATH 里时，每试一次
		// 新建就攒一条。这种直接回收，不让用户去手动收拾；已经跑过的会话
		// 不在此列，它们的历史比一次连接失败值钱。
		if view.ACPSessionID == "" && view.MessageCount == 0 {
			if derr := s.sessions.Delete(ctx, OwnerScope(), sessionID); derr != nil {
				slog.Warn("discard never-started session", "session", sessionID, "err", derr)
			}
			return nil, fmt.Errorf("open acp session: %w", err)
		}
		s.markSessionError(sessionID, err)
		return nil, fmt.Errorf("open acp session: %w", err)
	}

	// Open 只是把进程拉起来，不代表在跑——active 留给 turn 进行中。
	updates := map[string]any{
		"acp_session_id": sess.ACPSessionID(),
		"state":          model.SessionIdle,
		"cwd":            cwd,
	}
	if err := s.db.WithContext(ctx).Model(&model.Session{}).Where("id = ?", sessionID).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("save acp session id: %w", err)
	}

	view, err = s.sessions.Get(ctx, OwnerScope(), sessionID)
	if err != nil {
		return nil, err
	}
	cat := s.catalogFor(ctx, sessionID)
	if settings, err := s.manager.Settings(key); err == nil {
		cat.filterSettings(&settings)
		view.Settings = &settings
		s.saveSettingsSnapshot(sessionID, &settings)
		// 懒连接下 Open 多由 Send 顺路触发，前端不会拿到这份 HTTP 响应——
		// 统一视图与命令清单同时走 SSE 广播一份。
		br.Publish(StreamEvent{Kind: "settings", Settings: &settings})
	}
	view.Commands = cat.filterCommands(s.manager.Commands(key))
	if len(view.Commands) > 0 {
		br.Publish(StreamEvent{Kind: "commands", Commands: view.Commands})
	}
	return view, nil
}

// Destroy 在删除会话前做彻底清理：先尽力删掉 agent 侧的线程历史
// （session/delete，删不掉只记警告不阻塞本地删除），再关进程。
func (s *ChatService) Destroy(ctx context.Context, sessionID uint) error {
	view, err := s.sessions.Get(ctx, OwnerScope(), sessionID)
	if err == nil && view.ACPSessionID != "" {
		var agent model.Agent
		if err := s.db.WithContext(ctx).First(&agent, view.AgentID).Error; err == nil {
			err := s.manager.DeleteRemote(ctx, sessionKey(sessionID),
				acp.RuntimeFor(agent.Command, agent.Args, agent.Env), view.ACPSessionID)
			if err != nil {
				slog.Warn("delete remote session", "session", sessionID, "err", err)
			}
		}
	}
	return s.Close(ctx, sessionID)
}

// Close 关掉 ACP 会话并回收子进程，数据库记录与转录文件保留。
func (s *ChatService) Close(ctx context.Context, sessionID uint) error {
	if err := s.manager.Close(sessionKey(sessionID)); err != nil {
		return err
	}
	s.transcripts.Close(sessionKey(sessionID))
	s.mu.Lock()
	delete(s.brokers, sessionID)
	s.mu.Unlock()

	return s.db.WithContext(ctx).Model(&model.Session{}).
		Where("id = ?", sessionID).
		Update("state", model.SessionEnded).Error
}

// Release 回收空闲会话的子进程但保留续聊能力：上下文留在 agent 侧
// （acpSessionId 已持久化），下次发消息 Open 会用 session/load 无感恢复。
func (s *ChatService) Release(ctx context.Context, sessionID uint) error {
	if err := s.manager.Close(sessionKey(sessionID)); err != nil {
		return err
	}
	s.transcripts.Close(sessionKey(sessionID))
	return s.db.WithContext(ctx).Model(&model.Session{}).
		Where("id = ?", sessionID).
		Update("state", model.SessionIdle).Error
}

// StartIdleReaper 周期回收空闲子进程——会话可以随时 resume，没必要一直挂着。
// timeout <= 0 表示不回收。
func (s *ChatService) StartIdleReaper(ctx context.Context, timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for _, key := range s.manager.Idle(timeout) {
					id, err := strconv.ParseUint(key, 10, 64)
					if err != nil {
						// probe-agent-N 这类管理用连接不归这里管。
						continue
					}
					if err := s.Release(ctx, uint(id)); err != nil {
						slog.Warn("release idle session", "session", id, "err", err)
					} else {
						slog.Info("released idle session", "session", id, "timeout", timeout)
					}
				}
			}
		}
	}()
}

// Running 报告该会话当前是否有活着的 agent 进程。
func (s *ChatService) Running(sessionID uint) bool {
	_, ok := s.manager.Get(sessionKey(sessionID))
	return ok
}

func (s *ChatService) markSessionError(sessionID uint, cause error) {
	err := s.db.Model(&model.Session{}).Where("id = ?", sessionID).Updates(map[string]any{
		"state":       model.SessionError,
		"stop_reason": TruncateError(cause.Error()),
	}).Error
	if err != nil {
		slog.Error("mark session error", "session", sessionID, "err", err)
	}
}

func (s *ChatService) brokerFor(sessionID uint) *stream.Broker {
	s.mu.Lock()
	defer s.mu.Unlock()
	if br, ok := s.brokers[sessionID]; ok {
		return br
	}
	br := stream.NewBroker()
	s.brokers[sessionID] = br
	return br
}

func sessionKey(id uint) string { return strconv.FormatUint(uint64(id), 10) }

func TruncateError(s string) string {
	const max = 512
	if len(s) <= max {
		return s
	}
	return s[:max]
}
