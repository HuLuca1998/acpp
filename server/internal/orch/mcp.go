package orch

import (
	"context"
	"encoding/json"
	"fmt"

	"acpp/server/internal/mcp"
	"acpp/server/internal/model"
)

// 编排 MCP server：把派发能力（hire_role / wait_task）以 MCP 工具的形式
// 挂给编排主会话。协议外壳在 internal/mcp，这里只声明工具与执行。

// spawnAgentTool 是 hire_role 的工具声明。名字刻意避开 codex 内部
// collaboration 工具族（spawn_agent/wait/close_agent…）：撞名时模型会
// 优先调内部工具，派发悄悄流进黑盒子代理（实测踩坑）。
func (s *Service) spawnAgentTool(orch *model.OrchSession) mcp.Tool {
	return mcp.Tool{
		Name: "hire_role",
		Description: "雇佣一个角色子代理完成子任务。会阻塞直到子代理完成并返回其最终结论，" +
			"耗时可能较长，属正常。task 必须自包含（背景/目标/涉及路径/验收标准），" +
			"子代理看不到你的对话上下文。需要并行时在同一条消息里发多个调用。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"role": map[string]any{
					"type":        "string",
					"description": "角色名（见系统提示里的可用角色清单）",
				},
				"task": map[string]any{
					"type":        "string",
					"description": "任务全文：背景、目标、涉及路径、验收标准",
				},
			},
			"required": []string{"role", "task"},
		},
		Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				Role string `json:"role"`
				Task string `json:"task"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("子任务失败：参数解析失败: %v", err)
			}
			result, err := s.SpawnAgent(ctx, orch, args.Role, args.Task)
			if err != nil {
				return "", fmt.Errorf("子任务失败：%w", err)
			}
			return result, nil
		},
	}
}

// waitTaskTool 是 hire_role 的同步窗口耗尽后（长任务）的接力入口——
// 任务在后台照跑，成果不丢。
func (s *Service) waitTaskTool(orch *model.OrchSession) mcp.Tool {
	return mcp.Tool{
		Name: "wait_task",
		Description: "继续等待一个后台任务的结果。hire_role 返回「任务仍在后台运行」的提示时调用它，" +
			"直到拿到任务结论；不要因为等待就重新派发同一个任务。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{
					"type":        "number",
					"description": "hire_role 提示里给出的任务编号",
				},
			},
			"required": []string{"task_id"},
		},
		Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				TaskID float64 `json:"task_id"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("子任务失败：参数解析失败: %v", err)
			}
			result, err := s.WaitTask(ctx, orch, uint(args.TaskID))
			if err != nil {
				return "", fmt.Errorf("子任务失败：%w", err)
			}
			return result, nil
		},
	}
}

// HandleMCP 处理一条发到 /api/mcp/{token} 的 JSON-RPC 消息。
func (s *Service) HandleMCP(ctx context.Context, token string, raw []byte) (any, bool) {
	return mcp.Serve(ctx, "acpp", token, raw, func(ctx context.Context, token string) ([]mcp.Tool, error) {
		orch, err := s.byMCPToken(ctx, token)
		if err != nil {
			return nil, err
		}
		return []mcp.Tool{s.spawnAgentTool(orch), s.waitTaskTool(orch)}, nil
	})
}
