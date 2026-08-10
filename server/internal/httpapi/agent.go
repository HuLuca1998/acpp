package httpapi

import (
	"net/http"

	"acpp/server/internal/service"
)

type agentHandler struct {
	agents *service.AgentService
}

func (h agentHandler) list(w http.ResponseWriter, r *http.Request) {
	agents, err := h.agents.List(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, newPage(agents))
}

func (h agentHandler) get(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	agent, err := h.agents.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, agent)
}

func (h agentHandler) create(w http.ResponseWriter, r *http.Request) {
	var in service.AgentInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}

	agent, err := h.agents.Create(r.Context(), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusCreated, agent)
}

func (h agentHandler) update(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	var in service.AgentInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}

	agent, err := h.agents.Update(r.Context(), id, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, agent)
}

func (h agentHandler) remove(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	if err := h.agents.Delete(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, nil)
}
