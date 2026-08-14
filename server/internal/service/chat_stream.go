package service

import (
	"context"
	"encoding/json"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
)

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
	// Entries 是 plan 事件的任务条目数组。
	Entries       json.RawMessage `json:"entries,omitempty"`
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

	// Task 是编排会话的任务状态快照（kind=task_update），普通会话不发。
	Task *model.OrchTask `json:"task,omitempty"`
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
		// 技能调用统计：从 tool_call 信号识别并计数（按 toolCallId 去重）。
		if s.skillUsage != nil {
			s.skillUsage.Observe(ev)
		}
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
		s.saveSettingsSnapshot(sessionID, ev.Settings)
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
		br.publish(StreamEvent{Kind: "plan", Entries: ev.Entries})

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
