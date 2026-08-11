package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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

	Text          string          `json:"text,omitempty"`
	ToolCallID    string          `json:"toolCallId,omitempty"`
	Title         string          `json:"title,omitempty"`
	ToolKind      string          `json:"toolKind,omitempty"`
	Status        string          `json:"status,omitempty"`
	RawInput      json.RawMessage `json:"rawInput,omitempty"`
	RawOutput     json.RawMessage `json:"rawOutput,omitempty"`
	Content       json.RawMessage `json:"content,omitempty"`
	Locations     json.RawMessage `json:"locations,omitempty"`
	Settings      *acp.Settings   `json:"settings,omitempty"`
	Used          int64           `json:"used,omitempty"`
	Size          int64           `json:"size,omitempty"`
	Commands      []acp.Command   `json:"commands,omitempty"`
	Usage         *acp.Usage      `json:"usage,omitempty"`
	ElicitationID string          `json:"elicitationId,omitempty"`
	// 权限请求：ID 用于回传裁决，Options 是 agent 给的选项。
	// PlanReview 非空时这是「计划完成」审批，前端渲染专门卡片。
	PermissionID string                 `json:"permissionId,omitempty"`
	Options      []acp.PermissionOption `json:"options,omitempty"`
	PlanReview   *acp.PlanReview        `json:"planReview,omitempty"`
	StopReason   string                 `json:"stopReason,omitempty"`
	Error        string                 `json:"error,omitempty"`

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

// agentCatalog 是配置页取舍的快照：被禁用的模型与命令。
type agentCatalog struct {
	models   map[string]bool
	commands map[string]bool
}

// catalogFor 读会话所属 agent 的配置页取舍。读不到按全启用处理——
// 过滤是体验优化，不该因为一次查库失败把清单清空。
func (s *ChatService) catalogFor(ctx context.Context, sessionID uint) agentCatalog {
	cat := agentCatalog{models: map[string]bool{}, commands: map[string]bool{}}
	var session model.Session
	if err := s.db.WithContext(ctx).First(&session, sessionID).Error; err != nil {
		return cat
	}
	var agent model.Agent
	if err := s.db.WithContext(ctx).First(&agent, session.AgentID).Error; err != nil {
		return cat
	}
	for _, m := range agent.Models {
		if m.Disabled {
			cat.models[m.ID] = true
		}
	}
	for _, c := range agent.Commands {
		if c.Disabled {
			cat.commands[c.Name] = true
		}
	}
	return cat
}

// filterSettings 把配置页禁用的模型从统一视图的清单里去掉。
// 当前值不动——就算被禁也如实显示，只是不再出现在可选项里。
func (cat agentCatalog) filterSettings(settings *acp.Settings) {
	if settings == nil || len(cat.models) == 0 {
		return
	}
	kept := make([]acp.Model, 0, len(settings.Models))
	for _, m := range settings.Models {
		if !cat.models[m.ID] {
			kept = append(kept, m)
		}
	}
	settings.Models = kept
}

// filterCommands 去掉配置页禁用的斜杠命令。
func (cat agentCatalog) filterCommands(commands []acp.Command) []acp.Command {
	if len(cat.commands) == 0 {
		return commands
	}
	kept := make([]acp.Command, 0, len(commands))
	for _, c := range commands {
		if !cat.commands[c.Name] {
			kept = append(kept, c)
		}
	}
	return kept
}

// Open 为一条已存在的数据库会话拉起 agent 并完成 ACP 握手。
// 会话已经开着时直接返回，重复调用是安全的。
func (s *ChatService) Open(ctx context.Context, sessionID uint) (*SessionView, error) {
	view, err := s.sessions.Get(ctx, sessionID)
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
	cat := s.catalogFor(ctx, sessionID)
	if settings, err := s.manager.Settings(key); err == nil {
		cat.filterSettings(&settings)
		view.Settings = &settings
	}
	view.Commands = cat.filterCommands(s.manager.Commands(key))
	return view, nil
}

// ApplySettings 逐项应用统一设置变更并广播最新视图（多标签页保持同步）。
func (s *ChatService) ApplySettings(ctx context.Context, sessionID uint, patch acp.SettingsPatch) (*acp.Settings, error) {
	settings, err := s.manager.Apply(ctx, sessionKey(sessionID), patch)
	if err != nil {
		return nil, translateNoSession(sessionID, err)
	}
	s.catalogFor(ctx, sessionID).filterSettings(&settings)
	s.brokerFor(sessionID).publish(StreamEvent{Kind: "settings", Settings: &settings})
	return &settings, nil
}

func translateNoSession(sessionID uint, err error) error {
	if errors.Is(err, acp.ErrNoSession) {
		return fmt.Errorf("session %d: %w", sessionID, ErrNotFound)
	}
	return err
}

// ResolvePermission 把用户对权限请求的裁决回给阻塞中的 agent。
// optionID 为空表示用户取消。
func (s *ChatService) ResolvePermission(sessionID uint, permissionID, optionID string) error {
	if err := s.manager.ResolvePermission(sessionKey(sessionID), permissionID, optionID); err != nil {
		return translateNoSession(sessionID, err)
	}
	return nil
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

// ImageInput 是随消息上传的一张图片（base64，无 data: 前缀）。
type ImageInput struct {
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

// SendInput 是发一轮的入参：文本 + 可选图片 + 可选 @ 引用的文件路径。
type SendInput struct {
	Content string       `json:"content"`
	Images  []ImageInput `json:"images,omitempty"`
	// Files 是 @ 引用的文件（绝对路径或相对会话 cwd），内容由后端读出
	// 以 resource 块嵌进 prompt（两端 embeddedContext 都支持）。
	Files []string `json:"files,omitempty"`
}

// Send 广播用户消息并异步跑一轮。消息本身不落库——session/prompt 请求会
// 原样进转录，重建时从那里读回；这里广播的临时消息只为界面即时显示。
func (s *ChatService) Send(ctx context.Context, sessionID uint, in SendInput) (*model.Message, error) {
	text := in.Content
	if strings.TrimSpace(text) == "" && len(in.Images) == 0 && len(in.Files) == 0 {
		return nil, fmt.Errorf("%w: message content is required", ErrInvalid)
	}

	view, err := s.Open(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	// 组 prompt 内容块：@ 文件 → resource（后端读内容），图片 → image，正文 → text。
	blocks, payload, err := s.buildBlocks(view.Cwd, in)
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
		Payload:   payload,
		CreatedAt: time.Now(),
	}

	br := s.brokerFor(sessionID)
	br.startTurn()
	br.publish(StreamEvent{Kind: "user_message", Message: msg})

	go s.runTurn(sessionID, br, blocks)

	return msg, nil
}

// buildBlocks 把发送入参翻译成 prompt 内容块，并给临时消息组展示 payload。
// @ 文件在这里读内容：路径相对会话 cwd 解析，读不到直接报错（发出去
// 一个空引用比报错更糟）。
func (s *ChatService) buildBlocks(cwd string, in SendInput) ([]acp.ContentBlock, model.JSONMap, error) {
	var blocks []acp.ContentBlock
	payload := model.JSONMap{}

	var files []string
	for _, path := range in.Files {
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: read %s: %s", ErrInvalid, path, err)
		}
		blocks = append(blocks, acp.ResourceBlock("file://"+path, string(data)))
		files = append(files, path)
	}
	if len(files) > 0 {
		payload["files"] = files
	}

	var images []map[string]any
	for _, img := range in.Images {
		if img.Data == "" || img.MimeType == "" {
			return nil, nil, fmt.Errorf("%w: image data and mimeType are required", ErrInvalid)
		}
		blocks = append(blocks, acp.ImageBlock(img.Data, img.MimeType))
		images = append(images, map[string]any{"data": img.Data, "mimeType": img.MimeType})
	}
	if len(images) > 0 {
		payload["images"] = images
	}

	if strings.TrimSpace(in.Content) != "" {
		blocks = append(blocks, acp.TextBlock(in.Content))
	}
	if len(payload) == 0 {
		payload = nil
	}
	return blocks, payload, nil
}

// runTurn 跑完一轮。它在自己的 goroutine 里，
// 用 context.Background 是因为发起请求的 HTTP 连接早就返回了。
func (s *ChatService) runTurn(sessionID uint, br *broker, blocks []acp.ContentBlock) {
	ctx := context.Background()

	result, err := s.manager.Prompt(ctx, sessionKey(sessionID), blocks)
	if errors.Is(err, acp.ErrBusy) {
		// turn 进行中：改走插话通道（claude 排队为独立一轮，codex 注入当前轮）。
		var followUp bool
		result, followUp, err = s.manager.Interject(ctx, sessionKey(sessionID), blocks)
		if err == nil && !followUp {
			// 内容并入当前轮，收尾归当前轮的 runTurn，这里直接退场。
			return
		}
	}
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

// ProbeAgent 拉一个临时会话读取 agent 的统一设置能力（flavor 与模型清单），
// 缓存进 Agent 记录后立即关闭。新会话页靠这份缓存在建会话之前展示
// 跨 agent 的模型清单。探测失败时清单置空并记下错误，不影响 agent 使用。
func (s *ChatService) ProbeAgent(ctx context.Context, agentID uint) (*model.Agent, error) {
	var agent model.Agent
	if err := s.db.WithContext(ctx).First(&agent, agentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("agent %d: %w", agentID, ErrNotFound)
		}
		return nil, fmt.Errorf("load agent %d: %w", agentID, err)
	}

	// 探测会话不干活，cwd 用独立的临时目录，不碰 agent 配置的工作目录。
	probeCwd := filepath.Join(os.TempDir(), "acpp-probe")
	key := fmt.Sprintf("probe-agent-%d", agentID)

	updates := map[string]any{}
	_, err := s.manager.Open(ctx, acp.OpenOptions{
		Key:     key,
		Runtime: acp.RuntimeFor(agent.Command, agent.Args, agent.Env),
		Cwd:     probeCwd,
		OnEvent: func(acp.Event) {},
	})
	if err != nil {
		updates["flavor"] = ""
		updates["models"] = model.AgentModelSlice{}
		updates["commands"] = model.AgentCommandSlice{}
		updates["status"] = model.AgentError
		updates["last_error"] = truncateError(err.Error())
	} else {
		settings, serr := s.manager.Settings(key)
		// 斜杠命令清单以通知形式在会话建立后异步推到，等它一小会——
		// 拿不到就算了（agent 可能真的没有命令），不拖垮探测。
		var commands []acp.Command
		for range 30 {
			if commands = s.manager.Commands(key); len(commands) > 0 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		_ = s.manager.Close(key)
		if serr != nil {
			return nil, serr
		}
		// 重探不清空配置页的取舍：旧缓存里被禁用的条目保持禁用。
		oldDisabled := map[string]bool{}
		for _, m := range agent.Models {
			if m.Disabled {
				oldDisabled["m:"+m.ID] = true
			}
		}
		for _, c := range agent.Commands {
			if c.Disabled {
				oldDisabled["c:"+c.Name] = true
			}
		}
		models := make(model.AgentModelSlice, 0, len(settings.Models))
		for _, m := range settings.Models {
			models = append(models, model.AgentModel{
				ID: m.ID, Name: m.Name, Description: m.Description,
				Disabled: oldDisabled["m:"+m.ID],
			})
		}
		cached := make(model.AgentCommandSlice, 0, len(commands))
		for _, c := range commands {
			cached = append(cached, model.AgentCommand{
				Name: c.Name, Description: c.Description,
				Disabled: oldDisabled["c:"+c.Name],
			})
		}
		updates["flavor"] = string(settings.Flavor)
		updates["models"] = models
		updates["commands"] = cached
		updates["status"] = model.AgentIdle
		updates["last_error"] = ""
	}

	if err := s.db.WithContext(ctx).Model(&model.Agent{}).Where("id = ?", agentID).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("save agent probe result: %w", err)
	}
	if err := s.db.WithContext(ctx).First(&agent, agentID).Error; err != nil {
		return nil, fmt.Errorf("reload agent %d: %w", agentID, err)
	}
	return &agent, nil
}

// ProbeAgentAsync 在后台探测（注册/更新 agent 后自动触发），完成后结果落库。
func (s *ChatService) ProbeAgentAsync(agentID uint) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if _, err := s.ProbeAgent(ctx, agentID); err != nil {
			slog.Warn("probe agent", "agent", agentID, "err", err)
		}
	}()
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
func (s *ChatService) handleEvent(sessionID uint, br *broker, ev acp.Event) {
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
			Content:    ev.Content,
			Locations:  ev.Locations,
		})

	case acp.EventPermission:
		br.publish(StreamEvent{
			Kind:         "permission",
			PermissionID: ev.PermissionID,
			ToolCallID:   ev.ToolCallID,
			ToolKind:     ev.ToolKind,
			Title:        ev.Title,
			RawInput:     ev.RawInput,
			Content:      ev.Content,
			Options:      ev.Options,
			PlanReview:   ev.PlanReview,
		})

	case acp.EventPermissionDone:
		br.publish(StreamEvent{Kind: "permission_done", PermissionID: ev.PermissionID})

	case acp.EventSettings:
		// agent 自行改配置推来的视图同样要过配置页的取舍。
		s.catalogFor(context.Background(), sessionID).filterSettings(ev.Settings)
		br.publish(StreamEvent{Kind: "settings", Settings: ev.Settings})

	case acp.EventUsage:
		br.publish(StreamEvent{Kind: "usage", Used: ev.Used, Size: ev.Size})

	case acp.EventCommands:
		commands := s.catalogFor(context.Background(), sessionID).filterCommands(ev.Commands)
		br.publish(StreamEvent{Kind: "commands", Commands: commands})

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
		br.publish(StreamEvent{Kind: "turn_end", StopReason: string(ev.StopReason), Usage: ev.Usage})

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

// Destroy 在删除会话前做彻底清理：先尽力删掉 agent 侧的线程历史
//（session/delete，删不掉只记警告不阻塞本地删除），再关进程。
func (s *ChatService) Destroy(ctx context.Context, sessionID uint) error {
	view, err := s.sessions.Get(ctx, sessionID)
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
