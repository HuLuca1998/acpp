package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

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

	// injMeta / injDirs 是技能隔离注入的会话参数，session/new 与 session/load
	// 都要带（恢复路径不带会退回机器级 skill）。spawn 前算好，只读。
	injMeta map[string]any
	injDirs []string
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

func (s *Session) emit(ev Event) {
	if s.onEvent != nil {
		s.onEvent(ev)
	}
}

// emitSettings 推送最新的统一设置视图。
func (s *Session) emitSettings() {
	settings := s.adapter.Settings(s.Caps())
	s.emit(Event{Kind: EventSettings, Settings: &settings})
}
