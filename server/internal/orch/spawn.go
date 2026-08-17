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

// spawnSyncWait 是 hire_role/wait_task 单次调用的同步等待窗口。runtime
// 对 MCP 工具调用有我们改不掉的超时上限（claude 实测约 5 分钟 clamp，
// env 与 per-server timeout 都突破不了；codex 上限未知），窗口取其下
// ——超窗不算失败，返回「继续 wait_task」提示让主控接力等待，任务
// 本体不受任何 runtime 超时影响。
const spawnSyncWait = 270 * time.Second

// SpawnAgent 是 hire_role 工具的实现：按角色拉起一条真实的 ACP 子会话
// 后台执行任务——外化 subagent 的核心（adr-006）。调用方（MCP handler）
// 在同步窗口内等结果：快任务直接拿到结论（与内部 Task 体验一致），
// 慢任务收到接力提示改调 WaitTask，成果永不因 runtime 超时丢失。
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
	// 行为，不是资源压测。计数在任务 goroutine 收尾时归还。
	s.mu.Lock()
	if s.runningTasks[orch.ID] >= orchMaxConcurrentTasks {
		s.mu.Unlock()
		return "", fmt.Errorf("too many concurrent tasks (max %d), wait for one to finish", orchMaxConcurrentTasks)
	}
	s.runningTasks[orch.ID]++
	s.mu.Unlock()

	task := model.OrchTask{
		OrchSessionID: orch.ID,
		RoleID:        role.ID,
		RoleName:      role.Name,
		Task:          taskText,
		State:         model.OrchTaskRunning,
	}
	if err := s.db.WithContext(ctx).Create(&task).Error; err != nil {
		s.mu.Lock()
		s.runningTasks[orch.ID]--
		s.mu.Unlock()
		return "", fmt.Errorf("create orch task: %w", err)
	}
	s.publishTaskUpdate(orch.ID, &task)

	go s.executeTask(orch, role, &agent, task)

	return s.awaitTask(ctx, task.ID)
}

// WaitTask 是 wait_task 工具的实现：对一条后台任务续一段同步等待窗口。
func (s *Service) WaitTask(ctx context.Context, orch *model.OrchSession, taskID uint) (string, error) {
	task, err := s.task(ctx, taskID)
	if err != nil || task.OrchSessionID != orch.ID {
		return "", fmt.Errorf("unknown task %d", taskID)
	}
	return s.awaitTask(ctx, taskID)
}

// executeTask 在独立 goroutine 里跑完任务并统一收尾落库、广播终态——
// 与等待方完全解耦，runtime 的调用超时、连接断开都影响不到它。
func (s *Service) executeTask(orch *model.OrchSession, role *model.Role, agent *model.Agent, task model.OrchTask) {
	defer func() {
		s.mu.Lock()
		s.runningTasks[orch.ID]--
		s.mu.Unlock()
	}()
	started := time.Now()

	result, runErr := s.runTask(context.Background(), orch, role, agent, &task)

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
	if err := s.db.Save(&task).Error; err != nil {
		slog.Error("save orch task", "task", task.ID, "err", err)
	}
	s.publishTaskUpdate(orch.ID, &task)
}

// awaitTask 轮询任务终态，最多等一个同步窗口。超窗返回接力提示
// （正常文本而非错误——主控读到后调 wait_task 继续）。
func (s *Service) awaitTask(ctx context.Context, taskID uint) (string, error) {
	deadline := time.NewTimer(spawnSyncWait)
	defer deadline.Stop()
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()

	for {
		task, err := s.task(context.Background(), taskID)
		if err != nil {
			return "", err
		}
		switch task.State {
		case model.OrchTaskDone:
			return task.Result, nil
		case model.OrchTaskFailed:
			return "", fmt.Errorf("%s", task.Result)
		}
		select {
		case <-ctx.Done():
			// 调用方已断开，响应写不出去了；任务在后台继续跑。
			return "", ctx.Err()
		case <-deadline.C:
			return fmt.Sprintf("任务 #%d 仍在后台运行（角色继续工作中，成果不会丢失）。"+
				"请立即调用 wait_task(task_id=%d) 继续等待它的结果，不要重新派发。", taskID, taskID), nil
		case <-tick.C:
		}
	}
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

	// 任务不限时（与单轮 ACP_TURN_TIMEOUT 默认不限的哲学一致），且与
	// MCP 调用方的 HTTP 连接解耦：主控侧就算超时放弃，任务照跑到底、
	// 成果照常落盘落库——主控事后可从任务列表/现场捡回结果（实测它
	// 真会这么自救）。任务的死期只有用户急停与会话删除。
	taskCtx, cancel := context.WithCancel(context.Background())
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
			return "", fmt.Errorf("task cancelled: %w", taskCtx.Err())
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
