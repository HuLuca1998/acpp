package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"acpp/server/internal/datasource"
	"acpp/server/internal/mcp"
	"acpp/server/internal/mcpcall"
	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

// 工具台（页面 /tools）的管理面：看我方 MCP server 暴露了哪些工具、
// 人工试运行、发自定义 JSON-RPC、回看调用记录。
//
// 与 agent 回连的 /api/mcp/db/{token} 是**同一套工具与同一条协议路径**，
// 只是上下文来源不同：agent 拿会话 token，工具台直接给工作目录。分成
// 两个前缀是为了鉴权干净——回连端点必须公开（子进程发不出 cookie），
// 管理面则是 owner 专属，混在一个前缀下迟早出事。
type toolsHandler struct {
	sources *datasource.Service
	calls   *mcpcall.Service
}

// toolFace 是工具台看到的一个 MCP server。
type toolFace struct {
	Name string `json:"name"`
	// Endpoint 是 agent 回连的地址形状（token 随会话现签，不在这里给）。
	Endpoint string `json:"endpoint"`
	// Mounted 表示这个上下文下工具面会不会真的挂给 agent。数据源为空时
	// 一个都不挂——页面要说清这件事，否则用户会以为 AI 看得到这些工具。
	Mounted     bool              `json:"mounted"`
	SourceCount int               `json:"sourceCount"`
	Tools       []mcp.Declaration `json:"tools"`
}

// servers 列出当前上下文下的工具面。cwd 决定项目，项目决定数据源。
func (h toolsHandler) servers(w http.ResponseWriter, r *http.Request) {
	cwd := r.URL.Query().Get("cwd")

	tools, err := h.sources.InspectTools(r.Context(), cwd)
	if err != nil {
		writeError(w, err)
		return
	}
	sources, err := h.sources.ForCwd(r.Context(), cwd, true)
	if err != nil {
		writeError(w, err)
		return
	}

	face := toolFace{
		Name:        datasource.ServerName,
		Endpoint:    "/api/mcp/db/{token}",
		Mounted:     len(sources) > 0,
		SourceCount: len(sources),
		Tools:       tools,
	}
	writeData(w, http.StatusOK, newPage([]toolFace{face}))
}

// inspectInput 是试运行与自定义请求的共同入参：Request 是**原样的**
// JSON-RPC 消息体，前端填参数那套只是替用户把 tools/call 拼好。
type inspectInput struct {
	Cwd     string          `json:"cwd"`
	Request json.RawMessage `json:"request"`
}

type inspectResult struct {
	Response   json.RawMessage `json:"response,omitempty"`
	DurationMs int64           `json:"durationMs"`
	// Accepted 表示这条消息是通知、协议上就没有响应（回 202）。
	// 页面要说清「不是没结果，是它本来就不回」。
	Accepted bool `json:"accepted"`
}

// inspect 把一条 JSON-RPC 消息发给工具面，回完整响应。
func (h toolsHandler) inspect(w http.ResponseWriter, r *http.Request) {
	var in inspectInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	if len(in.Request) == 0 {
		writeError(w, fmt.Errorf("%w: request is required", service.ErrInvalid))
		return
	}

	started := time.Now()
	resp, hasResp := h.sources.InspectMCP(r.Context(), in.Cwd, in.Request)
	out := inspectResult{DurationMs: time.Since(started).Milliseconds(), Accepted: !hasResp}
	if hasResp {
		raw, err := json.Marshal(resp)
		if err != nil {
			writeError(w, err)
			return
		}
		out.Response = raw
	}
	writeData(w, http.StatusOK, out)
}

// calls 分页读调用记录。
func (h toolsHandler) callList(w http.ResponseWriter, r *http.Request) {
	// 局部变量避开 page 这个名字：它是响应外壳的类型名，遮蔽了就用不上。
	pageNo, pageSize := pageParams(r)
	q := r.URL.Query()
	filter := mcpcall.Filter{
		Server:     q.Get("server"),
		Tool:       q.Get("tool"),
		Source:     q.Get("source"),
		ErrorsOnly: q.Get("errorsOnly") == "1",
	}
	rows, total, err := h.calls.List(r.Context(), filter, pageNo, pageSize)
	if err != nil {
		writeError(w, err)
		return
	}
	if rows == nil {
		rows = []model.MCPCall{}
	}
	writeData(w, http.StatusOK, page[model.MCPCall]{
		Items: rows, Total: total, Page: pageNo, PageSize: pageSize,
	})
}

// callStats 按工具聚合调用统计。
func (h toolsHandler) callStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.calls.Stats(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, newPage(stats))
}

// callClear 清空调用记录。
func (h toolsHandler) callClear(w http.ResponseWriter, r *http.Request) {
	if err := h.calls.Clear(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, nil)
}
