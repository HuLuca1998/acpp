package httpapi

import (
	"net/http"

	"acpp/server/internal/service"
)

type agentHandler struct {
	agents *service.AgentService
	// chat 用于探测 agent 的模型清单（要拉临时会话，归 ChatService 管）。
	chat *service.ChatService
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
	// 后台探测模型清单，前端刷新列表时拿到结果。
	h.chat.ProbeAgentAsync(agent.ID)
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
	// 命令/环境可能变了，模型清单跟着重探。
	h.chat.ProbeAgentAsync(agent.ID)
	writeData(w, http.StatusOK, agent)
}

// catalog 更新配置页的勾选（models/commands 的启用状态）。
func (h agentHandler) catalog(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	var in service.CatalogInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}

	agent, err := h.agents.UpdateCatalog(r.Context(), id, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, agent)
}

// probe 同步探测 agent 的模型清单，返回带缓存结果的最新记录。
func (h agentHandler) probe(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	agent, err := h.chat.ProbeAgent(r.Context(), id)
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
