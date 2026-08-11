package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"acpp/server/internal/service"
)

type chatHandler struct {
	chat *service.ChatService
}

// open 拉起 agent 并完成握手。前端进入会话页时调一次。
func (h chatHandler) open(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	view, err := h.chat.Open(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, view)
}

// send 收下用户消息并立刻返回。agent 的回复通过 events 端点流式推出去。
func (h chatHandler) send(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	var in struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}

	msg, err := h.chat.Send(r.Context(), id, in.Content)
	if err != nil {
		if errors.Is(err, service.ErrBusy) {
			writeJSON(w, http.StatusConflict, envelope{Error: err.Error()})
			return
		}
		writeError(w, err)
		return
	}
	writeData(w, http.StatusAccepted, msg)
}

// setMode 切换会话的审批/沙箱模式。
func (h chatHandler) setMode(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	var in struct {
		ModeID string `json:"modeId"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}

	caps, err := h.chat.SetMode(r.Context(), id, in.ModeID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, caps)
}

// setModel 切换会话模型。
func (h chatHandler) setModel(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	var in struct {
		ModelID string `json:"modelId"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}

	caps, err := h.chat.SetModel(r.Context(), id, in.ModelID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, caps)
}

// setConfig 设置会话配置项（协作模式、推理档等）。
func (h chatHandler) setConfig(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	var in struct {
		ConfigID string `json:"configId"`
		Value    string `json:"value"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}

	caps, err := h.chat.SetConfigOption(r.Context(), id, in.ConfigID, in.Value)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, caps)
}

// elicitation 把用户对交互式提问的作答回给阻塞中的 agent。
func (h chatHandler) elicitation(w http.ResponseWriter, r *http.Request) {
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

	if err := h.chat.ResolveElicitation(id, in.ElicitationID, in.Action, in.Content); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, nil)
}

func (h chatHandler) cancel(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	if err := h.chat.Cancel(id); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, nil)
}

// events 是 SSE 流。chunk 一到就写出并 flush，中间层不攒——
// 用户应该在第一个 token 到达时看到它，而不是在整轮结束时。
func (h chatHandler) events(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, envelope{Error: "streaming unsupported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// 关掉 nginx 一类反代的缓冲，否则 SSE 会被攒起来一次性下发。
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	events, cancel := h.chat.Subscribe(id)
	defer cancel()

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
