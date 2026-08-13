package acp

import (
	"context"
	"fmt"
	"log/slog"
)

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
