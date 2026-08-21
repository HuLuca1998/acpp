package service

import (
	"context"
	"strings"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
	"acpp/server/internal/stream"
)

// StreamEvent 是推给浏览器的一条 SSE 事件（别名指向共享的 stream.Event，
// 广播机制与事件形状归 internal/stream，聊天与编排两族共用）。
type StreamEvent = stream.Event

// StreamNotice 是一条全局通知（别名指向 stream.Notice，形状与广播机制
// 同样归 internal/stream）。
type StreamNotice = stream.Notice

// handleEvent 把 ACP 事件转成 SSE 事件推给浏览器。
// 持久化不在这里发生——线级消息已经由 WireTap 写进转录。
func (s *ChatService) handleEvent(sessionID uint, br *stream.Broker, ev acp.Event) {
	switch ev.Kind {
	case acp.EventMessage:
		s.rememberTail(sessionID, ev.Text)
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
		s.notify(sessionID, StreamNotice{
			Event: "permission", Text: ev.Title,
			PermissionID: ev.PermissionID, Options: ev.Options,
		})

	case acp.EventPermissionDone:
		br.Publish(StreamEvent{Kind: "permission_done", PermissionID: ev.PermissionID})
		s.notify(sessionID, StreamNotice{Event: "permission_done", PermissionID: ev.PermissionID})

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
		s.notify(sessionID, StreamNotice{
			Event: "elicitation", Text: ev.Text, ElicitationID: ev.ElicitationID,
		})

	case acp.EventElicitationDone:
		br.Publish(StreamEvent{Kind: "elicitation_done", ElicitationID: ev.ElicitationID})
		s.notify(sessionID, StreamNotice{Event: "elicitation_done", ElicitationID: ev.ElicitationID})

	case acp.EventPlan:
		br.Publish(StreamEvent{Kind: "plan", Entries: ev.Entries})

	case acp.EventTurnEnd:
		br.Publish(StreamEvent{Kind: "turn_end", StopReason: string(ev.StopReason), Usage: ev.Usage})
		s.notify(sessionID, StreamNotice{Event: "turn_end", Text: s.takeTail(sessionID)})

	case acp.EventError:
		br.Publish(StreamEvent{Kind: "error", Error: ev.Error})
		s.notify(sessionID, StreamNotice{Event: "error", Text: ev.Error})
	}
}

// Subscribe 订阅会话的事件流。返回的 cancel 必须被调用以释放订阅。
// 当前轮已经发生的事件会先补给新订阅者，刷新页面不会丢掉正在跑的这一轮。
func (s *ChatService) Subscribe(sessionID uint) (<-chan StreamEvent, func()) {
	return s.brokerFor(sessionID).Subscribe()
}

// noticeTextRunes 是通知摘要的长度上限。系统通知与页内提示都只有一两行的
// 位置，多给的字符不会被显示，只会把有用的部分挤出去。
const noticeTextRunes = 120

// notify 把一件值得打扰用户的事广播到全局流。
//
// 这里只如实上报「发生了什么」，不判断该不该弹——那要知道用户此刻在看哪
// 一页、页面在不在前台，只有客户端清楚（见 stream.Notice）。为此查一次
// 会话拿归属与标题：通知是低频事件（一轮最多几条），不值得为它多养一份缓存。
func (s *ChatService) notify(sessionID uint, n StreamNotice) {
	if s.notices == nil {
		return
	}
	var session model.Session
	if err := s.db.First(&session, sessionID).Error; err != nil {
		return
	}
	n.Kind = "notify"
	n.SessionID = sessionID
	n.SessionTitle = session.Title
	n.Text = summarizeNotice(n.Text)
	n.TenantID = session.TenantID
	s.notices.Publish(n)
}

// summarizeNotice 把一段正文压成通知能显示的一行：折叠全部空白再截断。
func summarizeNotice(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= noticeTextRunes {
		return text
	}
	return string(runes[:noticeTextRunes]) + "…"
}

// rememberTail 收下正文分片的尾巴，供轮末通知做摘要。
//
// 留尾不留头：一轮结束时用户想知道的是结论，而结论在最后一段话里。只留
// 上限的两倍长度，再长的轮次也不会在内存里攒出一整篇回答。
func (s *ChatService) rememberTail(sessionID uint, text string) {
	if s.notices == nil || text == "" {
		return
	}
	s.tailsMu.Lock()
	defer s.tailsMu.Unlock()
	tail := s.tails[sessionID] + text
	if runes := []rune(tail); len(runes) > noticeTextRunes*2 {
		tail = string(runes[len(runes)-noticeTextRunes*2:])
	}
	s.tails[sessionID] = tail
}

// takeTail 取走本轮攒下的正文尾巴——取走即清空，下一轮从头攒。
func (s *ChatService) takeTail(sessionID uint) string {
	s.tailsMu.Lock()
	defer s.tailsMu.Unlock()
	tail := s.tails[sessionID]
	delete(s.tails, sessionID)
	return tail
}
