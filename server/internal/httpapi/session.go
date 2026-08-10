package httpapi

import (
	"net/http"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

type sessionHandler struct {
	sessions *service.SessionService
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

	if err := h.sessions.Delete(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, nil)
}

func (h sessionHandler) listMessages(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	messages, err := h.sessions.Messages(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, newPage(messages))
}

func (h sessionHandler) createMessage(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	var in struct {
		Role    model.MessageRole `json:"role"`
		Kind    model.MessageKind `json:"kind"`
		Content string            `json:"content"`
		Payload model.JSONMap     `json:"payload"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}

	msg, err := h.sessions.AppendMessage(r.Context(), id, model.Message{
		Role:    in.Role,
		Kind:    in.Kind,
		Content: in.Content,
		Payload: in.Payload,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusCreated, msg)
}
