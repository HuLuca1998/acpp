package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// 握手要快；单轮默认不设上限——长程任务跑几个小时是正常使用方式，
// 需要保护的部署自己传 turnTimeout。
const handshakeTimeout = 60 * time.Second

// Manager 持有全部活着的会话，按我们自己的 key（数据库 session id）索引。
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	// opening 标记正在握手的 key，防止并发 Open 各自拉起一个进程后泄漏其一。
	opening       map[string]chan struct{}
	max           int
	promptTimeout time.Duration
	// skillpackDir 是控制端技能包目录（<dataDir>/skillpack）。非空时每条会话
	// 注入技能隔离：屏蔽机器级 skill、加载技能包、保留项目级 skill。
	skillpackDir string
}

// NewManager 构造会话池。turnTimeout 是单轮硬上限，<=0 表示不限时
// （默认）——空闲回收以在途调用计数为准，turn 跑多久都不会被误收。
// skillpackDir 为空时不做技能隔离（会话沿用机器级 skill）。
func NewManager(max int, turnTimeout time.Duration, skillpackDir string) *Manager {
	if max <= 0 {
		max = 8
	}
	return &Manager{
		sessions:      make(map[string]*Session),
		opening:       make(map[string]chan struct{}),
		max:           max,
		promptTimeout: turnTimeout,
		skillpackDir:  skillpackDir,
	}
}

// turnContext 给一轮调用套上超时；不限时时原样返回。
func (m *Manager) turnContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if m.promptTimeout > 0 {
		return context.WithTimeout(ctx, m.promptTimeout)
	}
	return context.WithCancel(ctx)
}

// OpenOptions 是打开一条会话所需的全部信息。
type OpenOptions struct {
	// Key 是上层用来索引这条会话的标识（这里用数据库里的 session id）。
	Key     string
	Runtime Runtime
	// Cwd 必须是绝对路径；不存在时会被创建。
	Cwd string
	// OnEvent 接收该会话的全部归一化事件，必须立刻返回。
	OnEvent func(Event)
	// WireTap 可为 nil；非 nil 时收到该会话的全部线级消息（原始 JSON-RPC）。
	WireTap WireTap
	// ResumeACPSessionID 非空且 agent 声明 loadSession 时，先尝试
	// session/load 恢复这条会话的上下文，失败再回退 session/new。
	ResumeACPSessionID string
	// ExtraEnv 追加到 agent 进程环境，在技能隔离注入之后应用——
	// 编排会话用它覆盖 CODEX_HOME / 设置 MCP_TOOL_TIMEOUT 等，优先级最高。
	ExtraEnv map[string]string
	// MetaExtra 深合并进 session/new 与 session/load 的 _meta（编排会话的
	// systemPrompt 追加、disallowedTools 收口走这里），与技能隔离的
	// Meta 冲突时以 MetaExtra 为准。
	MetaExtra map[string]any
	// MCPServers 挂载到会话上的 MCP server 清单（协议原样透传，
	// 空为不挂载）。
	MCPServers []any
	// ReplayEvents 为真时，session/load 重放的历史内容照常经 OnEvent 送出，
	// 不做抑制。常规会话必须留 false——历史的正源是转录与重建，重放混进实时
	// 流会让消息重复一遍。只有「我要的就是这条历史」的一次性读取才开
	// （读 codex 子代理独立 thread 的转录就是唯一用例）。
	ReplayEvents bool
}

// Open 拉起 agent、完成握手并新建（或恢复）会话。
// 同一 key 的并发调用只会拉起一个进程，其余等待首个完成。
func (m *Manager) Open(ctx context.Context, opts OpenOptions) (*Session, error) {
	if opts.Cwd == "" || !filepath.IsAbs(opts.Cwd) {
		return nil, fmt.Errorf("acp: cwd must be an absolute path, got %q", opts.Cwd)
	}
	// cwd 必须是已存在的目录，否则 session/new 会以一个不清不楚的错误失败。
	if err := os.MkdirAll(opts.Cwd, 0o755); err != nil {
		return nil, fmt.Errorf("acp: create cwd: %w", err)
	}

	var opening chan struct{}
	for {
		m.mu.Lock()
		if existing, ok := m.sessions[opts.Key]; ok {
			m.mu.Unlock()
			return existing, nil
		}
		if ch, ok := m.opening[opts.Key]; ok {
			// 另一个调用正在握手，等它出结果后重查。
			m.mu.Unlock()
			select {
			case <-ch:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if len(m.sessions) >= m.max {
			m.mu.Unlock()
			return nil, fmt.Errorf("acp: too many open sessions (max %d), close one first", m.max)
		}
		opening = make(chan struct{})
		m.opening[opts.Key] = opening
		m.mu.Unlock()
		break
	}
	defer func() {
		m.mu.Lock()
		delete(m.opening, opts.Key)
		m.mu.Unlock()
		close(opening)
	}()

	sess := &Session{cwd: opts.Cwd, onEvent: opts.OnEvent, replayEvents: opts.ReplayEvents}
	// 刚打开还没跑过调用，从现在起计空闲，否则零值等于「很久以前」立即被回收。
	sess.lastDone.Store(time.Now().UnixNano())
	handler := &sessionHandler{session: sess}

	// 技能隔离在 spawn 前算一次：进程级 env 只能在启动时注入，且方言此刻只
	// 能按命令名判断（agentInfo 要 initialize 后才有，但命令名足以分 claude/
	// codex）。Meta/AdditionalDirs 留给 handshake 的 session/new 与 load 用。
	if m.skillpackDir != "" {
		inj := adapterFor(FlavorOf("", opts.Runtime.Command)).Isolation(IsolationInput{
			SkillpackDir: m.skillpackDir,
			Cwd:          opts.Cwd,
			Home:         os.Getenv("HOME"),
		})
		if len(inj.Env) > 0 {
			env := make(map[string]string, len(opts.Runtime.Env)+len(inj.Env))
			maps.Copy(env, opts.Runtime.Env)
			maps.Copy(env, inj.Env)
			opts.Runtime.Env = env
		}
		sess.injMeta = inj.Meta
		sess.injDirs = inj.AdditionalDirs
	}
	// 上层追加注入在技能隔离之后应用：编排会话要能覆盖 CODEX_HOME 指到
	// 角色专属 home，_meta 冲突同理以上层为准。
	if len(opts.ExtraEnv) > 0 {
		env := make(map[string]string, len(opts.Runtime.Env)+len(opts.ExtraEnv))
		maps.Copy(env, opts.Runtime.Env)
		maps.Copy(env, opts.ExtraEnv)
		opts.Runtime.Env = env
	}
	if len(opts.MetaExtra) > 0 {
		sess.injMeta = mergeMeta(sess.injMeta, opts.MetaExtra)
	}
	sess.mcpServers = opts.MCPServers

	conn, err := Spawn(ctx, opts.Runtime, handler, opts.WireTap)
	if err != nil {
		return nil, err
	}
	sess.conn = conn

	if err := m.handshake(ctx, conn, sess, opts.Runtime.Command, opts.ResumeACPSessionID); err != nil {
		_ = conn.Close()
		return nil, err
	}

	m.mu.Lock()
	m.sessions[opts.Key] = sess
	m.mu.Unlock()

	// 子进程意外退出时把会话摘掉，别留下一条指向死进程的记录。
	go func() {
		<-conn.Done()
		m.mu.Lock()
		if m.sessions[opts.Key] == sess {
			delete(m.sessions, opts.Key)
		}
		m.mu.Unlock()
		if stderr := conn.Stderr(); stderr != "" {
			slog.Warn("acp: agent exited", "key", opts.Key, "stderr", truncate(stderr, 512))
		}
	}()

	return sess, nil
}

func (m *Manager) handshake(ctx context.Context, conn *Conn, sess *Session, command, resumeID string) error {
	ctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()

	// 声明 fs 是一份承诺：agent 会真的调过来，下面的 handler 必须能应答。
	var init InitializeResult
	err := conn.Call(ctx, "initialize", InitializeParams{
		ProtocolVersion: ProtocolVersion,
		ClientCapabilities: ClientCapabilities{
			FS:       FSCapability{ReadTextFile: true, WriteTextFile: true},
			Terminal: false,
			// 声明表单式 elicitation，agent 的交互式提问才会发过来。
			Elicitation: &ElicitationCapability{},
		},
	}, &init)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	if init.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("acp: unsupported protocol version %d (we speak %d)", init.ProtocolVersion, ProtocolVersion)
	}

	// 方言识别用 agent 自报身份 + 启动命令双信号；差异全部由 adapter 吃掉。
	agentName := ""
	if init.AgentInfo != nil {
		agentName = init.AgentInfo.Name
	}
	sess.adapter = adapterFor(FlavorOf(agentName, command))

	// 先尝试恢复：上下文留在 agent 侧同一个 thread 里，进程重启后还能续聊。
	// load 会重放全部历史 update，重放期间的内容事件被抑制，不能混进实时流。
	if resumeID != "" && init.AgentCapabilities.LoadSession {
		sess.setReplaying(true)
		var loaded NewSessionResult
		err := conn.Call(ctx, "session/load", LoadSessionParams{
			SessionID:             resumeID,
			Cwd:                   sess.cwd,
			MCPServers:            sess.mcpList(),
			AdditionalDirectories: sess.injDirs,
			Meta:                  sess.injMeta,
		}, &loaded)
		sess.setReplaying(false)
		if err == nil {
			sess.acpSessionID = resumeID
			if loaded.SessionID != "" {
				sess.acpSessionID = loaded.SessionID
			}
			sess.mu.Lock()
			sess.caps = Caps{
				Modes:         loaded.Modes,
				ConfigOptions: loaded.ConfigOptions,
			}
			sess.mu.Unlock()
			return nil
		}
		slog.Warn("acp: session/load failed, falling back to session/new", "sessionId", resumeID, "err", err)
	}

	var created NewSessionResult
	err = conn.Call(ctx, "session/new", NewSessionParams{
		Cwd:                   sess.cwd,
		MCPServers:            sess.mcpList(),
		AdditionalDirectories: sess.injDirs,
		Meta:                  sess.injMeta,
	}, &created)
	if err != nil {
		return fmt.Errorf("session/new: %w", err)
	}
	if created.SessionID == "" {
		return errors.New("acp: agent returned an empty sessionId")
	}

	sess.acpSessionID = created.SessionID
	sess.mu.Lock()
	sess.caps = Caps{
		Modes:         created.Modes,
		ConfigOptions: created.ConfigOptions,
	}
	sess.mu.Unlock()
	return nil
}

// Get 返回已打开的会话。
func (m *Manager) Get(key string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[key]
	return sess, ok
}

// Idle 返回没有在途调用、且最后一次调用结束早于 olderThan 的会话 key。
// 回收策略归上层；这里只回答「哪些会话可以安全关」。
func (m *Manager) Idle(olderThan time.Duration) []string {
	cutoff := time.Now().Add(-olderThan).UnixNano()

	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []string
	for key, sess := range m.sessions {
		if sess.activeCalls.Load() == 0 && sess.lastDone.Load() < cutoff {
			keys = append(keys, key)
		}
	}
	return keys
}

// DeleteRemote 删除 agent 侧的会话历史（session/delete，两端都支持）。
// 会话开着时直接调；没开着时拉一个临时进程完成——删除是低频操作，
// 几秒的 spawn 可以接受。本地记录与转录的删除不归这里管。
func (m *Manager) DeleteRemote(ctx context.Context, key string, rt Runtime, acpSessionID string) error {
	if acpSessionID == "" {
		return nil
	}

	if sess, ok := m.Get(key); ok {
		var res json.RawMessage
		if err := sess.conn.Call(ctx, "session/delete", DeleteSessionParams{SessionID: acpSessionID}, &res); err != nil {
			return fmt.Errorf("session/delete: %w", err)
		}
		return nil
	}

	conn, err := Spawn(ctx, rt, noopHandler{}, nil)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	var init InitializeResult
	err = conn.Call(ctx, "initialize", InitializeParams{
		ProtocolVersion:    ProtocolVersion,
		ClientCapabilities: ClientCapabilities{},
	}, &init)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	var res json.RawMessage
	if err := conn.Call(ctx, "session/delete", DeleteSessionParams{SessionID: acpSessionID}, &res); err != nil {
		return fmt.Errorf("session/delete: %w", err)
	}
	return nil
}

// noopHandler 供一次性管理连接（远端删除等）使用：不接工作负载，
// 反向调用一律以取消/拒绝应答。
type noopHandler struct{}

func (noopHandler) RequestPermission(context.Context, RequestPermissionParams) (RequestPermissionResult, error) {
	return RequestPermissionResult{Outcome: PermissionOutcome{Outcome: "cancelled"}}, nil
}

func (noopHandler) Elicitation(context.Context, ElicitationParams) (ElicitationResult, error) {
	return ElicitationResult{Action: "cancel"}, nil
}

func (noopHandler) ReadTextFile(context.Context, ReadTextFileParams) (ReadTextFileResult, error) {
	return ReadTextFileResult{}, errors.New("acp: fs is not available on this connection")
}

func (noopHandler) WriteTextFile(context.Context, WriteTextFileParams) error {
	return errors.New("acp: fs is not available on this connection")
}

func (noopHandler) OnUpdate(SessionNotification) {}

// Close 关掉一条会话并回收子进程。
func (m *Manager) Close(key string) error {
	m.mu.Lock()
	sess, ok := m.sessions[key]
	delete(m.sessions, key)
	m.mu.Unlock()
	if !ok {
		return nil
	}
	return sess.conn.Close()
}

// CloseAll 在服务退出时回收全部子进程。
func (m *Manager) CloseAll() {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, sess := range m.sessions {
		sessions = append(sessions, sess)
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()

	for _, sess := range sessions {
		_ = sess.conn.Close()
	}
}
