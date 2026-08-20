// Package mcp 是我方 MCP server 的协议外壳：agent 回连时说的那套
// JSON-RPC，以及工具的声明与分发。
//
// 协议面刻意最小——initialize / ping / tools/list / tools/call 四个方法的
// JSON-RPC over HTTP POST（streamable http 的无流子集，实测 claude 与
// codex 两条 runtime 都接受）；不实现 GET 事件流，我们没有 server 主动
// 通知要发。
//
// 业务包（数据源）只负责「这次调用有哪些工具、工具怎么执行」，
// 外壳一份共用：多一个挂给 agent 的能力面，不该多抄一遍协议。
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
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
	// Annotations 是「这个工具会不会改东西」的提示，进 tools/list 也进
	// 工具台。可选，nil 表示不表态。
	Annotations *Annotations
	// Call 执行工具。返回的文本进 MCP 的 content；返回 error 会被包装成
	// **工具级错误**（isError:true 的文本内容）而不是 JSON-RPC error——
	// 模型能读到工具级错误并自行决策，协议错误它反而处理不了。
	Call func(ctx context.Context, args json.RawMessage) (string, error)
}

// Annotations 是 MCP 的工具注解（协议 2025-03-26 起的标准可选字段）。
//
// 我们只用「只读」与「破坏性」两位，用途有两个：给 client 一个判断依据，
// 以及让工具台知道哪些工具按下运行会真的改到线上数据——那一类要先弹确认。
//
// 它始终是**提示不是护栏**：真正拦住写操作的是数据源上的只读开关
// （datasource.runSQL），注解写错也越不过去。
type Annotations struct {
	ReadOnlyHint    bool `json:"readOnlyHint,omitempty"`
	DestructiveHint bool `json:"destructiveHint,omitempty"`
}

// Declaration 是 tools/list 里的一条工具声明。
//
// 导出它是为了让管理面（工具台页面）与协议走**同一份声明**：页面上
// 看到的描述与参数，就是模型看到的那一份，不另抄一遍也就不会抄漏。
type Declaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations *Annotations   `json:"annotations,omitempty"`
}

// Call 是一次 tools/call 的观测记录，交给 Server.OnCall。
// Result 是回给模型的文本原样——要不要截断由观测方决定，外壳不替它做主。
type Call struct {
	Server   string
	Token    string
	Tool     string
	Args     json.RawMessage
	Result   string
	IsError  bool
	Duration time.Duration
}

// Server 是一个挂给 agent 的工具面。协议由外壳处理，业务只给三件事：
// 叫什么、这次调用有哪些工具、调用完了通知谁。
type Server struct {
	// Name 进 serverInfo，也是 agent 侧工具名的前缀（mcp__<name>__<tool>）。
	Name string
	// Resolve 把端点 token 换成这次调用可用的工具集——工具按请求现算而不是
	// 启动时定死，因为它们依赖会话状态（数据源看会话所在项目有哪些库）。
	// token 即凭证：解析失败一律报同一个错，不区分「不存在」与「无权」。
	Resolve func(ctx context.Context, token string) ([]Tool, error)
	// OnCall 在每次 tools/call 结束后调用（成功与失败都调），供上层记调用
	// 统计。它在请求路径上同步执行，实现必须快；观测失败不该影响工具本身，
	// 所以它不返回错误。为 nil 时不观测。
	OnCall func(ctx context.Context, rec Call)
}

// Serve 处理一条发到 MCP 端点的消息。
//
// 返回 (响应, 是否有响应)：通知类消息无响应（调用方回 HTTP 202）。
func (s Server) Serve(ctx context.Context, token string, raw []byte) (any, bool) {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return Errorf(nil, -32700, "parse error"), true
	}

	tools, err := s.Resolve(ctx, token)
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
			"serverInfo":      map[string]any{"name": s.Name, "version": "1"},
		}}, true

	case "ping":
		return Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}, true

	case "tools/list":
		return Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"tools": Declare(tools),
		}}, true

	case "tools/call":
		return s.call(ctx, token, req, tools), true
	}

	if req.ID == nil {
		// notifications/initialized 等通知：无响应。
		return nil, false
	}
	return Errorf(req.ID, -32601, "method %q not found", req.Method), true
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

// Declare 把工具集转成 tools/list 的条目形状。
func Declare(tools []Tool) []Declaration {
	out := make([]Declaration, 0, len(tools))
	for _, t := range tools {
		out = append(out, Declaration{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
			Annotations: t.Annotations,
		})
	}
	return out
}

func (s Server) call(ctx context.Context, token string, req Request, tools []Tool) Response {
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
		started := time.Now()
		result, err := t.Call(ctx, params.Arguments)
		text, isError := result, false
		if err != nil {
			text, isError = err.Error(), true
		}
		s.observe(ctx, Call{
			Server:   s.Name,
			Token:    token,
			Tool:     params.Name,
			Args:     params.Arguments,
			Result:   text,
			IsError:  isError,
			Duration: time.Since(started),
		})
		// isError 只在真出错时才写：协议上它可选且默认 false，两条 runtime
		// 都是按这个形状实测通过的，不为了对称多塞一个字段。
		payload := map[string]any{
			"content": []any{map[string]any{"type": "text", "text": text}},
		}
		if isError {
			payload["isError"] = true
		}
		return Response{JSONRPC: "2.0", ID: req.ID, Result: payload}
	}
	return Errorf(req.ID, -32602, "unknown tool %q", params.Name)
}

func (s Server) observe(ctx context.Context, rec Call) {
	if s.OnCall == nil {
		return
	}
	s.OnCall(ctx, rec)
}
