package acp

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
	"sync/atomic"
	"time"
)

// 握手要快；单轮默认不设上限——长程任务跑几个小时是正常使用方式，
// 需要保护的部署自己传 turnTimeout。
const handshakeTimeout = 60 * time.Second

// EventKind 是推给上层的归一化事件类型。
type EventKind string

const (
	EventMessage  EventKind = "message"
	EventThought  EventKind = "thought"
	EventToolCall EventKind = "tool_call"
	EventPlan     EventKind = "plan"
	// EventPermission 表示 agent 在等用户裁决权限；Done 在裁决/超时后发出，
	// 界面收到后应收起卡片。
	EventPermission     EventKind = "permission"
	EventPermissionDone EventKind = "permission_done"
	// EventSettings 在 agent 自行切档/改配置后发出，带最新的统一设置视图。
	EventSettings EventKind = "settings"
	// EventUsage 是上下文用量快照（usage_update 通知）。
	EventUsage EventKind = "usage"
	// EventCommands 是可用斜杠命令清单（全量替换）。
	EventCommands EventKind = "commands"
	// EventElicitation 表示 agent 在等用户作答；Done 在作答/超时后发出，
	// 界面收到后应收起提问卡片。
	EventElicitation     EventKind = "elicitation"
	EventElicitationDone EventKind = "elicitation_done"
	EventTurnEnd         EventKind = "turn_end"
	EventError           EventKind = "error"
)

// Event 是一条归一化事件。工具调用的多条更新共用同一个 ToolCallID，
// 上层必须按它合并，否则界面会出现一堆重复条目。
type Event struct {
	Kind          EventKind       `json:"kind"`
	Text          string          `json:"text,omitempty"`
	ToolCallID    string          `json:"toolCallId,omitempty"`
	Title         string          `json:"title,omitempty"`
	ToolKind      string          `json:"toolKind,omitempty"`
	Status        string          `json:"status,omitempty"`
	RawInput      json.RawMessage `json:"rawInput,omitempty"`
	RawOutput     json.RawMessage `json:"rawOutput,omitempty"`
	Content       json.RawMessage `json:"content,omitempty"`
	Locations     json.RawMessage `json:"locations,omitempty"`
	Entries       json.RawMessage `json:"entries,omitempty"`
	Settings      *Settings       `json:"settings,omitempty"`
	Used          int64           `json:"used,omitempty"`
	Size          int64           `json:"size,omitempty"`
	Commands      []Command       `json:"commands,omitempty"`
	ElicitationID string          `json:"elicitationId,omitempty"`
	// 权限请求：ID 用于回传裁决，Options 是 agent 给的选项。
	// Title/RawInput/Content 只有 claude 带，前端按空值收敛。
	// PlanReview 非空时这是「计划完成」审批，前端渲染专门卡片。
	PermissionID string             `json:"permissionId,omitempty"`
	Options      []PermissionOption `json:"options,omitempty"`
	PlanReview   *PlanReview        `json:"planReview,omitempty"`
	StopReason   StopReason         `json:"stopReason,omitempty"`
	Usage        *Usage             `json:"usage,omitempty"`
	Error        string             `json:"error,omitempty"`
}

// ErrBusy 表示这条会话上还有一个 turn 没结束。
var ErrBusy = errors.New("acp: session is busy with another turn")

// ErrNoSession 表示这条会话当前没有活着的 agent 进程。
var ErrNoSession = errors.New("acp: session not open")

// Session 是一条活着的 ACP 会话：一个 agent 子进程 + 其上的会话状态。
type Session struct {
	conn         *Conn
	acpSessionID string
	cwd          string
	onEvent      func(Event)
	// adapter 按 flavor 把统一设置概念翻译成该 runtime 的协议调用。
	adapter Adapter

	turnMu     sync.Mutex
	mu         sync.Mutex
	turns      int
	cancelTurn context.CancelFunc

	// caps 是 session/new 返回的能力快照，set_* 成功与 agent 主动通知时更新。
	caps Caps
	// commands 是斜杠命令清单快照——通知只在会话建立后推一次，
	// 不存下来的话页面刷新就丢了。
	commands []Command

	// activeCalls 是在途的 prompt/插话调用数，lastDone 是最后一次调用
	// 结束的时刻（unix nano）。空闲回收靠这两样判断能否安全关进程；
	// 权限/提问挂起发生在 prompt 调用之内，天然被 activeCalls 覆盖。
	activeCalls atomic.Int32
	lastDone    atomic.Int64

	// elicitations 是挂起的交互式提问：agent 阻塞在 elicitation/create 上，
	// 等 ResolveElicitation 把用户作答塞进对应 chan。
	elicitationSeq int64
	elicitations   map[string]chan ElicitationResult

	// permissions 是挂起的权限请求：agent 阻塞在 session/request_permission
	// 上，等 ResolvePermission 把用户的裁决塞进对应 chan。
	permissionSeq int64
	permissions   map[string]chan RequestPermissionResult

	// replaying 在 session/load 重放历史期间为真，内容事件被抑制。
	replaying bool
}

func (s *Session) setReplaying(v bool) {
	s.mu.Lock()
	s.replaying = v
	s.mu.Unlock()
}

func (s *Session) isReplaying() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.replaying
}

// Caps 是会话能力的原始快照（modes 与 configOptions），只在 acp 包内部
// 流转——上层消费的是 adapter 从它提取的统一 Settings 视图。
type Caps struct {
	Modes         *Modes         `json:"modes,omitempty"`
	ConfigOptions []ConfigOption `json:"configOptions,omitempty"`
}

// ACPSessionID 是 agent 侧返回的会话标识。
func (s *Session) ACPSessionID() string { return s.acpSessionID }

// Turns 是这条会话已经跑完的轮数。
func (s *Session) Turns() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turns
}

// Caps 返回能力快照的深拷贝，调用方可以随意持有。
func (s *Session) Caps() Caps {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.caps.clone()
}

func (c Caps) clone() Caps {
	out := Caps{}
	if c.Modes != nil {
		modes := *c.Modes
		modes.AvailableModes = append([]Mode(nil), c.Modes.AvailableModes...)
		out.Modes = &modes
	}
	if len(c.ConfigOptions) > 0 {
		out.ConfigOptions = make([]ConfigOption, len(c.ConfigOptions))
		for i, opt := range c.ConfigOptions {
			opt.Options = append([]ConfigOptionValue(nil), opt.Options...)
			out.ConfigOptions[i] = opt
		}
	}
	return out
}

// Manager 持有全部活着的会话，按我们自己的 key（数据库 session id）索引。
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	// opening 标记正在握手的 key，防止并发 Open 各自拉起一个进程后泄漏其一。
	opening       map[string]chan struct{}
	max           int
	promptTimeout time.Duration
}

// NewManager 构造会话池。turnTimeout 是单轮硬上限，<=0 表示不限时
//（默认）——空闲回收以在途调用计数为准，turn 跑多久都不会被误收。
func NewManager(max int, turnTimeout time.Duration) *Manager {
	if max <= 0 {
		max = 8
	}
	return &Manager{
		sessions:      make(map[string]*Session),
		opening:       make(map[string]chan struct{}),
		max:           max,
		promptTimeout: turnTimeout,
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

	sess := &Session{cwd: opts.Cwd, onEvent: opts.OnEvent}
	// 刚打开还没跑过调用，从现在起计空闲，否则零值等于「很久以前」立即被回收。
	sess.lastDone.Store(time.Now().UnixNano())
	handler := &sessionHandler{session: sess}

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
	sess.adapter = adapterFor(flavorOf(agentName, command))

	// 先尝试恢复：上下文留在 agent 侧同一个 thread 里，进程重启后还能续聊。
	// load 会重放全部历史 update，重放期间的内容事件被抑制，不能混进实时流。
	if resumeID != "" && init.AgentCapabilities.LoadSession {
		sess.setReplaying(true)
		var loaded NewSessionResult
		err := conn.Call(ctx, "session/load", LoadSessionParams{
			SessionID:  resumeID,
			Cwd:        sess.cwd,
			MCPServers: []any{},
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
		Cwd:        sess.cwd,
		MCPServers: []any{},
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

// setMode 是 adapter 专用的协议原语：切 session mode 并更新本地快照。
func (s *Session) setMode(ctx context.Context, modeID string) error {
	var result json.RawMessage
	err := s.conn.Call(ctx, "session/set_mode", SetModeParams{
		SessionID: s.acpSessionID,
		ModeID:    modeID,
	}, &result)
	if err != nil {
		return fmt.Errorf("session/set_mode: %w", err)
	}

	s.mu.Lock()
	if s.caps.Modes != nil {
		s.caps.Modes.CurrentModeID = modeID
	}
	s.mu.Unlock()
	return nil
}

// setConfigOption 是 adapter 专用的协议原语。两端的响应都带回全量配置项，
// 直接覆盖快照。
func (s *Session) setConfigOption(ctx context.Context, configID, value string) error {
	var result SetConfigOptionResult
	err := s.conn.Call(ctx, "session/set_config_option", SetConfigOptionParams{
		SessionID: s.acpSessionID,
		ConfigID:  configID,
		Value:     value,
	}, &result)
	if err != nil {
		return fmt.Errorf("session/set_config_option: %w", err)
	}

	s.mu.Lock()
	if len(result.ConfigOptions) > 0 {
		s.caps.ConfigOptions = result.ConfigOptions
	} else {
		// 响应没带全量时至少把本地这一项改掉。
		for i := range s.caps.ConfigOptions {
			if s.caps.ConfigOptions[i].ID == configID {
				s.caps.ConfigOptions[i].CurrentValue = value
			}
		}
	}
	s.mu.Unlock()
	return nil
}

// Commands 返回会话的斜杠命令清单快照。
func (m *Manager) Commands(key string) []Command {
	sess, ok := m.Get(key)
	if !ok {
		return nil
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return append([]Command(nil), sess.commands...)
}

// Settings 返回会话设置的统一视图。
func (m *Manager) Settings(key string) (Settings, error) {
	sess, ok := m.Get(key)
	if !ok {
		return Settings{}, ErrNoSession
	}
	return sess.adapter.Settings(sess.Caps()), nil
}

// Apply 逐项应用设置变更，翻译工作由会话的 adapter 完成；
// 任何一项失败立刻返回，已生效的项不回滚（与用户逐个点选的语义一致）。
func (m *Manager) Apply(ctx context.Context, key string, patch SettingsPatch) (Settings, error) {
	sess, ok := m.Get(key)
	if !ok {
		return Settings{}, ErrNoSession
	}
	ad := sess.adapter

	if patch.Model != nil {
		if err := ad.SetModel(ctx, sess, *patch.Model); err != nil {
			return Settings{}, err
		}
	}
	if patch.Effort != nil {
		if err := ad.SetEffort(ctx, sess, *patch.Effort); err != nil {
			return Settings{}, err
		}
	}
	if patch.Fast != nil {
		if err := ad.SetFast(ctx, sess, *patch.Fast); err != nil {
			return Settings{}, err
		}
	}
	// plan 必须先于 level：claude 退出 plan 会回落到默认档，level 放最后
	// 才能保证 {plan:false, level:x} 这类组合以 x 收尾。
	if patch.Plan != nil {
		if err := ad.SetPlan(ctx, sess, *patch.Plan); err != nil {
			return Settings{}, err
		}
	}
	if patch.Level != nil {
		if err := ad.SetAccessLevel(ctx, sess, *patch.Level); err != nil {
			return Settings{}, err
		}
	}
	return ad.Settings(sess.Caps()), nil
}

// Get 返回已打开的会话。
func (m *Manager) Get(key string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[key]
	return sess, ok
}

// Prompt 发一轮对话，阻塞到整轮结束。turn 期间的内容通过 OnEvent 实时推出去。
//
// ACP 会话自带上下文，所以第二轮起只需要发用户这一句，不要重复系统提示。
func (m *Manager) Prompt(ctx context.Context, key string, blocks []ContentBlock) (PromptResult, error) {
	sess, ok := m.Get(key)
	if !ok {
		return PromptResult{}, ErrNoSession
	}

	// 一条会话同一时刻只允许一个 turn。
	if !sess.turnMu.TryLock() {
		return PromptResult{}, ErrBusy
	}
	defer sess.turnMu.Unlock()

	turnCtx, cancel := m.turnContext(ctx)
	defer cancel()

	sess.mu.Lock()
	sess.cancelTurn = cancel
	sess.mu.Unlock()

	defer func() {
		sess.mu.Lock()
		sess.cancelTurn = nil
		sess.turns++
		sess.mu.Unlock()
	}()

	result, err := sess.promptCall(turnCtx, blocks)
	if err != nil {
		// 超时后只 reject 是不够的：agent 还在后台跑、还在烧钱、还可能继续改文件。
		if errors.Is(turnCtx.Err(), context.DeadlineExceeded) {
			m.cancelTurn(sess)
		}
		sess.emit(Event{Kind: EventError, Error: err.Error()})
		return PromptResult{}, err
	}

	sess.emit(Event{Kind: EventTurnEnd, StopReason: result.StopReason, Usage: result.Usage})
	return result, nil
}

// promptCall 是裸的 session/prompt 往返，不做 turn 排他——
// Prompt 与 claude 的插话（promptQueueing 排队）共用。
func (s *Session) promptCall(ctx context.Context, blocks []ContentBlock) (PromptResult, error) {
	s.activeCalls.Add(1)
	defer func() {
		s.activeCalls.Add(-1)
		s.lastDone.Store(time.Now().UnixNano())
	}()

	var result PromptResult
	err := s.conn.Call(ctx, "session/prompt", PromptParams{
		SessionID: s.acpSessionID,
		Prompt:    blocks,
	}, &result)
	return result, err
}

// steeringCall 把消息注入正在跑的 turn（_session/steering）。
func (s *Session) steeringCall(ctx context.Context, blocks []ContentBlock) error {
	s.activeCalls.Add(1)
	defer func() {
		s.activeCalls.Add(-1)
		s.lastDone.Store(time.Now().UnixNano())
	}()

	var result json.RawMessage
	err := s.conn.Call(ctx, "_session/steering", SteeringParams{
		SessionID: s.acpSessionID,
		Prompt:    blocks,
	}, &result)
	if err != nil {
		return fmt.Errorf("_session/steering: %w", err)
	}
	return nil
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

// Interject 在 turn 进行中插入一条用户消息，翻译交给 adapter。
// followUp=true 表示这条消息产生了自己独立的一轮（结果已含在返回值里，
// 并已发出 turn_end 事件）；false 表示内容并入当前轮、由当前轮统一收尾。
func (m *Manager) Interject(ctx context.Context, key string, blocks []ContentBlock) (PromptResult, bool, error) {
	sess, ok := m.Get(key)
	if !ok {
		return PromptResult{}, false, ErrNoSession
	}

	ctx, cancel := m.turnContext(ctx)
	defer cancel()

	result, followUp, err := sess.adapter.Interject(ctx, sess, blocks)
	if err != nil {
		return PromptResult{}, false, err
	}
	if followUp {
		sess.emit(Event{Kind: EventTurnEnd, StopReason: result.StopReason, Usage: result.Usage})
	}
	return result, followUp, nil
}

// Cancel 中止会话上正在跑的 turn。
func (m *Manager) Cancel(key string) error {
	sess, ok := m.Get(key)
	if !ok {
		return ErrNoSession
	}
	m.cancelTurn(sess)
	return nil
}

func (m *Manager) cancelTurn(sess *Session) {
	_ = sess.conn.Notify("session/cancel", CancelParams{SessionID: sess.acpSessionID})
	sess.mu.Lock()
	cancel := sess.cancelTurn
	sess.mu.Unlock()
	if cancel != nil {
		cancel()
	}
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

func (s *Session) emit(ev Event) {
	if s.onEvent != nil {
		s.onEvent(ev)
	}
}

// sessionHandler 实现 agent 的反向调用。
type sessionHandler struct {
	session *Session
}

// OnUpdate 把 session/update 归一化成 Event。每一类都要有去处——收到即丢是允许的，
// 但必须是个决定，不是遗漏。
func (h *sessionHandler) OnUpdate(n SessionNotification) {
	u := n.Update

	// session/load 的历史重放不进实时流：转录与重建才是历史的正源。
	if h.session.isReplaying() {
		switch u.SessionUpdate {
		case UpdateAgentMessageChunk, UpdateAgentThoughtChunk,
			UpdateUserMessageChunk, UpdateToolCall, UpdateToolCallUpdate,
			UpdatePlan, UpdateUsage:
			return
		}
	}

	switch u.SessionUpdate {
	case UpdateAgentMessageChunk:
		h.session.emit(Event{Kind: EventMessage, Text: u.Text()})

	case UpdateAgentThoughtChunk:
		h.session.emit(Event{Kind: EventThought, Text: u.Text()})

	case UpdateToolCall, UpdateToolCallUpdate:
		h.session.emit(Event{
			Kind:       EventToolCall,
			ToolCallID: u.ToolCallID,
			Title:      u.Title,
			ToolKind:   u.Kind,
			Status:     u.Status,
			RawInput:   u.RawInput,
			RawOutput:  u.RawOutput,
			Content:    u.Content,
			Locations:  u.Locations,
		})

	case UpdatePlan:
		h.session.emit(Event{Kind: EventPlan, Entries: u.Entries})

	case UpdateCurrentMode:
		// agent 会自己切档（如 claude 的 ExitPlanMode），不跟着更新界面上
		// 显示的档位就与实际不符。推统一 Settings 视图，翻译交给 adapter。
		modeID := u.EffectiveModeID()
		h.session.mu.Lock()
		if h.session.caps.Modes != nil && modeID != "" {
			h.session.caps.Modes.CurrentModeID = modeID
		}
		h.session.mu.Unlock()
		h.session.emitSettings()

	case UpdateConfigOption:
		// 配置项变化（比如 agent 通过斜杠命令切了协作模式），带全量新配置。
		if len(u.ConfigOptions) > 0 {
			h.session.mu.Lock()
			h.session.caps.ConfigOptions = u.ConfigOptions
			h.session.mu.Unlock()
			h.session.emitSettings()
		}

	case UpdateUsage:
		// 上下文用量快照。size 语义两端有出入（窗口大小 vs 水位），
		// 按占比展示两端都成立；claude 独有的 cost 按交集规范不透出。
		h.session.emit(Event{Kind: EventUsage, Used: u.Used, Size: u.Size})

	case UpdateUserMessageChunk:
		// 只在 session/load 重放历史时出现，这里不重放，忽略。

	case UpdateAvailableCommands:
		// 全量替换，供输入框做 "/" 补全；发送时就是普通文本，两端都认。
		h.session.mu.Lock()
		h.session.commands = u.AvailableCommands
		h.session.mu.Unlock()
		h.session.emit(Event{Kind: EventCommands, Commands: u.AvailableCommands})

	case UpdateSessionInfo:
		// claude 带自动标题（与本项目「首条消息简写」策略重复）、codex 带
		// threadStatus，都无用途，明确丢弃。

	default:
		slog.Debug("acp: unhandled session update", "kind", u.SessionUpdate)
	}
}

// emitSettings 推送最新的统一设置视图。
func (s *Session) emitSettings() {
	settings := s.adapter.Settings(s.Caps())
	s.emit(Event{Kind: EventSettings, Settings: &settings})
}

// Elicitation 把 agent 的交互式提问推给界面并阻塞等用户作答。
// ctx 超时（conn 层给了 prompt 同级的时长）或连接关闭时以 cancel 收场，
// agent 侧会把这轮提问当作放弃。
func (h *sessionHandler) Elicitation(ctx context.Context, p ElicitationParams) (ElicitationResult, error) {
	s := h.session

	// 只支持表单模式；url 之类的其它模式直接取消，agent 会自行回退。
	if p.Mode != "form" {
		return ElicitationResult{Action: "cancel"}, nil
	}

	ch := make(chan ElicitationResult, 1)
	s.mu.Lock()
	s.elicitationSeq++
	id := fmt.Sprintf("e%d", s.elicitationSeq)
	if s.elicitations == nil {
		s.elicitations = make(map[string]chan ElicitationResult)
	}
	s.elicitations[id] = ch
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.elicitations, id)
		s.mu.Unlock()
		// 无论作答还是超时，都让界面把提问卡片收起来。
		s.emit(Event{Kind: EventElicitationDone, ElicitationID: id})
	}()

	s.emit(Event{
		Kind:          EventElicitation,
		ElicitationID: id,
		ToolCallID:    p.ToolCallID,
		Text:          p.Message,
		RawInput:      p.RequestedSchema,
	})

	select {
	case res := <-ch:
		return res, nil
	case <-ctx.Done():
		return ElicitationResult{Action: "cancel"}, nil
	}
}

// ResolveElicitation 把用户对交互式提问的作答回给阻塞中的 agent。
func (m *Manager) ResolveElicitation(key, elicitationID string, result ElicitationResult) error {
	sess, ok := m.Get(key)
	if !ok {
		return ErrNoSession
	}

	sess.mu.Lock()
	ch, pending := sess.elicitations[elicitationID]
	if pending {
		delete(sess.elicitations, elicitationID)
	}
	sess.mu.Unlock()

	if !pending {
		return fmt.Errorf("elicitation %s is no longer pending", elicitationID)
	}
	ch <- result
	return nil
}

// RequestPermission 把权限请求挂起交给用户裁决——runtime 只在当前档位
// 认为需要确认时才会问（codex agent 档不问、claude acceptEdits 不问编辑），
// 问了就该给用户看。ctx 超时或连接关闭时以 cancelled 收场。
// claude 的 ExitPlanMode 也走这条通道（选项即模式切换），交给用户选正合适。
func (h *sessionHandler) RequestPermission(ctx context.Context, p RequestPermissionParams) (RequestPermissionResult, error) {
	s := h.session

	ch := make(chan RequestPermissionResult, 1)
	s.mu.Lock()
	s.permissionSeq++
	id := fmt.Sprintf("p%d", s.permissionSeq)
	if s.permissions == nil {
		s.permissions = make(map[string]chan RequestPermissionResult)
	}
	s.permissions[id] = ch
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.permissions, id)
		s.mu.Unlock()
		// 无论裁决还是超时，都让界面把卡片收起来。
		s.emit(Event{Kind: EventPermissionDone, PermissionID: id})
	}()

	s.emit(Event{
		Kind:         EventPermission,
		PermissionID: id,
		ToolCallID:   p.ToolCall.ToolCallID,
		ToolKind:     p.ToolCall.Kind,
		Title:        p.ToolCall.Title,
		RawInput:     p.ToolCall.RawInput,
		Content:      p.ToolCall.Content,
		Options:      p.Options,
		// 「计划完成」审批由 adapter 识别并翻译成统一视图。
		PlanReview: s.adapter.PlanReview(p),
	})

	select {
	case res := <-ch:
		return res, nil
	case <-ctx.Done():
		return RequestPermissionResult{Outcome: PermissionOutcome{Outcome: "cancelled"}}, nil
	}
}

// ResolvePermission 把用户对权限请求的裁决回给阻塞中的 agent。
// optionID 为空表示用户取消（agent 侧按放弃处理）。
func (m *Manager) ResolvePermission(key, permissionID, optionID string) error {
	sess, ok := m.Get(key)
	if !ok {
		return ErrNoSession
	}

	sess.mu.Lock()
	ch, pending := sess.permissions[permissionID]
	if pending {
		delete(sess.permissions, permissionID)
	}
	sess.mu.Unlock()

	if !pending {
		return fmt.Errorf("permission %s is no longer pending", permissionID)
	}

	result := RequestPermissionResult{Outcome: PermissionOutcome{Outcome: "cancelled"}}
	if optionID != "" {
		result = RequestPermissionResult{
			Outcome: PermissionOutcome{Outcome: "selected", OptionID: optionID},
		}
	}
	ch <- result
	return nil
}

// ReadTextFile 代理 agent 的读文件请求，路径限制在会话 cwd 内。
func (h *sessionHandler) ReadTextFile(_ context.Context, p ReadTextFileParams) (ReadTextFileResult, error) {
	path, err := h.session.guardPath(p.Path)
	if err != nil {
		return ReadTextFileResult{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ReadTextFileResult{}, fmt.Errorf("read %s: %w", p.Path, err)
	}

	content := string(data)
	if p.Line != nil || p.Limit != nil {
		content = sliceLines(content, p.Line, p.Limit)
	}
	return ReadTextFileResult{Content: content}, nil
}

// WriteTextFile 代理 agent 的写文件请求，路径限制在会话 cwd 内。
func (h *sessionHandler) WriteTextFile(_ context.Context, p WriteTextFileParams) error {
	path, err := h.session.guardPath(p.Path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent of %s: %w", p.Path, err)
	}
	if err := os.WriteFile(path, []byte(p.Content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", p.Path, err)
	}
	return nil
}

// guardPath 把路径解析到 canonical 形式并确认它在 cwd 之内。
//
// 这条防线只覆盖走 fs 代理的操作：claude 走，codex 用自带 shell 完全不走。
// 所以它是纵深防御的一层，不是全部。
func (s *Session) guardPath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.cwd, path)
	}
	clean := filepath.Clean(path)

	root, err := filepath.EvalSymlinks(s.cwd)
	if err != nil {
		root = filepath.Clean(s.cwd)
	}
	// 目标可能还不存在（写新文件），所以对父目录求 canonical 路径。
	resolvedDir, err := filepath.EvalSymlinks(filepath.Dir(clean))
	if err != nil {
		resolvedDir = filepath.Dir(clean)
	}
	resolved := filepath.Join(resolvedDir, filepath.Base(clean))

	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside the session working directory", path)
	}
	return resolved, nil
}

func sliceLines(content string, line, limit *int) string {
	lines := strings.Split(content, "\n")
	start := 0
	if line != nil && *line > 0 {
		start = *line - 1
	}
	if start >= len(lines) {
		return ""
	}
	end := len(lines)
	if limit != nil && *limit > 0 && start+*limit < end {
		end = start + *limit
	}
	return strings.Join(lines[start:end], "\n")
}
