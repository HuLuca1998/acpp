// Package ask 是「别的 AI 来问一句」的同步面（adr-022）：本机的 claude / codex
// CLI 经 POST /api/ask 把问题交给 acpp，acpp 替它开会话、发一轮、等到轮末，
// 把回答正文一次性交回。调用方只认「问一句、拿答案」这一个原语，会话、
// SSE、权限卡片全在这里消化掉。
package ask

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

// Input 是 /api/ask 的请求体。字段刻意少：调用方是脚本，cwd 之外的
// 内部概念（agent id、设置视图）一概不外露。
type Input struct {
	// Agent 是被问的一方，按内置 agent 的名字认（claude / codex）。续聊时可省。
	Agent string `json:"agent,omitempty"`
	// Cwd 是对方干活的目录。新会话必填；续聊时忽略（会话已经有目录了）。
	Cwd string `json:"cwd,omitempty"`
	// Prompt 是问题正文。
	Prompt string `json:"prompt"`
	// Title 是新会话的标题，可省——省了就按 prompt 首句自动简写。调用方往
	// prompt 前面注入了角色 header 时，首句是「你是一名严格的审查者……」，
	// 侧栏里一排全是这个，认不出哪条是哪条；给它一个能认的名字。
	Title string `json:"title,omitempty"`
	// Level 是权限档：safe（默认，只读）/ auto-edit / full。续聊时省略表示不动。
	Level acp.AccessLevel `json:"level,omitempty"`
	// Thread 是要续聊的会话 id；省略即新开一条。
	Thread uint `json:"thread,omitempty"`
}

// Result 是一轮问答的结果。Text 是这一轮 agent 说的全部正文（工具调用
// 之间的段落按顺序拼起来）；StopReason 只在不是正常说完时才值得看。
type Result struct {
	Thread     uint   `json:"thread"`
	Text       string `json:"text"`
	StopReason string `json:"stopReason,omitempty"`
}

// Service 把一次问答翻译成会话操作：AgentService / SessionService /
// ChatService 串成一条同步路径。
type Service struct {
	agents   *service.AgentService
	sessions *service.SessionService
	chat     *service.ChatService
	// timeout 是一轮问答的最长等待。到点就中止那一轮并报错，不让一个
	// 跑飞的 agent 把调用方的 HTTP 连接吊死。
	timeout time.Duration

	// inflight 是正在问答中的会话。同一条会话第二个问题进来要拒绝，而
	// 「查 TurnActive → Send」之间有窗口：两个请求都能过闸，后到的会被
	// Send 当插话并入前一轮，两边等到同一个轮末、拿回同一段文本。闸要
	// 在本包里持锁做。
	mu       sync.Mutex
	inflight map[uint]struct{}
}

// ErrTimeout 表示一轮在期限内没有跑完。单独成哨兵是为了让 HTTP 层给 504
// 而不是 500：调用方要能分辨「对方太慢」与「服务坏了」。
var ErrTimeout = errors.New("ask: turn did not finish before the deadline")

// ErrInterjected 表示等答案的途中，界面上的人在这条会话里插了话。包着
// acp.ErrBusy 复用 409：对调用方而言语义一样——这条 thread 现在不归你。
var ErrInterjected = fmt.Errorf("%w: turn taken over by a person in the UI", acp.ErrBusy)

// DefaultTimeout 是一轮问答的默认上限。审查 / 咨询几分钟就完，全权干活
// 的一轮可能要几十分钟，一小时是对后者的容忍，不是对前者的期望。
const DefaultTimeout = time.Hour

// livenessPoll 是「轮还在不在跑」的兜底探查间隔：事件流对慢订阅者会丢事件
// （见 stream.Broker.Publish），turn_done 一旦被丢，光等流就只能等到超时。
const livenessPoll = 5 * time.Second

func NewService(agents *service.AgentService, sessions *service.SessionService, chat *service.ChatService, timeout time.Duration) *Service {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Service{agents: agents, sessions: sessions, chat: chat, timeout: timeout,
		inflight: make(map[uint]struct{})}
}

// Ask 跑一轮问答并阻塞到轮末。ctx 一取消（调用方挂断 / 超时）就中止那一轮：
// 没人等的回答不必再算下去。
func (s *Service) Ask(ctx context.Context, scope service.Scope, in Input) (*Result, error) {
	if strings.TrimSpace(in.Prompt) == "" {
		return nil, fmt.Errorf("%w: prompt is required", service.ErrInvalid)
	}
	switch in.Level {
	case "", acp.AccessSafe, acp.AccessAutoEdit, acp.AccessFull:
	default:
		return nil, fmt.Errorf("%w: level must be one of safe / auto-edit / full", service.ErrInvalid)
	}

	sessionID, agent, err := s.resolveSession(ctx, scope, in)
	if err != nil {
		return nil, err
	}
	created := agent != nil
	release, err := s.acquire(sessionID)
	if err != nil {
		return nil, err
	}
	defer release()

	// 新会话默认只读：问一句不该顺手让对方能改文件。续聊不带 level 就沿用。
	patch := acp.SettingsPatch{}
	if in.Level != "" {
		patch.Level = &in.Level
	} else if created {
		level := acp.AccessSafe
		patch.Level = &level
	}
	// 新会话还要拨到配置页里给 AI 协作定的模型与思考深度（空=沿用默认）。
	if created {
		if m := strings.TrimSpace(agent.AskModel); m != "" {
			patch.Model = &m
		}
		if e := acp.Effort(strings.TrimSpace(agent.AskEffort)); e != "" {
			patch.Effort = &e
		}
	}

	stop, err := s.runTurn(ctx, sessionID, patch, in.Prompt)
	if err != nil {
		// 刚为这一问开的会话一轮都没跑成，留着只是侧栏里一条空记录与一个
		// 挂着的子进程。唯独「被人接手」不能收：那条会话此刻正被人用着，
		// 收掉等于把会话从他手底下删了。
		if created && !errors.Is(err, ErrInterjected) {
			s.discard(ctx, scope, sessionID)
		}
		return nil, err
	}

	text, err := s.replyText(sessionID)
	if err != nil {
		return nil, err
	}
	out := &Result{Thread: sessionID, Text: text}
	if !stop.OK() {
		out.StopReason = string(stop)
	}
	return out, nil
}

// acquire 把会话标成问答中；已在问答中、或界面那边正有一轮在跑，都算忙。
func (s *Service) acquire(sessionID uint) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, busy := s.inflight[sessionID]; busy || s.chat.TurnActive(sessionID) {
		return nil, fmt.Errorf("thread %d: %w", sessionID, acp.ErrBusy)
	}
	s.inflight[sessionID] = struct{}{}
	return func() {
		s.mu.Lock()
		delete(s.inflight, sessionID)
		s.mu.Unlock()
	}, nil
}

// runTurn 拨设置、订阅、发送、等轮末。超时从这里就开始算：拉起子进程
// 与握手也可能卡住，只给等待段设限的话，前面那段能无限期占着会话闸门。
func (s *Service) runTurn(parent context.Context, sessionID uint, patch acp.SettingsPatch, prompt string) (acp.StopReason, error) {
	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()

	// 裁决权限要知道**实际生效**的档位：续聊不带 level 时沿用会话现状，
	// 那可能是 full——按空值当非 full 处理会把该放的请求拒掉。
	var level acp.AccessLevel
	if patch.Level != nil || patch.Model != nil || patch.Effort != nil {
		settings, err := s.chat.ApplySettings(ctx, sessionID, patch)
		if err != nil {
			return "", err
		}
		level = settings.CurrentLevel
	} else {
		view, err := s.chat.Open(ctx, sessionID)
		if err != nil {
			return "", err
		}
		if view.Settings != nil {
			level = view.Settings.CurrentLevel
		}
	}

	// 先订阅再发送：订阅晚于 Send 会漏掉轮首的事件。
	events, unsubscribe := s.chat.Subscribe(sessionID)
	defer unsubscribe()

	sent, err := s.chat.Send(ctx, sessionID, service.SendInput{Content: prompt})
	if err != nil {
		return "", err
	}
	return s.waitTurn(ctx, sessionID, level, sent.ID, events)
}

// waitTurn 消费事件流直到这一轮收尾。路上替没在场的人做两件事：权限请求
// 按权限档裁决，交互式提问一律取消——问的人是脚本，没有第二个人能回答。
//
// sentID 是自己那条 user_message 的 id。等待途中出现**别的** user_message，
// 唯一解释是界面上的人在这条会话里插了话（user_message 全项目只在 Send
// 里发）：插话会并入当前轮或排成下一轮，而取答案的锚点是「最后一条用户
// 消息之后的正文」——锚点一滑，脚本拿回的就是人那一问的答案，还分辨不出。
// 所以立刻退出并回 ErrInterjected；不 Cancel 那一轮，会话与回答留给人。
func (s *Service) waitTurn(ctx context.Context, sessionID uint, level acp.AccessLevel, sentID uint, events <-chan service.StreamEvent) (acp.StopReason, error) {
	var stop acp.StopReason
	// started 以自己那条 user_message 为准：Send 之前广播里若还留着别的事件
	//（刚拨设置推来的 settings、上一轮重放的残留），不该被当成这一轮的。
	started := false

	// handle 消化一条事件；done 为真表示这一轮到此为止（正常收尾或出错）。
	handle := func(ev service.StreamEvent) (done bool, err error) {
		switch ev.Kind {
		case "user_message":
			switch {
			case ev.Message != nil && ev.Message.ID == sentID:
				started = true
			case started:
				return true, fmt.Errorf("thread %d: %w", sessionID, ErrInterjected)
			}
		case "permission":
			s.decidePermission(sessionID, level, ev)
		case "elicitation":
			if err := s.chat.ResolveElicitation(sessionID, ev.ElicitationID, "cancel", nil); err != nil {
				slog.Warn("ask: cancel elicitation", "session", sessionID, "err", err)
			}
		case "turn_end":
			stop = acp.StopReason(ev.StopReason)
		case "error":
			if started {
				return true, fmt.Errorf("thread %d: %s", sessionID, ev.Error)
			}
		case "turn_done":
			if started {
				return true, nil
			}
		}
		return false, nil
	}

	poll := time.NewTicker(livenessPoll)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			s.cancelTurn(sessionID)
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return "", fmt.Errorf("thread %d: %w", sessionID, ErrTimeout)
			}
			return "", ctx.Err()

		case <-poll.C:
			// 轮已经不在跑而 turn_done 没到：事件被丢了，按收尾处理。收尾前
			// 先把通道里已经排着的事件吃完——轮末的 error / turn_end 可能
			// 就在里面，凭「不在跑」直接返回会把失败报成成功、丢掉结束原因。
			if !started || s.chat.TurnActive(sessionID) {
				continue
			}
			for {
				select {
				case ev, ok := <-events:
					if !ok {
						return stop, nil
					}
					if done, err := handle(ev); done {
						return stop, err
					}
				default:
					return stop, nil
				}
			}

		case ev, ok := <-events:
			if !ok {
				return "", fmt.Errorf("thread %d: event stream closed", sessionID)
			}
			if done, err := handle(ev); done {
				return stop, err
			}
		}
	}
}

// cancelTurn 在调用方挂断或超时后停掉那一轮。Send 只是把轮丢进 goroutine，
// 轮真正登记到 acp 层要晚几毫秒——挂断恰好落在这个缝里时 Cancel 会落空，
// agent 就对着空气继续干。所以先等它登记上（最多几秒），再取消。
func (s *Service) cancelTurn(sessionID uint) {
	deadline := time.Now().Add(3 * time.Second)
	for !s.chat.TurnActive(sessionID) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if err := s.chat.Cancel(sessionID); err != nil {
		slog.Warn("ask: cancel turn", "session", sessionID, "err", err)
	}
}

// decidePermission 替用户点权限卡：full 档放行一次，其余档拒绝。
//
// 只选 *_once：allow_always 会在 agent 侧落一条持久规则，这条会话之后被
// 人接手时那规则还在。拒绝也一样只拒这一次——agent 会带着「被拒」继续
// 作答，比整轮卡死好。选项里找不到对应种类就按取消处理（optionID 为空）。
func (s *Service) decidePermission(sessionID uint, level acp.AccessLevel, ev service.StreamEvent) {
	want := "reject_once"
	if level == acp.AccessFull {
		want = "allow_once"
	}
	optionID := ""
	for _, o := range ev.Options {
		if o.Kind == want {
			optionID = o.OptionID
			break
		}
	}
	if err := s.chat.ResolvePermission(sessionID, ev.PermissionID, optionID); err != nil {
		slog.Warn("ask: resolve permission", "session", sessionID, "err", err)
	}
}

// resolveSession 找到要发问的会话：带 thread 就校验归属后复用，否则按
// agent 名字新开一条并打上 ask 来源。新开的会话连 agent 记录一起返回
// （上层据此拨模型与思考深度）；复用的返回 nil。
func (s *Service) resolveSession(ctx context.Context, scope service.Scope, in Input) (uint, *model.Agent, error) {
	if in.Thread != 0 {
		ref, err := s.sessions.Guard(ctx, scope, in.Thread)
		if err != nil {
			return 0, nil, err
		}
		return ref.ID, nil, nil
	}

	name := strings.ToLower(strings.TrimSpace(in.Agent))
	if name == "" {
		return 0, nil, fmt.Errorf("%w: agent is required for a new thread", service.ErrInvalid)
	}
	if strings.TrimSpace(in.Cwd) == "" {
		return 0, nil, fmt.Errorf("%w: cwd is required for a new thread", service.ErrInvalid)
	}
	agents, err := s.agents.List(ctx)
	if err != nil {
		return 0, nil, err
	}
	var agent *model.Agent
	for i := range agents {
		if strings.ToLower(agents[i].Name) == name {
			agent = &agents[i]
			break
		}
	}
	if agent == nil {
		return 0, nil, fmt.Errorf("agent %q: %w", in.Agent, service.ErrNotFound)
	}

	view, err := s.sessions.Create(ctx, scope, service.SessionInput{
		AgentID: agent.ID,
		Cwd:     in.Cwd,
		Title:   strings.TrimSpace(in.Title),
		Origin:  model.SessionOriginAsk,
	})
	if err != nil {
		return 0, nil, err
	}
	return view.ID, agent, nil
}

// discard 收掉一条没跑成的新会话：先回收子进程再删记录，顺序与
// sessionHandler.remove 一致。用独立的 ctx——调用方多半已经挂断了。
func (s *Service) discard(parent context.Context, scope service.Scope, sessionID uint) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 10*time.Second)
	defer cancel()
	if err := s.chat.Destroy(ctx, sessionID); err != nil {
		slog.Warn("ask: destroy failed session", "session", sessionID, "err", err)
	}
	if err := s.sessions.Delete(ctx, scope, sessionID); err != nil {
		slog.Warn("ask: delete failed session", "session", sessionID, "err", err)
	}
}

// replyText 从转录重建结果里取这一轮的回答。走重建而不是攒流式分片：
// 分片在订阅者慢时会被丢，转录才是事实源。
func (s *Service) replyText(sessionID uint) (string, error) {
	all, _, err := s.chat.Messages(sessionID, 0, 0)
	if err != nil {
		return "", err
	}
	return ReplyAfterLastPrompt(all), nil
}

// ReplyAfterLastPrompt 取最后一条用户提问之后的全部 agent 正文，段落间空一行。
// 思考、工具调用、计划这些不是「回答」，一律不算。
func ReplyAfterLastPrompt(all []model.Message) string {
	start := 0
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Role == model.RoleUser {
			start = i + 1
			break
		}
	}
	var parts []string
	for _, m := range all[start:] {
		if m.Role == model.RoleAgent && m.Kind == model.KindText {
			if text := strings.TrimSpace(m.Content); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}
