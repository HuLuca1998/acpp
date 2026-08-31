// Package discord 是独立于会话体系的 Discord 频道工作区子系统
// （adr-016 绑定面 / adr-017 对话面）：频道经 /init 绑定仓库与模型并克隆
// 出专属工作目录，@bot 开子区、子区内直接与 acp 对话。刻意与会话零耦合——
// 项目内只 import 两个叶子包（gitrepo、acp 协议客户端），业务依赖全部经
// Deps 闭包注入，回退面 = 删本包 + 装配处几行 + 配置文件。
package discord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"acpp/server/internal/acp"
	"acpp/server/internal/mcp"
)

// 哨兵错误自带一套（本包不依赖 service），httpapi 的 writeError 登记映射。
var (
	ErrInvalid  = errors.New("invalid")
	ErrNotFound = errors.New("not found")
)

// CatalogFunc 由装配层提供：内置工具（claude/codex）的启用模型与思考深度
// 清单，/init 表单与绑定编辑框的选项都从这来。
type CatalogFunc func(ctx context.Context) ([]AgentOption, error)

// ReposFunc 由装配层提供：可克隆的远端仓库清单（gh CLI），/init 表单的
// 仓库下拉用。取不到不挡流程——表单退化成纯手输。
type ReposFunc func(ctx context.Context) ([]RepoOption, error)

// RepoOption 是仓库下拉的一项。
type RepoOption struct {
	Name     string `json:"name"`
	CloneURL string `json:"cloneUrl"`
}

// DataSourcesFunc 由装配层提供：可绑定的数据库连接清单（只给启用中的）。
// /init 表单的数据库项与 /db 的换绑选项都从这来；取不到不挡流程——那一项
// 消失，频道就是不锁定。
type DataSourcesFunc func(ctx context.Context) ([]DBOption, error)

// DBOption 是数据库选择的一项。Ref 是数据源的对外标识 `<项目>/<环境>`
// （pp-game/prod），Database 是它锁定的那个库。
type DBOption struct {
	ID       uint   `json:"id"`
	Ref      string `json:"ref"`
	Database string `json:"database"`
	ReadOnly bool   `json:"readOnly"`
}

// Deps 是装配层注入的全部外部依赖，discord 包因此不认识其他业务包
// （acp 是叶子协议客户端，与 gitrepo 同性质，直接用）。
type Deps struct {
	// DefaultWorkRoot 是没配置工作根时的克隆落点根（<工作区根>/discord，
	// 与租户 root 同层）。工作区根可运行时改，所以是函数不是值。
	DefaultWorkRoot func() string
	Catalog         CatalogFunc
	Repos           ReposFunc
	// DataSources 是可绑定的数据库连接清单（/init 的数据库项、/db 换绑）。
	DataSources DataSourcesFunc
	// AgentRuntime 返回内置工具的启动方式（命令/参数/环境），子区对话
	// 拉起 acp 子进程用。nil 时对话面整体停用（@ 提及不响应）。
	AgentRuntime func(ctx context.Context, agent string) (acp.Runtime, error)
	// SkillpackDir 是控制端技能包目录，子区会话与网页会话注入同一份。
	SkillpackDir string
	// MCPBase 是自家 acpp-chat 工具面（send_file）的回连端点前缀
	// （http://127.0.0.1:<port>/api/mcp/discord/）。空则不挂这个工具面。
	MCPBase string
	// Mounts 为一个子区会话算工具面挂载载荷（MCP server 清单 + _meta
	// 追加），与网页会话同源：报告工具面无条件挂，数据库工具面还要项目
	// 配了数据源才有。key 是子区会话键（回连凭证与报告回调按它路由）；
	// agent 产出报告并调用 report_open 时 onReport 会被回调（rel 相对
	// cwd）。nil 表示两个工具面都不接。
	// withDB 为真才挂数据库工具面（子区默认开，/db off 显式关）；
	// dbSourceID 非零时把可见数据源锁死到那一条（频道绑定的环境）。
	Mounts func(ctx context.Context, key, cwd, flavor string, withDB bool, dbSourceID uint, onReport func(rel, title string)) (mcpServers []any, metaExtra map[string]any, err error)
}

// AgentOption 是一个内置工具的可选项集合。
type AgentOption struct {
	Agent   string        `json:"agent"`
	Models  []ModelOption `json:"models"`
	Efforts []string      `json:"efforts"`
}

// ModelOption 是一个可选模型（Label 已做过 alias 优先的展示名处理）。
type ModelOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Guild 是 bot 所在的一个服务器（状态面展示用）。
type Guild struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Status 是 gateway 的实时状态快照。
type Status struct {
	// Running 表示子系统被启用且配了 token（gateway 循环在跑）；
	// Connected 才是真连上了。
	Running   bool    `json:"running"`
	Connected bool    `json:"connected"`
	BotUser   string  `json:"botUser,omitempty"`
	BotID     string  `json:"botId,omitempty"`
	AppID     string  `json:"appId,omitempty"`
	Guilds    []Guild `json:"guilds"`
	LastError string  `json:"lastError,omitempty"`
}

// ConfigView 是配置的对外形状：token 永不回传，只报有没有。
type ConfigView struct {
	Enabled  bool   `json:"enabled"`
	TokenSet bool   `json:"tokenSet"`
	WorkRoot string `json:"workRoot"`
}

// Info 是 GET /api/discord 的完整视图。
type Info struct {
	Config   ConfigView    `json:"config"`
	Status   Status        `json:"status"`
	Bindings []Binding     `json:"bindings"`
	Catalog  []AgentOption `json:"catalog"`
	// InviteURL 是把这个 bot 邀进服务器的 OAuth2 授权链接（连上 gateway
	// 拿到 appId 后才有）。权限位由后端出口径——它与功能清单耦合。
	InviteURL string `json:"inviteUrl,omitempty"`
}

// invitePermissions 是邀请链接的权限位，与 docs/discord-bot-setup.md 的
// 11 项清单一一对应：Manage Channels（写频道主题）、Add Reactions、
// View Channels、Send Messages、Manage Messages（置顶手册）、Embed
// Links、Attach Files、Read Message History、Manage Threads、Create
// Public Threads、Send Messages in Threads。改这里必须同步手册。
const invitePermissions = 1<<4 | 1<<6 | 1<<10 | 1<<11 | 1<<13 | 1<<14 |
	1<<15 | 1<<16 | 1<<34 | 1<<35 | 1<<38

// ConfigPatch 是配置更新入参，逐项可选。BotToken 的语义：nil 不动、
// 空串清除、非空替换。
type ConfigPatch struct {
	Enabled  *bool   `json:"enabled"`
	BotToken *string `json:"botToken"`
	WorkRoot *string `json:"workRoot"`
}

// Service 管子系统生命周期：配置变化时起停 gateway，维护状态快照。
type Service struct {
	store *store
	deps  Deps

	mu sync.Mutex
	// parent 是 Start 收到的进程级上下文，gateway 每次重启从它派生。
	parent context.Context
	cancel context.CancelFunc
	st     Status
	// registered 记录本次连接内已注册过 /init 的 guild，重连后清零重来。
	registered map[string]bool
	// pending 是等着选分支的 /init（id → 中途状态），15 分钟过期
	//（interaction token 的时效）。
	pending map[string]pendingInit
	// topicRetry 记录哪些频道挂着主题限速重试（每频道最多一个）。
	topicRetry map[string]bool

	// acpMgr 是 discord 专属的 acp 会话池——与网页会话池完全分开，
	// 上限与空闲回收独立，互不挤占。AgentRuntime 未注入时为 nil（对话停用）。
	acpMgr *acp.Manager
	// chats 是子区的对话运行态（输入队列、挂起的问答），内存态。
	chatMu sync.Mutex
	chats  map[string]*threadChat
	// chanKind 缓存「这个 channel id 是绑定频道的子区吗」的判定结果，
	// 免得每条消息都去 REST 查一次。
	chanKind map[string]string
	// botRoles 缓存 bot 在各 guild 的集成角色 id（@角色也算 @bot）。
	botRoles map[string]string
	// chatTok 是自家 acpp-chat 工具面（send_file）的回连凭证。
	chatTok mcp.PeerTokens
}

// New 加载配置并构建服务；gateway 由 Start 按配置决定起不起。
func New(path string, deps Deps) (*Service, error) {
	st, err := newStore(path)
	if err != nil {
		return nil, err
	}
	s := &Service{store: st, deps: deps, registered: map[string]bool{},
		pending: map[string]pendingInit{}, topicRetry: map[string]bool{},
		chats: map[string]*threadChat{}, chanKind: map[string]string{}, botRoles: map[string]string{}}
	if deps.AgentRuntime != nil {
		// 子区对话的会话池：无人值守场景给宽松的轮超时，上限收紧——
		// discord 的并发子区不会太多，别让它抢网页会话的资源。
		s.acpMgr = acp.NewManager(chatMaxSessions, chatTurnTimeout, deps.SkillpackDir)
	}
	return s, nil
}

// Start 记住进程级上下文并按当前配置拉起 gateway 与空闲回收。
func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	s.parent = ctx
	s.mu.Unlock()
	s.applyGateway()
	if s.acpMgr != nil {
		go s.reapIdle(ctx)
	}
}

// Close 停掉 gateway 并回收全部 acp 子进程（进程退出时用；幂等）。
func (s *Service) Close() {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.mu.Unlock()
	if s.acpMgr != nil {
		s.acpMgr.CloseAll()
	}
}

// reapIdle 定期回收空闲的子区会话子进程。上下文在 agent 侧持久化
// （acpSessionId 落盘），下次说话 session/load 无感恢复。
func (s *Service) reapIdle(ctx context.Context) {
	tick := time.NewTicker(5 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			for _, key := range s.acpMgr.Idle(chatIdleTimeout) {
				if err := s.acpMgr.Close(key); err != nil {
					slog.Warn("回收子区会话失败", "key", key, "err", err)
				}
			}
		}
	}
}

// applyGateway 让 gateway 的运行态跟上配置：先停旧的，该跑再起新的。
func (s *Service) applyGateway() {
	cfg := s.store.config()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	run := cfg.Enabled && cfg.BotToken != ""
	s.st = Status{Running: run, Guilds: []Guild{}}
	s.registered = map[string]bool{}
	if !run || s.parent == nil {
		return
	}
	ctx, cancel := context.WithCancel(s.parent)
	s.cancel = cancel
	go s.runLoop(ctx, cfg.BotToken)
}

// runLoop 维持 Gateway 长连接：断开记状态、退避重连，配置变化时被 cancel。
func (s *Service) runLoop(ctx context.Context, token string) {
	backoff := backoffMin
	for ctx.Err() == nil {
		err := gatewayOnce(ctx, token, func() { backoff = backoffMin }, func(t string, d json.RawMessage) {
			s.handleEvent(ctx, token, t, d)
		})
		if ctx.Err() != nil {
			return
		}
		s.setDisconnected(err)
		slog.Info("discord gateway 断开，稍后重连", "err", err, "backoff", backoff)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
		if backoff *= 2; backoff > backoffMax {
			backoff = backoffMax
		}
	}
}

// handleEvent 消费一条 dispatch 事件，维护状态快照并分发交互。
func (s *Service) handleEvent(ctx context.Context, token, t string, d json.RawMessage) {
	switch t {
	case "READY":
		var ready struct {
			User struct {
				ID       string `json:"id"`
				Username string `json:"username"`
			} `json:"user"`
			Application struct {
				ID string `json:"id"`
			} `json:"application"`
		}
		if err := json.Unmarshal(d, &ready); err != nil {
			slog.Warn("解 READY 失败", "err", err)
			return
		}
		s.mu.Lock()
		s.st.Connected = true
		s.st.LastError = ""
		s.st.BotUser = ready.User.Username
		s.st.BotID = ready.User.ID
		s.st.AppID = ready.Application.ID
		s.st.Guilds = []Guild{}
		s.registered = map[string]bool{}
		s.mu.Unlock()
		slog.Info("discord bot 已上线", "user", ready.User.Username)
	case "GUILD_CREATE":
		var g Guild
		if err := json.Unmarshal(d, &g); err != nil || g.ID == "" {
			return
		}
		s.mu.Lock()
		found := false
		for i := range s.st.Guilds {
			if s.st.Guilds[i].ID == g.ID {
				s.st.Guilds[i].Name = g.Name
				found = true
			}
		}
		if !found {
			s.st.Guilds = append(s.st.Guilds, g)
		}
		needRegister := !s.registered[g.ID] && s.st.AppID != ""
		s.registered[g.ID] = true
		appID := s.st.AppID
		s.mu.Unlock()
		if needRegister {
			go s.registerCommands(ctx, token, appID, g.ID)
		}
	case "GUILD_DELETE":
		var g struct {
			ID          string `json:"id"`
			Unavailable bool   `json:"unavailable"`
		}
		// unavailable=true 是服务器故障不是被踢，列表里留着。
		if err := json.Unmarshal(d, &g); err != nil || g.Unavailable {
			return
		}
		s.mu.Lock()
		for i := range s.st.Guilds {
			if s.st.Guilds[i].ID == g.ID {
				s.st.Guilds = append(s.st.Guilds[:i], s.st.Guilds[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
	case "MESSAGE_CREATE":
		s.handleMessage(ctx, token, d)
	case "MESSAGE_UPDATE":
		s.handleMessageEdit(d)
	case "INTERACTION_CREATE":
		s.handleInteraction(ctx, token, d)
	}
}

func (s *Service) setDisconnected(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Connected = false
	if err != nil {
		s.st.LastError = err.Error()
	}
}

// Info 汇配置视图 + 状态 + 绑定 + 选项清单（catalog 失败只记日志，
// 不拖垮整个视图——配置页在 agent 探测未完成时也要能打开）。
func (s *Service) Info(ctx context.Context) Info {
	cfg := s.store.config()
	s.mu.Lock()
	st := s.st
	st.Guilds = append([]Guild(nil), s.st.Guilds...)
	s.mu.Unlock()

	catalog := []AgentOption{}
	if s.deps.Catalog != nil {
		if opts, err := s.deps.Catalog(ctx); err != nil {
			slog.Warn("discord 取模型清单失败", "err", err)
		} else {
			catalog = opts
		}
	}
	bindings := cfg.Bindings
	if bindings == nil {
		bindings = []Binding{}
	}
	inviteURL := ""
	if st.AppID != "" {
		inviteURL = fmt.Sprintf(
			"https://discord.com/oauth2/authorize?client_id=%s&scope=bot+applications.commands&permissions=%d",
			st.AppID, int64(invitePermissions))
	}
	return Info{
		InviteURL: inviteURL,
		Config: ConfigView{
			Enabled:  cfg.Enabled,
			TokenSet: cfg.BotToken != "",
			WorkRoot: s.effectiveWorkRoot(cfg),
		},
		Status:   st,
		Bindings: bindings,
		Catalog:  catalog,
	}
}

// SaveConfig 应用配置补丁并让 gateway 跟上，返回最新视图。
func (s *Service) SaveConfig(ctx context.Context, patch ConfigPatch) (Info, error) {
	if patch.BotToken != nil {
		if tok := strings.TrimSpace(*patch.BotToken); tok != "" {
			// 只做形状检查（三段点分、长度够），真伪交给 gateway 连接结果说话。
			if len(tok) < 50 || strings.Count(tok, ".") != 2 {
				return Info{}, fmt.Errorf("%w: 这不像一个 bot token（应为三段点分的长串）", ErrInvalid)
			}
			*patch.BotToken = tok
		}
	}
	if patch.WorkRoot != nil {
		root := strings.TrimSpace(*patch.WorkRoot)
		if root != "" && !filepath.IsAbs(root) {
			return Info{}, fmt.Errorf("%w: 工作根必须是绝对路径", ErrInvalid)
		}
		*patch.WorkRoot = root
	}
	_, err := s.store.update(func(c *Config) {
		if patch.Enabled != nil {
			c.Enabled = *patch.Enabled
		}
		if patch.BotToken != nil {
			c.BotToken = *patch.BotToken
		}
		if patch.WorkRoot != nil {
			c.WorkRoot = *patch.WorkRoot
		}
	})
	if err != nil {
		return Info{}, err
	}
	s.applyGateway()
	return s.Info(ctx), nil
}

// UpdateBinding 改一条绑定的模型/思考深度（后台管理页用；仓库不在这改——
// 换仓库语义上是重建工作区，走频道里重新 /init）。
func (s *Service) UpdateBinding(ctx context.Context, channelID string, patch BindingPatch) (Binding, error) {
	if patch.Agent == "" || patch.Model == "" {
		return Binding{}, fmt.Errorf("%w: agent 与 model 必填", ErrInvalid)
	}
	var out Binding
	_, err := s.store.update(func(c *Config) {
		for i := range c.Bindings {
			if c.Bindings[i].ChannelID == channelID {
				c.Bindings[i].Agent = patch.Agent
				c.Bindings[i].Model = patch.Model
				c.Bindings[i].ModelLabel = patch.ModelLabel
				c.Bindings[i].Effort = patch.Effort
				c.Bindings[i].Access = patch.Access
				c.Bindings[i].UpdatedAt = time.Now()
				out = c.Bindings[i]
				return
			}
		}
	})
	if err != nil {
		return Binding{}, err
	}
	if out.ChannelID == "" {
		return Binding{}, fmt.Errorf("%w: 频道 %s 没有绑定", ErrNotFound, channelID)
	}
	// 频道侧展示（置顶卡 + 主题）跟上网页端的改动，尽力而为。
	if cfg := s.store.config(); cfg.BotToken != "" {
		go s.syncChannelCard(context.Background(), cfg.BotToken, out)
	}
	return out, nil
}

// BindingPatch 是绑定编辑入参（管理页的编辑框整体提交）。
type BindingPatch struct {
	Agent      string `json:"agent"`
	Model      string `json:"model"`
	ModelLabel string `json:"modelLabel"`
	Effort     string `json:"effort"`
	Access     string `json:"access"`
}

// RemoveBinding 解绑频道。磁盘上的克隆不动——目录里可能已有未推送的活。
func (s *Service) RemoveBinding(channelID string) error {
	cfg := s.store.config()
	old, existed := cfg.binding(channelID)
	removed := false
	_, err := s.store.update(func(c *Config) {
		removed = c.removeBinding(channelID)
	})
	if err != nil {
		return err
	}
	if !removed {
		return fmt.Errorf("%w: 频道 %s 没有绑定", ErrNotFound, channelID)
	}
	// 频道侧收尾（摘置顶卡、清主题）尽力而为。
	if existed && cfg.BotToken != "" {
		go s.cleanupChannelCard(context.Background(), cfg.BotToken, old)
	}
	return nil
}

// effectiveWorkRoot 是克隆落点的根：没配置就用 <工作区根>/discord——
// 与租户 root 同层摆放（`~/acpp/<租户>` 旁边的 `~/acpp/discord`），
// 这正是「单独的 discord 工作目录」但不脱离工作区的家。
func (s *Service) effectiveWorkRoot(cfg Config) string {
	if cfg.WorkRoot != "" {
		return cfg.WorkRoot
	}
	if s.deps.DefaultWorkRoot != nil {
		if root := s.deps.DefaultWorkRoot(); root != "" {
			return root
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "acpp-discord"
	}
	return filepath.Join(home, "acpp", "discord")
}
