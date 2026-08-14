package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
	"acpp/server/internal/orch"
	"acpp/server/internal/service"
)

type orchHandler struct {
	orch *orch.Service
}

func (h orchHandler) list(w http.ResponseWriter, r *http.Request) {
	pageNum := queryInt(r, "page", 1)
	pageSize := queryInt(r, "pageSize", 20)
	items, total, err := h.orch.List(r.Context(), pageNum, pageSize)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, page[model.OrchSession]{
		Items:    items,
		Total:    total,
		Page:     pageNum,
		PageSize: pageSize,
	})
}

func (h orchHandler) create(w http.ResponseWriter, r *http.Request) {
	var in orch.SessionInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	orch, err := h.orch.Create(r.Context(), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusCreated, orch)
}

func (h orchHandler) get(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	orch, err := h.orch.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, orch)
}

func (h orchHandler) remove(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	if err := h.orch.Delete(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, nil)
}

func (h orchHandler) send(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	var in service.SendInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	msg, err := h.orch.Send(r.Context(), id, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusAccepted, msg)
}

func (h orchHandler) cancel(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	if err := h.orch.Cancel(id); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, nil)
}

// stop 是急停：中止主会话与全部在跑子任务。
func (h orchHandler) stop(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	if _, err := h.orch.Get(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	h.orch.Stop(r.Context(), id)
	writeData(w, http.StatusOK, nil)
}

func (h orchHandler) settings(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	var in acp.SettingsPatch
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	settings, err := h.orch.ApplySettings(r.Context(), id, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, settings)
}

func (h orchHandler) getSettings(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	settings, err := h.orch.Settings(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, settings)
}

func (h orchHandler) permission(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	var in struct {
		PermissionID string `json:"permissionId"`
		OptionID     string `json:"optionId"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	if err := h.orch.ResolvePermission(id, in.PermissionID, in.OptionID); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, nil)
}

func (h orchHandler) elicitation(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	var in struct {
		ElicitationID string         `json:"elicitationId"`
		Action        string         `json:"action"`
		Content       map[string]any `json:"content"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	if err := h.orch.ResolveElicitation(id, in.ElicitationID, in.Action, in.Content); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, nil)
}

func (h orchHandler) messages(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	if _, err := h.orch.Get(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	limit := queryInt(r, "limit", 0)
	before := uint(queryInt(r, "before", 0))
	messages, total, err := h.orch.Messages(id, limit, before)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, page[model.Message]{
		Items:    messages,
		Total:    int64(total),
		Page:     1,
		PageSize: len(messages),
	})
}

func (h orchHandler) events(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	events, cancel := h.orch.Subscribe(id)
	defer cancel()
	streamSSE(w, r, events)
}

// transcript 原样下发主会话转录 JSONL（logs 面板轮询靠 Range 续读）。
func (h orchHandler) transcript(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	if _, err := h.orch.Get(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	http.ServeFile(w, r, h.orch.TranscriptPath(id))
}

// ---- 任务子会话 ----

func (h orchHandler) tasks(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	tasks, err := h.orch.Tasks(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, tasks)
}

func (h orchHandler) taskMessages(w http.ResponseWriter, r *http.Request) {
	tid, err := pathID(r, "tid")
	if err != nil {
		writeError(w, err)
		return
	}
	limit := queryInt(r, "limit", 0)
	before := uint(queryInt(r, "before", 0))
	messages, total, err := h.orch.TaskMessages(tid, limit, before)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, page[model.Message]{
		Items:    messages,
		Total:    int64(total),
		Page:     1,
		PageSize: len(messages),
	})
}

func (h orchHandler) taskEvents(w http.ResponseWriter, r *http.Request) {
	tid, err := pathID(r, "tid")
	if err != nil {
		writeError(w, err)
		return
	}
	events, cancel := h.orch.SubscribeTask(tid)
	defer cancel()
	streamSSE(w, r, events)
}

func (h orchHandler) taskCancel(w http.ResponseWriter, r *http.Request) {
	tid, err := pathID(r, "tid")
	if err != nil {
		writeError(w, err)
		return
	}
	if err := h.orch.CancelTask(tid); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, nil)
}

func (h orchHandler) taskPermission(w http.ResponseWriter, r *http.Request) {
	tid, err := pathID(r, "tid")
	if err != nil {
		writeError(w, err)
		return
	}
	var in struct {
		PermissionID string `json:"permissionId"`
		OptionID     string `json:"optionId"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	if err := h.orch.ResolveTaskPermission(tid, in.PermissionID, in.OptionID); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, nil)
}

func (h orchHandler) taskElicitation(w http.ResponseWriter, r *http.Request) {
	tid, err := pathID(r, "tid")
	if err != nil {
		writeError(w, err)
		return
	}
	var in struct {
		ElicitationID string         `json:"elicitationId"`
		Action        string         `json:"action"`
		Content       map[string]any `json:"content"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	if err := h.orch.ResolveTaskElicitation(tid, in.ElicitationID, in.Action, in.Content); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, nil)
}

// ---- MCP 端点 ----

// mcp 处理 agent 子进程的 MCP 回连（JSON-RPC over POST）。
// 不走统一响应外壳——MCP 有自己的 JSON-RPC 形状；GET（server 主动
// 事件流）不支持，按规范回 405。
func (h orchHandler) mcp(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	switch r.Method {
	case http.MethodPost:
	case http.MethodGet:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	case http.MethodDelete:
		// session 终止通知：无状态实现，直接应答。
		w.WriteHeader(http.StatusOK)
		return
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	resp, hasResp := h.orch.HandleMCP(r.Context(), token, raw)
	if !hasResp {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		// 连接已断（spawn 等待中 client 超时放弃是常态），只能放弃响应。
		return
	}
}

// streamSSE 是 SSE 下发的共用循环（chatHandler.events 的翻版，供编排的
// 主/任务两路流复用）。
func streamSSE(w http.ResponseWriter, r *http.Request, events <-chan service.StreamEvent) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, envelope{Error: "streaming unsupported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			payload, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
