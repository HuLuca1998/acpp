package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
	"acpp/server/internal/transcript"
)

// ErrBusy 表示该会话上还有一轮没结束，由 HTTP 层翻译成 409。
var ErrBusy = errors.New("session is busy with another turn")

// deriveTitle 从首条消息取首行并截短，作为会话的自动标题。
func deriveTitle(text string) string {
	line := strings.TrimSpace(text)
	if i := strings.IndexAny(line, "\r\n"); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	const maxRunes = 24
	runes := []rune(line)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "…"
	}
	return line
}

// StreamEvent 是推给浏览器的一条 SSE 事件。
type StreamEvent struct {
	Kind string `json:"kind"`
	// Seq 在同一条会话内单调递增，供前端去重与断线续传。
	Seq int `json:"seq"`

	Text          string             `json:"text,omitempty"`
	ToolCallID    string             `json:"toolCallId,omitempty"`
	Title         string             `json:"title,omitempty"`
	ToolKind      string             `json:"toolKind,omitempty"`
	Status        string             `json:"status,omitempty"`
	RawInput      json.RawMessage    `json:"rawInput,omitempty"`
	RawOutput     json.RawMessage    `json:"rawOutput,omitempty"`
	ModeID        string             `json:"modeId,omitempty"`
	ConfigOptions []acp.ConfigOption `json:"configOptions,omitempty"`
	ElicitationID string             `json:"elicitationId,omitempty"`
	StopReason    string             `json:"stopReason,omitempty"`
	Error         string             `json:"error,omitempty"`

	// Message 在一条消息落库后带上完整记录，前端用它替换流式占位。
	Message *model.Message `json:"message,omitempty"`
}

// ChatService 把 ACP 会话、转录留存与浏览器推流接在一起。
// 对话内容唯一的持久化形态是转录 JSONL；数据库只保管会话元数据。
type ChatService struct {
	db          *gorm.DB
	sessions    *SessionService
	manager     *acp.Manager
	transcripts *transcript.Store

	mu      sync.Mutex
	brokers map[uint]*broker
}

func NewChatService(db *gorm.DB, sessions *SessionService, manager *acp.Manager, transcripts *transcript.Store) *ChatService {
	return &ChatService{
		db:          db,
		sessions:    sessions,
		manager:     manager,
		transcripts: transcripts,
		brokers:     make(map[uint]*broker),
	}
}

// Open 为一条已存在的数据库会话拉起 agent 并完成 ACP 握手。
// 会话已经开着时直接返回，重复调用是安全的。
func (s *ChatService) Open(ctx context.Context, sessionID uint) (*SessionView, error) {
	view, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	key := sessionKey(sessionID)
	if sess, ok := s.manager.Get(key); ok {
		caps := sess.Caps()
		view.Caps = &caps
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

	br := s.brokerFor(sessionID)
	sess, err := s.manager.Open(ctx, acp.OpenOptions{
		Key:     key,
		Runtime: acp.RuntimeFor(agent.Command, agent.Args, agent.Env),
		Cwd:     cwd,
		OnEvent: func(ev acp.Event) { s.handleEvent(br, ev) },
		// 全量线级消息进转录，这是对话内容唯一的持久化。
		WireTap: func(dir string, msg json.RawMessage) { s.transcripts.Append(key, dir, msg) },
		// 进程重启后优先恢复 agent 侧的同一条会话，保住上下文。
		ResumeACPSessionID: view.ACPSessionID,
	})
	if err != nil {
		s.markSessionError(sessionID, err)
		return nil, fmt.Errorf("open acp session: %w", err)
	}

	updates := map[string]any{
		"acp_session_id": sess.ACPSessionID(),
		"state":          model.SessionActive,
		"cwd":            cwd,
	}
	if err := s.db.WithContext(ctx).Model(&model.Session{}).Where("id = ?", sessionID).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("save acp session id: %w", err)
	}

	view, err = s.sessions.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	caps := sess.Caps()
	view.Caps = &caps
	return view, nil
}

// SetMode 切换会话的审批/沙箱模式，返回最新能力快照。
func (s *ChatService) SetMode(ctx context.Context, sessionID uint, modeID string) (*acp.Caps, error) {
	caps, err := s.manager.SetMode(ctx, sessionKey(sessionID), modeID)
	if err != nil {
		return nil, translateNoSession(sessionID, err)
	}
	s.brokerFor(sessionID).publish(StreamEvent{Kind: "mode", ModeID: modeID})
	return &caps, nil
}

// SetModel 切换会话模型，返回最新能力快照。
func (s *ChatService) SetModel(ctx context.Context, sessionID uint, modelID string) (*acp.Caps, error) {
	caps, err := s.manager.SetModel(ctx, sessionKey(sessionID), modelID)
	if err != nil {
		return nil, translateNoSession(sessionID, err)
	}
	return &caps, nil
}

// SetConfigOption 设置会话配置项（协作模式、推理档等），返回最新能力快照。
func (s *ChatService) SetConfigOption(ctx context.Context, sessionID uint, configID, value string) (*acp.Caps, error) {
	caps, err := s.manager.SetConfigOption(ctx, sessionKey(sessionID), configID, value)
	if err != nil {
		return nil, translateNoSession(sessionID, err)
	}
	s.brokerFor(sessionID).publish(StreamEvent{Kind: "config", ConfigOptions: caps.ConfigOptions})
	return &caps, nil
}

func translateNoSession(sessionID uint, err error) error {
	if errors.Is(err, acp.ErrNoSession) {
		return fmt.Errorf("session %d: %w", sessionID, ErrNotFound)
	}
	return err
}

// ResolveElicitation 把用户对交互式提问的作答回给阻塞中的 agent。
func (s *ChatService) ResolveElicitation(sessionID uint, elicitationID, action string, content map[string]any) error {
	switch action {
	case "accept", "decline", "cancel":
	default:
		return fmt.Errorf("%w: bad action %q", ErrInvalid, action)
	}
	err := s.manager.ResolveElicitation(sessionKey(sessionID), elicitationID, acp.ElicitationResult{
		Action:  action,
		Content: content,
	})
	if err != nil {
		return translateNoSession(sessionID, err)
	}
	return nil
}

// Send 广播用户消息并异步跑一轮。消息本身不落库——session/prompt 请求会
// 原样进转录，重建时从那里读回；这里广播的临时消息只为界面即时显示。
func (s *ChatService) Send(ctx context.Context, sessionID uint, text string) (*model.Message, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("%w: message content is required", ErrInvalid)
	}

	view, err := s.Open(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	// 无标题的会话用首条消息的简写自动命名（对齐主流 AI 聊天应用）。
	// 在返回 202 之前同步落库，前端跳转后立刻能看到新标题。
	if view.Title == "" {
		if title := deriveTitle(text); title != "" {
			if err := s.db.WithContext(ctx).Model(&model.Session{}).
				Where("id = ?", sessionID).Update("title", title).Error; err != nil {
				slog.Warn("auto title", "session", sessionID, "err", err)
			}
		}
	}

	// 临时 id 用毫秒时间戳：远大于重建器的行号序 id，不会与其冲突；
	// turn 结束后前端重拉重建列表，这条临时消息随之被替换。
	msg := &model.Message{
		ID:        uint(time.Now().UnixMilli()),
		SessionID: sessionID,
		Role:      model.RoleUser,
		Kind:      model.KindText,
		Content:   text,
		CreatedAt: time.Now(),
	}

	br := s.brokerFor(sessionID)
	br.startTurn()
	br.publish(StreamEvent{Kind: "user_message", Message: msg})

	go s.runTurn(sessionID, br, text)

	return msg, nil
}

// runTurn 跑完一轮。它在自己的 goroutine 里，
// 用 context.Background 是因为发起请求的 HTTP 连接早就返回了。
func (s *ChatService) runTurn(sessionID uint, br *broker, text string) {
	ctx := context.Background()

	result, err := s.manager.Prompt(ctx, sessionKey(sessionID), text)
	if err != nil {
		br.publish(StreamEvent{Kind: "error", Error: err.Error()})
		s.markSessionError(sessionID, err)
		br.endTurn()
		return
	}

	updates := map[string]any{"stop_reason": string(result.StopReason)}
	if result.StopReason.OK() {
		updates["state"] = model.SessionIdle
	} else {
		// 只有 end_turn 是正常说完；其余四种都意味着回答可能是残缺的。
		updates["state"] = model.SessionError
	}
	if err := s.db.WithContext(ctx).Model(&model.Session{}).Where("id = ?", sessionID).Updates(updates).Error; err != nil {
		slog.Error("save stop reason", "session", sessionID, "err", err)
	}

	br.endTurn()
}

// Messages 从转录重建会话的消息列表。
func (s *ChatService) Messages(sessionID uint) ([]model.Message, error) {
	entries, err := s.transcripts.Read(sessionKey(sessionID))
	if err != nil {
		return nil, fmt.Errorf("read transcript: %w", err)
	}
	return RebuildMessages(sessionID, entries), nil
}

// MessageCount 是会话重建后的消息数，供列表页显示。
func (s *ChatService) MessageCount(sessionID uint) int {
	messages, err := s.Messages(sessionID)
	if err != nil {
		return 0
	}
	return len(messages)
}

// TranscriptPath 返回会话转录文件的路径，供原始日志下载。
func (s *ChatService) TranscriptPath(sessionID uint) string {
	return s.transcripts.Path(sessionKey(sessionID))
}

// handleEvent 把 ACP 事件转成 SSE 事件推给浏览器。
// 持久化不在这里发生——线级消息已经由 WireTap 写进转录。
func (s *ChatService) handleEvent(br *broker, ev acp.Event) {
	switch ev.Kind {
	case acp.EventMessage:
		br.publish(StreamEvent{Kind: "message_chunk", Text: ev.Text})

	case acp.EventThought:
		br.publish(StreamEvent{Kind: "thought_chunk", Text: ev.Text})

	case acp.EventToolCall:
		// tool_call_update 除 toolCallId 外全是可选，只带变了的字段，前端按 id 合并。
		br.publish(StreamEvent{
			Kind:       "tool_call",
			ToolCallID: ev.ToolCallID,
			Title:      ev.Title,
			ToolKind:   ev.ToolKind,
			Status:     ev.Status,
			RawInput:   ev.RawInput,
			RawOutput:  ev.RawOutput,
		})

	case acp.EventPermission:
		br.publish(StreamEvent{
			Kind:       "permission",
			ToolCallID: ev.ToolCallID,
			ToolKind:   ev.ToolKind,
			Title:      ev.Title,
		})

	case acp.EventMode:
		br.publish(StreamEvent{Kind: "mode", ModeID: ev.ModeID})

	case acp.EventConfig:
		br.publish(StreamEvent{Kind: "config", ConfigOptions: ev.ConfigOptions})

	case acp.EventElicitation:
		br.publish(StreamEvent{
			Kind:          "elicitation",
			ElicitationID: ev.ElicitationID,
			ToolCallID:    ev.ToolCallID,
			Text:          ev.Text,
			RawInput:      ev.RawInput,
		})

	case acp.EventElicitationDone:
		br.publish(StreamEvent{Kind: "elicitation_done", ElicitationID: ev.ElicitationID})

	case acp.EventPlan:
		br.publish(StreamEvent{Kind: "plan", RawInput: ev.Entries})

	case acp.EventTurnEnd:
		br.publish(StreamEvent{Kind: "turn_end", StopReason: string(ev.StopReason)})

	case acp.EventError:
		br.publish(StreamEvent{Kind: "error", Error: ev.Error})
	}
}

// Subscribe 订阅会话的事件流。返回的 cancel 必须被调用以释放订阅。
// 当前轮已经发生的事件会先补给新订阅者，刷新页面不会丢掉正在跑的这一轮。
func (s *ChatService) Subscribe(sessionID uint) (<-chan StreamEvent, func()) {
	return s.brokerFor(sessionID).subscribe()
}

// Cancel 中止会话上正在跑的一轮。
func (s *ChatService) Cancel(sessionID uint) error {
	if err := s.manager.Cancel(sessionKey(sessionID)); err != nil {
		if errors.Is(err, acp.ErrNoSession) {
			return fmt.Errorf("session %d: %w", sessionID, ErrNotFound)
		}
		return err
	}
	return nil
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

// Running 报告该会话当前是否有活着的 agent 进程。
func (s *ChatService) Running(sessionID uint) bool {
	_, ok := s.manager.Get(sessionKey(sessionID))
	return ok
}

func (s *ChatService) markSessionError(sessionID uint, cause error) {
	err := s.db.Model(&model.Session{}).Where("id = ?", sessionID).Updates(map[string]any{
		"state":       model.SessionError,
		"stop_reason": truncateError(cause.Error()),
	}).Error
	if err != nil {
		slog.Error("mark session error", "session", sessionID, "err", err)
	}
}

func (s *ChatService) brokerFor(sessionID uint) *broker {
	s.mu.Lock()
	defer s.mu.Unlock()
	if br, ok := s.brokers[sessionID]; ok {
		return br
	}
	br := newBroker()
	s.brokers[sessionID] = br
	return br
}

func sessionKey(id uint) string { return strconv.FormatUint(uint64(id), 10) }

func truncateError(s string) string {
	const max = 512
	if len(s) <= max {
		return s
	}
	return s[:max]
}
