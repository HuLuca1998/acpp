package httpapi

import (
	"net/http"

	"acpp/server/internal/model"
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
	pageNum := queryInt(r, "page", 1)
	pageSize := queryInt(r, "pageSize", 50)

	sessions, total, err := h.sessions.List(r.Context(), scopeOf(r), agentID, pageNum, pageSize)
	if err != nil {
		writeError(w, err)
		return
	}
	// 消息数走缓存列；这里只补实时的进程状态。
	for i := range sessions {
		sessions[i].Running = h.chat.Running(sessions[i].ID)
	}
	writeData(w, http.StatusOK, page[service.SessionView]{
		Items:    sessions,
		Total:    total,
		Page:     pageNum,
		PageSize: pageSize,
	})
}

func (h sessionHandler) get(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	// 归属先行：不属于当前身份的会话当作不存在（adr-007）。
	if _, err := h.sessions.Get(r.Context(), scopeOf(r), id); err != nil {
		writeError(w, err)
		return
	}

	// Peek 不拉进程：查看记录是零成本读操作。
	session, err := h.chat.Peek(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, session)
}

func (h sessionHandler) create(w http.ResponseWriter, r *http.Request) {
	var in service.SessionInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}

	session, err := h.sessions.Create(r.Context(), scopeOf(r), in)
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

	if _, err := h.sessions.Get(r.Context(), scopeOf(r), id); err != nil {
		writeError(w, err)
		return
	}

	// 先收子进程再删记录，否则 agent 会变成没人认领的孤儿进程。
	if err := h.chat.Destroy(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	if err := h.sessions.Delete(r.Context(), scopeOf(r), id); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, nil)
}

// listMessages 从转录重建消息列表；会话必须存在。
// ?limit= 取尾部 N 条（默认全量），?before=<id> 是「加载更早」的游标。
func (h sessionHandler) listMessages(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	if _, err := h.sessions.Get(r.Context(), scopeOf(r), id); err != nil {
		writeError(w, err)
		return
	}

	limit := queryInt(r, "limit", 0)
	before := uint(queryInt(r, "before", 0))
	messages, total, err := h.chat.Messages(id, limit, before)
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

// transcript 原样下发会话的 JSONL 转录，供导出与调试。
func (h sessionHandler) transcript(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	if _, err := h.sessions.Get(r.Context(), scopeOf(r), id); err != nil {
		writeError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	http.ServeFile(w, r, h.chat.TranscriptPath(id))
}
