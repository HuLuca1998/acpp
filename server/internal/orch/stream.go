package orch

import (
	"log/slog"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
	"acpp/server/internal/service"
	"acpp/server/internal/stream"
)

// handleOrchEvent 把编排会话（主或任务子会话）的 ACP 事件转成 SSE 事件。
// 与 ChatService.handleEvent 的差异：不做配置页取舍过滤（编排会话不受
// 模型下拉勾选影响），settings 快照写 orch 表；sessionID 是主会话 id，
// 任务子会话传 0（settings 不落库）。
func (s *Service) handleOrchEvent(sessionID uint, br *stream.Broker, ev acp.Event) {
	switch ev.Kind {
	case acp.EventMessage:
		br.Publish(stream.Event{Kind: "message_chunk", Text: ev.Text})

	case acp.EventThought:
		br.Publish(stream.Event{Kind: "thought_chunk", Text: ev.Text})

	case acp.EventToolCall:
		if s.skillUsage != nil {
			s.skillUsage.Observe(ev)
		}
		br.Publish(stream.Event{
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
		br.Publish(stream.Event{
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
		br.Publish(stream.Event{Kind: "permission_done", PermissionID: ev.PermissionID})

	case acp.EventSettings:
		if sessionID != 0 {
			s.saveOrchSettingsSnapshot(sessionID, ev.Settings)
		}
		br.Publish(stream.Event{Kind: "settings", Settings: ev.Settings})

	case acp.EventUsage:
		br.Publish(stream.Event{Kind: "usage", Used: ev.Used, Size: ev.Size})

	case acp.EventCommands:
		br.Publish(stream.Event{Kind: "commands", Commands: ev.Commands})

	case acp.EventElicitation:
		br.Publish(stream.Event{
			Kind:          "elicitation",
			ElicitationID: ev.ElicitationID,
			ToolCallID:    ev.ToolCallID,
			Text:          ev.Text,
			RawInput:      ev.RawInput,
		})

	case acp.EventElicitationDone:
		br.Publish(stream.Event{Kind: "elicitation_done", ElicitationID: ev.ElicitationID})

	case acp.EventPlan:
		br.Publish(stream.Event{Kind: "plan", Entries: ev.Entries})

	case acp.EventTurnEnd:
		br.Publish(stream.Event{Kind: "turn_end", StopReason: string(ev.StopReason), Usage: ev.Usage})

	case acp.EventError:
		br.Publish(stream.Event{Kind: "error", Error: ev.Error})
	}
}

// saveOrchSettingsSnapshot 把统一设置当前值写回编排会话记录（旁路）。
func (s *Service) saveOrchSettingsSnapshot(id uint, settings *acp.Settings) {
	snapshot := model.JSONMap{
		"model":  settings.CurrentModel,
		"effort": string(settings.CurrentEffort),
		"level":  string(settings.CurrentLevel),
		"plan":   settings.PlanOn,
		"fast":   settings.FastOn,
	}
	if err := s.db.Model(&model.OrchSession{}).
		Where("id = ?", id).Update("last_settings", snapshot).Error; err != nil {
		slog.Warn("save orch settings snapshot", "orch", id, "err", err)
	}
}

func (s *Service) markOrchError(id uint, cause error) {
	err := s.db.Model(&model.OrchSession{}).Where("id = ?", id).Updates(map[string]any{
		"state":       model.SessionError,
		"stop_reason": service.TruncateError(cause.Error()),
	}).Error
	if err != nil {
		slog.Error("mark orch session error", "orch", id, "err", err)
	}
}

// publishTaskUpdate 把任务状态变化推进主会话事件流——task 列表面板的
// 实时数据源。
func (s *Service) publishTaskUpdate(orchID uint, task *model.OrchTask) {
	s.brokerFor(orchKey(orchID)).Publish(stream.Event{Kind: "task_update", Task: task})
}
