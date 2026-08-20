package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

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
	// TenantName 是会话创建者的名字；**空表示 owner 自己**（他不在租户表
	// 里，见 model.Tenant 的注释）。租户只看得见自己的会话，这个字段对他
	// 恒为自己，真正用它的是 owner 的列表。
	TenantName string `json:"tenantName,omitempty"`
}

// SessionInput 是创建会话的入参。
type SessionInput struct {
	AgentID uint   `json:"agentId"`
	Title   string `json:"title"`
	Cwd     string `json:"cwd"`
	// Worktree 非空时先在 Cwd 所在仓库下开 `worktrees/<Worktree>`，会话的
	// 工作目录随之指向那里——「在隔离工作区里干活」是勾一下就该成立的事，
	// 不该让人先去终端里手敲 git worktree add（adr-007）。
	Worktree string `json:"worktree,omitempty"`
	// WorktreeBranch 为空时用 Worktree 当分支名。
	WorktreeBranch string `json:"worktreeBranch,omitempty"`
}

// List 按更新时间倒序分页。pageSize 有默认与上限——全量拉取会随
// 会话数线性变慢，侧栏这类场景只需要前几条。
func (s *SessionService) List(ctx context.Context, scope Scope, agentID uint, page, pageSize int, orderBy string) ([]SessionView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}

	q := scope.FilterSessions(s.db.WithContext(ctx).Model(&model.Session{}))
	if agentID != 0 {
		q = q.Where("agent_id = ?", agentID)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count sessions: %w", err)
	}

	if orderBy == "" {
		orderBy = "updated_at desc"
	}
	var sessions []model.Session
	err := q.Preload("Agent").
		Order(orderBy).
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
	s.fillTenantNames(ctx, views)
	return views, total, nil
}

// fillTenantNames 批量补上创建者名字（owner 的会话留空）。
//
// 刻意不用 GORM 关联：owner 的会话记 tenant_id = 0，而 owner 不在 tenants
// 表里（他由 loopback 判定，见 model.Tenant）。声明成关联会让 AutoMigrate
// 建出一条外键约束，那条约束对着 0 永远不成立——服务连启动都启动不了。
func (s *SessionService) fillTenantNames(ctx context.Context, views []SessionView) {
	ids := make([]uint, 0, len(views))
	for i := range views {
		if id := views[i].TenantID; id != 0 && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}

	var tenants []model.Tenant
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&tenants).Error; err != nil {
		// 名字取不到不影响列表本身：创建者列留空好过整页报错。
		slog.Warn("load tenant names", "err", err)
		return
	}
	byID := make(map[uint]string, len(tenants))
	for _, t := range tenants {
		byID[t.ID] = t.Name
	}
	for i := range views {
		views[i].TenantName = byID[views[i].TenantID]
	}
}

// Get 按 scope 取会话：不属于当前身份的会话一律当作**不存在**（404 而不是
// 403）——403 会把「这条会话确实存在」这个事实泄露出去，凭 id 逐个试就能
// 数出别人有多少会话。
func (s *SessionService) Get(ctx context.Context, scope Scope, id uint) (*SessionView, error) {
	var session model.Session
	err := scope.FilterSessions(s.db.WithContext(ctx)).
		Preload("Agent").First(&session, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("session %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get session %d: %w", id, err)
	}
	view := s.toView(&session)
	if session.TenantID != 0 {
		var tenant model.Tenant
		if err := s.db.WithContext(ctx).First(&tenant, session.TenantID).Error; err == nil {
			view.TenantName = tenant.Name
		}
	}
	return view, nil
}

func (s *SessionService) Create(ctx context.Context, scope Scope, in SessionInput) (*SessionView, error) {
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
		// 不带 cwd 就落在这个身份的工作区根：owner 是设置里选的那个，
		// 租户是自己的目录。刻意**不看 agent.Cwd**——那是 agent 配置里的
		// 历史残留，不该替用户决定他这次要在哪儿干活。
		cwd = Home(scope)
	}
	// 会话 cwd 是 agent 的活动范围，必须先过路径闸。目录不存在时由
	// ACP 侧的 session/new 负责创建（README §安全姿态），这里只管边界。
	cwd, err = scope.GuardNewPath(cwd)
	if err != nil {
		return nil, err
	}

	if in.Worktree != "" {
		worktree, err := CreateWorktree(ctx, scope, cwd, WorktreeInput{
			Name:   in.Worktree,
			Branch: in.WorktreeBranch,
		})
		if err != nil {
			return nil, err
		}
		cwd = worktree
	}

	session := model.Session{
		AgentID:  agent.ID,
		TenantID: scope.TenantID,
		Title:    in.Title,
		Cwd:      cwd,
		State:    model.SessionActive,
	}
	if err := s.db.WithContext(ctx).Create(&session).Error; err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	session.Agent = &agent
	return s.toView(&session), nil
}

// EnsureMCPToken 懒生成会话专属的 MCP 端点令牌（agent 子进程带它回连拿
// 工具）。已有就复用——token 换新会让恢复中的会话指向一个死端点。
func (s *SessionService) EnsureMCPToken(ctx context.Context, id uint) (string, error) {
	var session model.Session
	if err := s.db.WithContext(ctx).First(&session, id).Error; err != nil {
		return "", fmt.Errorf("load session %d: %w", id, err)
	}
	if session.MCPToken != "" {
		return session.MCPToken, nil
	}

	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate mcp token: %w", err)
	}
	token := hex.EncodeToString(raw)
	if err := s.db.WithContext(ctx).Model(&model.Session{}).
		Where("id = ?", id).Update("mcp_token", token).Error; err != nil {
		return "", fmt.Errorf("save mcp token: %w", err)
	}
	return token, nil
}

// SessionByMCPToken 把 MCP 端点的 token 解析成会话 id 与工作目录。
// token 即凭证：查不到一律同一个错，不区分「不存在」与「无权」。
//
// 回 id 是为了给调用记录留归属——不然工具台上只能看到「有人调了
// db_query」，看不出是哪条会话干的。
func (s *SessionService) SessionByMCPToken(ctx context.Context, token string) (uint, string, error) {
	if token == "" {
		return 0, "", fmt.Errorf("%w: mcp token", ErrNotFound)
	}
	var session model.Session
	if err := s.db.WithContext(ctx).Where("mcp_token = ?", token).First(&session).Error; err != nil {
		return 0, "", fmt.Errorf("%w: mcp token", ErrNotFound)
	}
	return session.ID, session.Cwd, nil
}

func (s *SessionService) Delete(ctx context.Context, scope Scope, id uint) error {
	res := scope.FilterSessions(s.db.WithContext(ctx)).Delete(&model.Session{}, id)
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

// ── 概览统计 ────────────────────────────────────────────────────────

// 概览页的统计面。
//
// 单独一个端点而不是让前端拿会话列表自己算：列表是分页的，前 50 条算出来
// 的「最近两周趋势」是错的——用户看到的图会随每页行数变化。聚合就该在
// 有全量数据的那一侧做。

// OverviewStats 是概览页要的全部聚合数据。
type OverviewStats struct {
	// Totals 是全量口径的总计。放在这里而不是让前端拿列表自己 reduce：
	// 列表是分页的，前 20 条加出来的「消息总量」只是前 20 条的和。
	Sessions int64 `json:"sessions"`
	Messages int64 `json:"messages"`
	// Daily 是最近 N 天每天的会话数与消息数，按日期升序、**补齐空缺的天**
	//（没有会话的那天也要有一个 0，否则折线会把两周压成三个点）。
	Daily []DailyStat `json:"daily"`
	// ByAgent 是会话在各 agent 上的分布。
	ByAgent []NamedCount `json:"byAgent"`
	// ByState 是会话的状态分布。
	ByState []NamedCount `json:"byState"`
}

type DailyStat struct {
	Date     string `json:"date"`
	Sessions int64  `json:"sessions"`
	Messages int64  `json:"messages"`
}

type NamedCount struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// Overview 汇总概览统计。days 是趋势图的天数窗口。
func (s *SessionService) Overview(ctx context.Context, scope Scope, days int) (*OverviewStats, error) {
	if days <= 0 || days > 90 {
		days = 14
	}
	since := time.Now().AddDate(0, 0, -(days - 1)).Truncate(24 * time.Hour)

	daily, err := s.dailyStats(ctx, scope, since, days)
	if err != nil {
		return nil, err
	}

	var totals struct {
		Sessions int64
		Messages int64
	}
	err = scope.FilterSessions(s.db.WithContext(ctx).Model(&model.Session{})).
		Select("count(*) as sessions, coalesce(sum(message_count),0) as messages").
		Scan(&totals).Error
	if err != nil {
		return nil, fmt.Errorf("overview totals: %w", err)
	}

	byAgent, err := s.countBy(ctx, scope, "agents.name", "JOIN agents ON agents.id = sessions.agent_id")
	if err != nil {
		return nil, err
	}
	byState, err := s.countBy(ctx, scope, "sessions.state", "")
	if err != nil {
		return nil, err
	}
	return &OverviewStats{
		Sessions: totals.Sessions,
		Messages: totals.Messages,
		Daily:    daily,
		ByAgent:  byAgent,
		ByState:  byState,
	}, nil
}

// dailyStats 按天聚合会话数与消息数，并补齐没有数据的日子。
func (s *SessionService) dailyStats(ctx context.Context, scope Scope, since time.Time, days int) ([]DailyStat, error) {
	type row struct {
		Day      string
		Sessions int64
		Messages int64
	}
	var rows []row
	// date() 是 SQLite 的写法——本项目的元数据库固定是 SQLite（见 README）。
	err := scope.FilterSessions(s.db.WithContext(ctx).Model(&model.Session{})).
		Select("date(created_at) as day, count(*) as sessions, coalesce(sum(message_count),0) as messages").
		Where("created_at >= ?", since).
		Group("day").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("daily stats: %w", err)
	}

	byDay := make(map[string]row, len(rows))
	for _, r := range rows {
		byDay[r.Day] = r
	}

	out := make([]DailyStat, 0, days)
	for i := range days {
		day := since.AddDate(0, 0, i).Format("2006-01-02")
		r := byDay[day]
		out = append(out, DailyStat{Date: day, Sessions: r.Sessions, Messages: r.Messages})
	}
	return out, nil
}

// countBy 按某一列分组计数，倒序返回。
func (s *SessionService) countBy(ctx context.Context, scope Scope, column, join string) ([]NamedCount, error) {
	q := scope.FilterSessions(s.db.WithContext(ctx).Model(&model.Session{}))
	if join != "" {
		q = q.Joins(join)
	}
	var out []NamedCount
	err := q.Select(column + " as name, count(*) as count").
		Group(column).
		Order("count desc").
		Scan(&out).Error
	if err != nil {
		return nil, fmt.Errorf("count by %s: %w", column, err)
	}
	return out, nil
}
