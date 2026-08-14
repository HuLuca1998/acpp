package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
)

// Send 向编排主会话发一轮。语义对齐 ChatService.Send：广播用户消息、
// 异步跑轮、懒连接；编排会话不做 @文件/图片（第一版纯文本）。
func (s *OrchService) Send(ctx context.Context, id uint, content string) (*model.Message, error) {
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("%w: message content is required", ErrInvalid)
	}
	orch, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	// 新的一轮解除急停——急停针对上一摊在跑的事。
	s.clearStopped(id)
	if err := s.ensureOpen(ctx, orch); err != nil {
		return nil, err
	}

	if orch.Title == "" {
		if title := deriveTitle(content); title != "" {
			if err := s.db.WithContext(ctx).Model(&model.OrchSession{}).
				Where("id = ?", id).Update("title", title).Error; err != nil {
				slog.Warn("orch auto title", "orch", id, "err", err)
			}
		}
	}

	msg := &model.Message{
		ID:        uint(time.Now().UnixMilli()),
		SessionID: id,
		Role:      model.RoleUser,
		Kind:      model.KindText,
		Content:   content,
		CreatedAt: time.Now(),
	}

	key := orchKey(id)
	br := s.brokerFor(key)
	if !s.manager.TurnActive(key) {
		br.startTurn()
	}
	br.publish(StreamEvent{Kind: "user_message", Message: msg})

	go s.runOrchTurn(id, br, []acp.ContentBlock{acp.TextBlock(content)})
	return msg, nil
}

// runOrchTurn 跑完主会话的一轮；spawn 的同步等待发生在这轮内部
// （MCP 工具调用挂起在 promptCall 里，天然被 turn 覆盖）。
func (s *OrchService) runOrchTurn(id uint, br *broker, blocks []acp.ContentBlock) {
	ctx := context.Background()
	key := orchKey(id)

	if err := s.db.WithContext(ctx).Model(&model.OrchSession{}).
		Where("id = ?", id).Update("state", model.SessionActive).Error; err != nil {
		slog.Warn("mark orch active", "orch", id, "err", err)
	}

	result, err := s.manager.Prompt(ctx, key, blocks)
	if errors.Is(err, acp.ErrBusy) {
		var followUp bool
		result, followUp, err = s.manager.Interject(ctx, key, blocks)
		if err == nil && !followUp {
			return
		}
	}
	if err != nil {
		br.publish(StreamEvent{Kind: "error", Error: err.Error()})
		s.markOrchError(id, err)
		if !s.manager.TurnActive(key) {
			br.endTurn()
		}
		return
	}

	updates := map[string]any{"stop_reason": string(result.StopReason)}
	if result.StopReason.OK() {
		updates["state"] = model.SessionIdle
	} else {
		updates["state"] = model.SessionError
	}
	if all, _, err := s.Messages(id, 0, 0); err == nil {
		updates["message_count"] = len(all)
	}
	if result.Usage != nil {
		s.addTokens(id, result.Usage)
	}
	if err := s.db.WithContext(ctx).Model(&model.OrchSession{}).
		Where("id = ?", id).Updates(updates).Error; err != nil {
		slog.Error("save orch stop reason", "orch", id, "err", err)
	}

	if s.manager.TurnActive(key) {
		return
	}
	br.endTurn()
}

// addTokens 把一轮的 token 计量累加到编排会话总量（主轮与任务子轮
// 都进同一个池——用户看到的是这摊编排到底烧了多少）。
func (s *OrchService) addTokens(id uint, usage *acp.Usage) {
	if usage == nil || usage.TotalTokens == 0 {
		return
	}
	if err := s.db.Model(&model.OrchSession{}).Where("id = ?", id).
		Update("tokens_used", gorm.Expr("tokens_used + ?", usage.TotalTokens)).Error; err != nil {
		slog.Warn("add orch tokens", "orch", id, "err", err)
	}
}

// Cancel 中止主会话当前轮（不动子任务；全面停摆走 Stop）。
func (s *OrchService) Cancel(id uint) error {
	if err := s.manager.Cancel(orchKey(id)); err != nil {
		if errors.Is(err, acp.ErrNoSession) {
			return fmt.Errorf("orch session %d: %w", id, ErrNotFound)
		}
		return err
	}
	return nil
}

// ApplySettings 应用主会话统一设置（模型/思考深度/权限档等）。
func (s *OrchService) ApplySettings(ctx context.Context, id uint, patch acp.SettingsPatch) (*acp.Settings, error) {
	orch, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.ensureOpen(ctx, orch); err != nil {
		return nil, err
	}
	settings, err := s.manager.Apply(ctx, orchKey(id), patch)
	if err != nil {
		if errors.Is(err, acp.ErrNoSession) {
			return nil, fmt.Errorf("orch session %d: %w", id, ErrNotFound)
		}
		return nil, err
	}
	s.saveOrchSettingsSnapshot(id, &settings)
	s.brokerFor(orchKey(id)).publish(StreamEvent{Kind: "settings", Settings: &settings})
	return &settings, nil
}

// Settings 读主会话当前设置：活着读实况，未连接给降级视图。
func (s *OrchService) Settings(ctx context.Context, id uint) (*acp.Settings, error) {
	orch, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if settings, err := s.manager.Settings(orchKey(id)); err == nil {
		return &settings, nil
	}
	var agent model.Agent
	if err := s.db.WithContext(ctx).First(&agent, orch.AgentID).Error; err != nil {
		return nil, fmt.Errorf("load agent %d: %w", orch.AgentID, err)
	}
	return degradedSettings(&agent, orch.LastSettings), nil
}

// ResolvePermission 回传主会话的权限裁决。
func (s *OrchService) ResolvePermission(id uint, permissionID, optionID string) error {
	return s.resolvePermissionByKey(orchKey(id), permissionID, optionID)
}

// ResolveTaskPermission 回传任务子会话的权限裁决（拖出的子面板上点）。
func (s *OrchService) ResolveTaskPermission(taskID uint, permissionID, optionID string) error {
	return s.resolvePermissionByKey(orchTaskKey(taskID), permissionID, optionID)
}

func (s *OrchService) resolvePermissionByKey(key, permissionID, optionID string) error {
	if err := s.manager.ResolvePermission(key, permissionID, optionID); err != nil {
		if errors.Is(err, acp.ErrNoSession) {
			return fmt.Errorf("%s: %w", key, ErrNotFound)
		}
		return err
	}
	return nil
}

// ResolveElicitation 回传主会话交互式提问的作答。
func (s *OrchService) ResolveElicitation(id uint, elicitationID, action string, content map[string]any) error {
	return s.resolveElicitationByKey(orchKey(id), elicitationID, action, content)
}

// ResolveTaskElicitation 回传任务子会话交互式提问的作答。
func (s *OrchService) ResolveTaskElicitation(taskID uint, elicitationID, action string, content map[string]any) error {
	return s.resolveElicitationByKey(orchTaskKey(taskID), elicitationID, action, content)
}

func (s *OrchService) resolveElicitationByKey(key, elicitationID, action string, content map[string]any) error {
	switch action {
	case "accept", "decline", "cancel":
	default:
		return fmt.Errorf("%w: bad action %q", ErrInvalid, action)
	}
	err := s.manager.ResolveElicitation(key, elicitationID, acp.ElicitationResult{
		Action:  action,
		Content: content,
	})
	if err != nil {
		if errors.Is(err, acp.ErrNoSession) {
			return fmt.Errorf("%s: %w", key, ErrNotFound)
		}
		return err
	}
	return nil
}

// CancelTask 中止一条在跑的任务子会话（列表上的单任务停止按钮）。
func (s *OrchService) CancelTask(taskID uint) error {
	if err := s.manager.Cancel(orchTaskKey(taskID)); err != nil {
		if errors.Is(err, acp.ErrNoSession) {
			return fmt.Errorf("orch task %d: %w", taskID, ErrNotFound)
		}
		return err
	}
	return nil
}
