// Package discord 是独立于会话体系的 Discord 频道工作区子系统（adr-016）：
// 频道经 /init 绑定仓库与模型，克隆出专属工作目录。刻意与会话零耦合——
// 项目内只 import 纯函数叶子包 gitrepo，模型清单由装配层经 CatalogFunc
// 注入，回退面 = 删本包 + 装配处几行 + 配置文件。
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

// Deps 是装配层注入的全部外部依赖，discord 包因此不认识其他业务包。
type Deps struct {
	// DefaultWorkRoot 是没配置工作根时的克隆落点根（<工作区根>/discord，
	// 与租户 root 同层）。工作区根可运行时改，所以是函数不是值。
	DefaultWorkRoot func() string
	Catalog         CatalogFunc
	Repos           ReposFunc
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
}

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
}

// New 加载配置并构建服务；gateway 由 Start 按配置决定起不起。
func New(path string, deps Deps) (*Service, error) {
	st, err := newStore(path)
	if err != nil {
		return nil, err
	}
	return &Service{store: st, deps: deps, registered: map[string]bool{}, pending: map[string]pendingInit{}}, nil
}

// Start 记住进程级上下文并按当前配置拉起 gateway。
func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	s.parent = ctx
	s.mu.Unlock()
	s.applyGateway()
}

// Close 停掉 gateway（进程退出时用；幂等）。
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
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
	return Info{
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
	return out, nil
}

// BindingPatch 是绑定编辑入参（管理页的编辑框整体提交）。
type BindingPatch struct {
	Agent      string `json:"agent"`
	Model      string `json:"model"`
	ModelLabel string `json:"modelLabel"`
	Effort     string `json:"effort"`
}

// RemoveBinding 解绑频道。磁盘上的克隆不动——目录里可能已有未推送的活。
func (s *Service) RemoveBinding(channelID string) error {
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
