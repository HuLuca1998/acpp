package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/gorm"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
)

// SessionService 负责会话与会话消息的读写。
type SessionService struct {
	db *gorm.DB
}

func NewSessionService(db *gorm.DB) *SessionService {
	return &SessionService{db: db}
}

// SessionView 是会话的对外视图，额外带上 agent 名称与方言。
// 消息数直接用 model.Session.MessageCount 缓存列，不在读路径重建。
type SessionView struct {
	model.Session
	AgentName string `json:"agentName"`
	// AgentFlavor 供界面显示品牌图标（claude/codex/generic）。
	AgentFlavor string `json:"agentFlavor,omitempty"`
	// Running 表示当前有没有活着的 agent 子进程，由 HTTP 层填充。
	Running bool `json:"running"`
	// Settings 是统一设置视图：会话开着时取自 runtime 实时状态；
	// 未连接时由 agent 探测缓存降级拼出（Current* 为空）。
	Settings *acp.Settings `json:"settings,omitempty"`
	// Commands 是斜杠命令清单，未连接时同样来自 agent 探测缓存。
	Commands []acp.Command `json:"commands,omitempty"`
	// GitBranch 是工作目录当前的 git 分支（detached 时是短 hash），
	// 非 git 目录为空。每次取视图时现读，agent 切分支后刷新即可见。
	GitBranch string `json:"gitBranch,omitempty"`
}

// SessionInput 是创建会话的入参。
type SessionInput struct {
	AgentID uint   `json:"agentId"`
	Title   string `json:"title"`
	Cwd     string `json:"cwd"`
}

// List 按更新时间倒序分页。pageSize 有默认与上限——全量拉取会随
// 会话数线性变慢，侧栏这类场景只需要前几条。
func (s *SessionService) List(ctx context.Context, agentID uint, page, pageSize int) ([]SessionView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}

	q := s.db.WithContext(ctx).Model(&model.Session{})
	if agentID != 0 {
		q = q.Where("agent_id = ?", agentID)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count sessions: %w", err)
	}

	var sessions []model.Session
	err := q.Preload("Agent").
		Order("updated_at desc").
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		Find(&sessions).Error
	if err != nil {
		return nil, 0, fmt.Errorf("list sessions: %w", err)
	}

	views := make([]SessionView, 0, len(sessions))
	for i := range sessions {
		views = append(views, *s.toView(&sessions[i]))
	}
	return views, total, nil
}

func (s *SessionService) Get(ctx context.Context, id uint) (*SessionView, error) {
	var session model.Session
	err := s.db.WithContext(ctx).Preload("Agent").First(&session, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("session %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get session %d: %w", id, err)
	}
	return s.toView(&session), nil
}

func (s *SessionService) Create(ctx context.Context, in SessionInput) (*SessionView, error) {
	if in.AgentID == 0 {
		return nil, fmt.Errorf("%w: agentId is required", ErrInvalid)
	}

	var agent model.Agent
	err := s.db.WithContext(ctx).First(&agent, in.AgentID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("agent %d: %w", in.AgentID, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("load agent %d: %w", in.AgentID, err)
	}

	cwd := in.Cwd
	if cwd == "" {
		cwd = agent.Cwd
	}
	if cwd == "" {
		cwd = DefaultCwd()
	}

	session := model.Session{
		AgentID: agent.ID,
		Title:   in.Title,
		Cwd:     cwd,
		State:   model.SessionActive,
	}
	if err := s.db.WithContext(ctx).Create(&session).Error; err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	session.Agent = &agent
	return s.toView(&session), nil
}

func (s *SessionService) Delete(ctx context.Context, id uint) error {
	res := s.db.WithContext(ctx).Delete(&model.Session{}, id)
	if res.Error != nil {
		return fmt.Errorf("delete session %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("session %d: %w", id, ErrNotFound)
	}
	return nil
}

func (s *SessionService) toView(session *model.Session) *SessionView {
	// MessageCount 由 HTTP 层从转录重建结果填充，消息本身不进库。
	view := SessionView{Session: *session}
	if session.Agent != nil {
		view.AgentName = session.Agent.Name
		view.AgentFlavor = session.Agent.Flavor
	}
	view.GitBranch = gitBranch(view.Cwd)
	return &view
}

// gitBranch 读工作目录的当前分支。只读文件不 exec git：
// 普通仓库读 .git/HEAD；worktree 的 .git 是一个 gitdir 指针文件，跟一跳。
func gitBranch(cwd string) string {
	if cwd == "" {
		return ""
	}
	gitPath := filepath.Join(cwd, ".git")
	head := filepath.Join(gitPath, "HEAD")
	if fi, err := os.Stat(gitPath); err != nil {
		return ""
	} else if !fi.IsDir() {
		data, err := os.ReadFile(gitPath)
		if err != nil {
			return ""
		}
		gitdir := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), "gitdir:"))
		head = filepath.Join(gitdir, "HEAD")
	}

	data, err := os.ReadFile(head)
	if err != nil {
		return ""
	}
	ref := strings.TrimSpace(string(data))
	if branch, ok := strings.CutPrefix(ref, "ref: refs/heads/"); ok {
		return branch
	}
	// detached HEAD：给短 hash，聊胜于无。
	if len(ref) >= 7 {
		return ref[:7]
	}
	return ""
}
