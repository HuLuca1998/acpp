package service

import (
	"context"

	"acpp/server/internal/acp"
	"acpp/server/internal/stream"
)

// StreamEvent 是推给浏览器的一条 SSE 事件（别名指向共享的 stream.Event，
// 广播机制与事件形状归 internal/stream，聊天与编排两族共用）。
type StreamEvent = stream.Event

// handleEvent 把 ACP 事件转成 SSE 事件推给浏览器。
// 持久化不在这里发生——线级消息已经由 WireTap 写进转录。
func (s *ChatService) handleEvent(sessionID uint, br *stream.Broker, ev acp.Event) {
	switch ev.Kind {
	case acp.EventMessage:
		br.Publish(StreamEvent{Kind: "message_chunk", Text: ev.Text})

	case acp.EventThought:
		br.Publish(StreamEvent{Kind: "thought_chunk", Text: ev.Text})

	case acp.EventToolCall:
		// 技能调用统计：从 tool_call 信号识别并计数（按 toolCallId 去重）。
		if s.skillUsage != nil {
			s.skillUsage.Observe(ev)
		}
		// tool_call_update 除 toolCallId 外全是可选，只带变了的字段，前端按 id 合并。
		br.Publish(StreamEvent{
			Kind:       "tool_call",
			ToolCallID: ev.ToolCallID,
			Title:      ev.Title,
			ToolKind:   ev.ToolKind,
			Status:     ev.Status,
			RawInput:   ev.RawInput,
			RawOutput:  ev.RawOutput,
			Content:    ev.Content,
			Locations:  ev.Locations,

			IsSubagent:       ev.IsSubagent,
			SubagentOf:       ev.SubagentOf,
			SubagentThreadID: ev.SubagentThreadID,
			SubagentPath:     ev.SubagentPath,
		})

	case acp.EventPermission:
		br.Publish(StreamEvent{
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
		br.Publish(StreamEvent{Kind: "permission_done", PermissionID: ev.PermissionID})

	case acp.EventSettings:
		// agent 自行改配置推来的视图同样要过配置页的取舍。
		s.catalogFor(context.Background(), sessionID).filterSettings(ev.Settings)
		s.saveSettingsSnapshot(sessionID, ev.Settings)
		br.Publish(StreamEvent{Kind: "settings", Settings: ev.Settings})

	case acp.EventUsage:
		// 记内存供轮末落库——上下文水位不该随会话停止而消失。
		s.rememberUsage(sessionID, ev.Used, ev.Size, ev.Cost)
		br.Publish(StreamEvent{Kind: "usage", Used: ev.Used, Size: ev.Size, Cost: ev.Cost})

	case acp.EventCommands:
		commands := s.catalogFor(context.Background(), sessionID).filterCommands(ev.Commands)
		br.Publish(StreamEvent{Kind: "commands", Commands: commands})

	case acp.EventElicitation:
		br.Publish(StreamEvent{
			Kind:          "elicitation",
			ElicitationID: ev.ElicitationID,
			ToolCallID:    ev.ToolCallID,
			Text:          ev.Text,
			RawInput:      ev.RawInput,
		})

	case acp.EventElicitationDone:
		br.Publish(StreamEvent{Kind: "elicitation_done", ElicitationID: ev.ElicitationID})

	case acp.EventPlan:
		br.Publish(StreamEvent{Kind: "plan", Entries: ev.Entries})

	case acp.EventTurnEnd:
		br.Publish(StreamEvent{Kind: "turn_end", StopReason: string(ev.StopReason), Usage: ev.Usage})

	case acp.EventError:
		br.Publish(StreamEvent{Kind: "error", Error: ev.Error})
	}
}

// Subscribe 订阅会话的事件流。返回的 cancel 必须被调用以释放订阅。
// 当前轮已经发生的事件会先补给新订阅者，刷新页面不会丢掉正在跑的这一轮。
func (s *ChatService) Subscribe(sessionID uint) (<-chan StreamEvent, func()) {
	return s.brokerFor(sessionID).Subscribe()
}
