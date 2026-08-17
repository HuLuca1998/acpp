package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"acpp/server/internal/acp"
	"acpp/server/internal/service"
)

type chatHandler struct {
	chat     *service.ChatService
	sessions *service.SessionService
}

// guard 是对话面的归属闸：会话不属于当前身份就当作不存在（adr-007）。
// 每个入口都要过一次——ChatService 内部按 id 直取，没有第二道闸。
func (h chatHandler) guard(r *http.Request, id uint) error {
	_, err := h.sessions.Get(r.Context(), scopeOf(r), id)
	return err
}

// open 拉起 agent 并完成握手。前端进入会话页时调一次。
func (h chatHandler) open(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	if err := h.guard(r, id); err != nil {
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
	if err := h.guard(r, id); err != nil {
		writeError(w, err)
		return
	}

	var in service.SendInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}

	msg, err := h.chat.Send(r.Context(), id, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusAccepted, msg)
}

// settings 应用统一设置变更（模型/思考深度/权限档/plan/fast，逐项可选）。
func (h chatHandler) settings(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	if err := h.guard(r, id); err != nil {
		writeError(w, err)
		return
	}

	var in acp.SettingsPatch
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}

	settings, err := h.chat.ApplySettings(r.Context(), id, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, settings)
}

// permission 把用户对权限请求的裁决回给阻塞中的 agent。
func (h chatHandler) permission(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	if err := h.guard(r, id); err != nil {
		writeError(w, err)
		return
	}

	var in struct {
		PermissionID string `json:"permissionId"`
		// OptionID 空串表示取消。
		OptionID string `json:"optionId"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}

	if err := h.chat.ResolvePermission(id, in.PermissionID, in.OptionID); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, nil)
}

// elicitation 把用户对交互式提问的作答回给阻塞中的 agent。
func (h chatHandler) elicitation(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	if err := h.guard(r, id); err != nil {
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
	if err := h.guard(r, id); err != nil {
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
	if err := h.guard(r, id); err != nil {
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
