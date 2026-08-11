package httpapi

import (
	"net/http"

	"acpp/server/internal/service"
)

type sessionHandler struct {
	sessions *service.SessionService
	chat     *service.ChatService
}

func (h sessionHandler) list(w http.ResponseWriter, r *http.Request) {
	var agentID uint
	if raw := r.URL.Query().Get("agentId"); raw != "" {
		id, err := parseID(raw)
		if err != nil {
			writeError(w, err)
			return
		}
		agentID = id
	}

	sessions, err := h.sessions.List(r.Context(), agentID)
	if err != nil {
		writeError(w, err)
		return
	}
	for i := range sessions {
		sessions[i].Running = h.chat.Running(sessions[i].ID)
		sessions[i].MessageCount = int64(h.chat.MessageCount(sessions[i].ID))
	}
	writeData(w, http.StatusOK, newPage(sessions))
}

func (h sessionHandler) get(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	session, err := h.sessions.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	session.Running = h.chat.Running(id)
	session.MessageCount = int64(h.chat.MessageCount(id))
	writeData(w, http.StatusOK, session)
}

func (h sessionHandler) create(w http.ResponseWriter, r *http.Request) {
	var in service.SessionInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}

	session, err := h.sessions.Create(r.Context(), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusCreated, session)
}

func (h sessionHandler) remove(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	// 先收子进程再删记录，否则 agent 会变成没人认领的孤儿进程。
	if err := h.chat.Destroy(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	if err := h.sessions.Delete(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, nil)
}

// listMessages 从转录重建消息列表；会话必须存在。
func (h sessionHandler) listMessages(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	if _, err := h.sessions.Get(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}

	messages, err := h.chat.Messages(id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, newPage(messages))
}

// transcript 原样下发会话的 JSONL 转录，供导出与调试。
func (h sessionHandler) transcript(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	if _, err := h.sessions.Get(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	http.ServeFile(w, r, h.chat.TranscriptPath(id))
}
