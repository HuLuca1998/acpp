// Package mcp 是我方 MCP server 的协议外壳：agent 回连时说的那套
// JSON-RPC，以及工具的声明与分发。
//
// 协议面刻意最小——initialize / ping / tools/list / tools/call 四个方法的
// JSON-RPC over HTTP POST（streamable http 的无流子集，实测 claude 与
// codex 两条 runtime 都接受）；不实现 GET 事件流，我们没有 server 主动
// 通知要发。
//
// 业务包（编排、数据源）只负责「这次调用有哪些工具、工具怎么执行」，
// 外壳一份共用：多一个挂给 agent 的能力面，不该多抄一遍协议。
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

// ProtocolVersion 是我们回给 client 的兜底版本；client 报了就回显它的
// （版本协商的宽松侧）。
const ProtocolVersion = "2025-06-18"

// Tool 是一个可被模型调用的工具。Description 写给模型看——它是「什么
// 时候会被调用」的真正开关，比名字重要得多。
type Tool struct {
	Name        string
	Description string
	// InputSchema 是 JSON Schema（object），原样进 tools/list。
	InputSchema map[string]any
	// Call 执行工具。返回的文本进 MCP 的 content；返回 error 会被包装成
	// **工具级错误**（isError:true 的文本内容）而不是 JSON-RPC error——
	// 模型能读到工具级错误并自行决策，协议错误它反而处理不了。
	Call func(ctx context.Context, args json.RawMessage) (string, error)
}

// Request 是一条 JSON-RPC 请求。ID 为 nil 表示通知（无响应）。
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response 是一条 JSON-RPC 响应。
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Errorf 构造一条 JSON-RPC 错误响应。
func Errorf(id json.RawMessage, code int, format string, args ...any) Response {
	return Response{JSONRPC: "2.0", ID: id, Error: &Error{Code: code, Message: fmt.Sprintf(format, args...)}}
}

// Serve 处理一条发到 MCP 端点的消息。
//
// name 是 serverInfo 里的名字；resolve 把端点的 token 换成这次调用可用的
// 工具集——工具按请求现算而不是启动时定死，因为它们依赖会话状态
// （编排看当前有哪些角色，数据源看会话所在项目有哪些库）。
// token 即凭证：解析失败一律报同一个错，不区分「不存在」与「无权」。
//
// 返回 (响应, 是否有响应)：通知类消息无响应（调用方回 HTTP 202）。
func Serve(ctx context.Context, name, token string, raw []byte,
	resolve func(context.Context, string) ([]Tool, error)) (any, bool) {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return Errorf(nil, -32700, "parse error"), true
	}

	tools, err := resolve(ctx, token)
	if err != nil {
		return Errorf(req.ID, -32000, "unknown mcp endpoint"), true
	}

	switch req.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &params)
		version := params.ProtocolVersion
		if version == "" {
			version = ProtocolVersion
		}
		return Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": name, "version": "1"},
		}}, true

	case "ping":
		return Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}, true

	case "tools/list":
		return Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"tools": declare(tools),
		}}, true

	case "tools/call":
		return call(ctx, req, tools), true
	}

	if req.ID == nil {
		// notifications/initialized 等通知：无响应。
		return nil, false
	}
	return Errorf(req.ID, -32601, "method %q not found", req.Method), true
}

func declare(tools []Tool) []any {
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		})
	}
	return out
}

func call(ctx context.Context, req Request, tools []Tool) Response {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return Errorf(req.ID, -32602, "bad tools/call params")
	}
	for _, t := range tools {
		if t.Name != params.Name {
			continue
		}
		result, err := t.Call(ctx, params.Arguments)
		if err != nil {
			return Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
				"content": []any{map[string]any{"type": "text", "text": err.Error()}},
				"isError": true,
			}}
		}
		return Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"content": []any{map[string]any{"type": "text", "text": result}},
		}}
	}
	return Errorf(req.ID, -32602, "unknown tool %q", params.Name)
}
