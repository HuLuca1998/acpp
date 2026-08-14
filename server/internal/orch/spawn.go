package orch

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
	"acpp/server/internal/service"
	"acpp/server/internal/stream"
)

// SpawnAgent 是 spawn_agent 工具的实现：按角色拉起一条真实的 ACP 子会话，
// 同步跑完任务并返回其最终结论——外化 subagent 的核心（adr-006）。
// 调用方（MCP handler）阻塞在这里，主会话的工具调用因此挂起等待，
// 语义与 claude 内部 Task 一致，只是全过程可观察。
func (s *Service) SpawnAgent(ctx context.Context, orch *model.OrchSession, roleName, taskText string) (string, error) {
	if strings.TrimSpace(taskText) == "" {
		return "", fmt.Errorf("%w: task is required", service.ErrInvalid)
	}
	if s.isStopped(orch.ID) {
		return "", fmt.Errorf("orchestration stopped by user")
	}

	role, err := s.roles.GetByName(ctx, roleName)
	if err != nil {
		return "", fmt.Errorf("unknown role %q", roleName)
	}
	var agent model.Agent
	if err := s.db.WithContext(ctx).First(&agent, role.AgentID).Error; err != nil {
		return "", fmt.Errorf("role %q has no usable agent", roleName)
	}

	// 并发护栏：一条主会话同时在跑的任务数有限——雇佣是注意力尺度的
	// 行为，不是资源压测。
	s.mu.Lock()
	if s.runningTasks[orch.ID] >= orchMaxConcurrentTasks {
		s.mu.Unlock()
		return "", fmt.Errorf("too many concurrent tasks (max %d), wait for one to finish", orchMaxConcurrentTasks)
	}
	s.runningTasks[orch.ID]++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.runningTasks[orch.ID]--
		s.mu.Unlock()
	}()

	task := model.OrchTask{
		OrchSessionID: orch.ID,
		RoleID:        role.ID,
		RoleName:      role.Name,
		Task:          taskText,
		State:         model.OrchTaskRunning,
	}
	if err := s.db.WithContext(ctx).Create(&task).Error; err != nil {
		return "", fmt.Errorf("create orch task: %w", err)
	}
	s.publishTaskUpdate(orch.ID, &task)
	started := time.Now()

	result, runErr := s.runTask(ctx, orch, role, &agent, &task)

	// 收尾统一在这里落库并广播——成功失败都要让派发流看到终态。
	task.DurationMS = time.Since(started).Milliseconds()
	if runErr != nil {
		task.State = model.OrchTaskFailed
		task.Result = service.TruncateError(runErr.Error())
	} else {
		task.State = model.OrchTaskDone
		task.Result = result
	}
	if all, _, err := s.TaskMessages(task.ID, 0, 0); err == nil {
		task.MessageCount = len(all)
	}
	if err := s.db.WithContext(context.Background()).Save(&task).Error; err != nil {
		slog.Error("save orch task", "task", task.ID, "err", err)
	}
	s.publishTaskUpdate(orch.ID, &task)

	if runErr != nil {
		return "", runErr
	}
	return result, nil
}

// runTask 打开角色子会话并跑完这一轮任务。子会话用完即收进程
// （记录与转录保留，可追溯可续看）；上下文在 agent 侧，之后想续问
// 可以按 acpSessionId 恢复（暂未开放）。
func (s *Service) runTask(ctx context.Context, orch *model.OrchSession, role *model.Role, agent *model.Agent, task *model.OrchTask) (string, error) {
	inj, err := s.roleInjection(role, agent)
	if err != nil {
		return "", err
	}

	key := orchTaskKey(task.ID)
	br := s.brokerFor(key)

	// 任务硬超时：MCP 侧超时配得更大，主动权在我们手里——超时后取消
	// 子会话 turn 并给主会话一个可解释的错误。
	taskCtx, cancel := context.WithTimeout(ctx, orchTaskTimeoutMinutes*time.Minute)
	defer cancel()

	cwd := orch.Cwd
	if cwd == "" {
		cwd = agent.Cwd
	}

	sess, err := s.manager.Open(taskCtx, acp.OpenOptions{
		Key:        key,
		Runtime:    acp.RuntimeFor(agent.Command, agent.Args, agent.Env),
		Cwd:        cwd,
		OnEvent:    func(ev acp.Event) { s.handleOrchEvent(0, key, br, ev) },
		WireTap:    func(dir string, msg json.RawMessage) { s.transcripts.Append(key, dir, msg) },
		ExtraEnv:   inj.ExtraEnv,
		MetaExtra:  inj.MetaExtra,
		MCPServers: inj.MCPServers,
	})
	if err != nil {
		return "", fmt.Errorf("open task session: %w", err)
	}
	// 进程用完即收；转录与 broker 保留（面板还能回看），broker 在
	// 会话删除时清理。
	defer func() {
		_ = s.manager.Close(key)
		s.transcripts.Close(key)
	}()

	if task.ACPSessionID = sess.ACPSessionID(); task.ACPSessionID != "" {
		if err := s.db.Model(&model.OrchTask{}).Where("id = ?", task.ID).
			Update("acp_session_id", task.ACPSessionID).Error; err != nil {
			slog.Warn("save task acp session id", "task", task.ID, "err", err)
		}
	}

	// 角色的统一设置预设（模型/思考深度/权限档），失败不阻塞任务——
	// 档位应用不上就用 runtime 默认档跑，宁可跑完也别卡死派发。
	patch := acp.SettingsPatch{}
	if role.Model != "" {
		patch.Model = &role.Model
	}
	if role.Effort != "" {
		e := acp.Effort(role.Effort)
		patch.Effort = &e
	}
	if role.Level != "" {
		l := acp.AccessLevel(role.Level)
		patch.Level = &l
	}
	if patch.Model != nil || patch.Effort != nil || patch.Level != nil {
		if _, err := s.manager.Apply(taskCtx, key, patch); err != nil {
			slog.Warn("apply role settings", "task", task.ID, "role", role.Name, "err", err)
		}
	}

	br.StartTurn()
	br.Publish(stream.Event{Kind: "user_message", Message: &model.Message{
		ID:        uint(time.Now().UnixMilli()),
		SessionID: task.ID,
		Role:      model.RoleUser,
		Kind:      model.KindText,
		Content:   task.Task,
		CreatedAt: time.Now(),
	}})

	result, err := s.manager.Prompt(taskCtx, key, []acp.ContentBlock{acp.TextBlock(task.Task)})
	br.EndTurn()
	if err != nil {
		if taskCtx.Err() != nil {
			return "", fmt.Errorf("task timed out after %d minutes", orchTaskTimeoutMinutes)
		}
		return "", err
	}
	if result.Usage != nil {
		s.addTokens(orch.ID, result.Usage)
		// 只改内存字段——落库统一在 SpawnAgent 收尾的 Save，这里写库会被
		// 那次全字段保存覆盖回去。
		task.TokensUsed = int64(result.Usage.TotalTokens)
	}
	task.StopReason = string(result.StopReason)

	// 子代理的产出 = 最后一条 agent 正文。从转录重建取，转录是唯一
	// 事实源（流式拼接可能丢 chunk）。
	answer := s.lastAgentText(task.ID)
	if !result.StopReason.OK() {
		// 非正常收尾（cancelled/max_tokens/refusal）：把状态如实告诉
		// 主会话，让它决定重试还是换路子。
		return "", fmt.Errorf("task ended with %s: %s", result.StopReason, service.TruncateError(answer))
	}
	if answer == "" {
		answer = "(子代理没有产出文本回复)"
	}
	return answer, nil
}

// lastAgentText 从任务转录重建消息，取最后一条 agent 正文。
func (s *Service) lastAgentText(taskID uint) string {
	all, _, err := s.TaskMessages(taskID, 0, 0)
	if err != nil {
		return ""
	}
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Role == model.RoleAgent && all[i].Kind == model.KindText {
			return all[i].Content
		}
	}
	return ""
}
