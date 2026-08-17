package orch

import (
	"context"
	"encoding/json"
	"fmt"

	"acpp/server/internal/model"
)

// 编排 MCP server：把系统能力（spawn_agent）以 MCP 工具的形式挂给编排
// 主会话。协议面刻意最小——initialize / tools/list / tools/call 三个方法
// 的 JSON-RPC over HTTP POST（streamable http 的无流子集，实测两端
// runtime 都接受）；不实现 GET 事件流（无 server 主动通知）。

// mcpProtocolVersion 是我们回给 client 的兜底版本；client 报了就回显
// 它的（版本协商的宽松侧）。
const mcpProtocolVersion = "2025-06-18"

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// spawnAgentTool 是 hire_role 的 MCP 工具声明。描述写给主会话的模型看
// ——它是「什么时候会被调用」的真正开关。名字刻意避开 codex 内部
// collaboration 工具族（spawn_agent/wait/close_agent…）：撞名时模型会
// 优先调内部工具，派发悄悄流进黑盒子代理（实测踩坑）。
var spawnAgentTool = map[string]any{
	"name": "hire_role",
	"description": "雇佣一个角色子代理完成子任务。会阻塞直到子代理完成并返回其最终结论，" +
		"耗时可能较长，属正常。task 必须自包含（背景/目标/涉及路径/验收标准），" +
		"子代理看不到你的对话上下文。需要并行时在同一条消息里发多个调用。",
	"inputSchema": map[string]any{
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
}

// waitTaskTool 是 wait_task 的 MCP 工具声明：hire_role 的同步窗口耗尽
// 后（长任务），主控用它接力等待——任务在后台照跑，成果不丢。
var waitTaskTool = map[string]any{
	"name": "wait_task",
	"description": "继续等待一个后台任务的结果。hire_role 返回「任务仍在后台运行」的提示时调用它，" +
		"直到拿到任务结论；不要因为等待就重新派发同一个任务。",
	"inputSchema": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{
				"type":        "number",
				"description": "hire_role 提示里给出的任务编号",
			},
		},
		"required": []string{"task_id"},
	},
}

// HandleMCP 处理一条发到 /api/mcp/{token} 的 JSON-RPC 消息。
// 返回 (响应对象, 是否有响应)：通知类消息无响应（HTTP 202）。
// token 即凭证——解析不到就报错，不区分「不存在」与「无权」。
func (s *Service) HandleMCP(ctx context.Context, token string, raw []byte) (any, bool) {
	var req mcpRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return mcpResponse{JSONRPC: "2.0", Error: &mcpError{Code: -32700, Message: "parse error"}}, true
	}

	orch, err := s.byMCPToken(ctx, token)
	if err != nil {
		return mcpResponse{JSONRPC: "2.0", ID: req.ID,
			Error: &mcpError{Code: -32000, Message: "unknown mcp endpoint"}}, true
	}

	switch req.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &params)
		version := params.ProtocolVersion
		if version == "" {
			version = mcpProtocolVersion
		}
		return mcpResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "acpp", "version": "1"},
		}}, true

	case "ping":
		return mcpResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}, true

	case "tools/list":
		return mcpResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"tools": []any{spawnAgentTool, waitTaskTool},
		}}, true

	case "tools/call":
		return s.mcpToolCall(ctx, orch, req), true
	}

	if req.ID == nil {
		// notifications/initialized 等通知：无响应。
		return nil, false
	}
	return mcpResponse{JSONRPC: "2.0", ID: req.ID,
		Error: &mcpError{Code: -32601, Message: fmt.Sprintf("method %q not found", req.Method)}}, true
}

// mcpToolCall 执行工具调用。错误走 MCP 的工具级错误（isError:true 的
// 文本内容）而不是 JSON-RPC error——模型能读到并自行决策，协议错误
// 它反而处理不了。
func (s *Service) mcpToolCall(ctx context.Context, orch *model.OrchSession, req mcpRequest) mcpResponse {
	var params struct {
		Name      string `json:"name"`
		Arguments struct {
			Role   string  `json:"role"`
			Task   string  `json:"task"`
			TaskID float64 `json:"task_id"`
		} `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return mcpResponse{JSONRPC: "2.0", ID: req.ID,
			Error: &mcpError{Code: -32602, Message: "bad tools/call params"}}
	}

	var result string
	var err error
	switch params.Name {
	case "hire_role":
		result, err = s.SpawnAgent(ctx, orch, params.Arguments.Role, params.Arguments.Task)
	case "wait_task":
		result, err = s.WaitTask(ctx, orch, uint(params.Arguments.TaskID))
	default:
		return mcpResponse{JSONRPC: "2.0", ID: req.ID,
			Error: &mcpError{Code: -32602, Message: fmt.Sprintf("unknown tool %q", params.Name)}}
	}

	if err != nil {
		return mcpResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"content": []any{map[string]any{"type": "text", "text": "子任务失败：" + err.Error()}},
			"isError": true,
		}}
	}
	return mcpResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
		"content": []any{map[string]any{"type": "text", "text": result}},
	}}
}
