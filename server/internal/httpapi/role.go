package httpapi

import (
	"acpp/server/internal/model"
	"net/http"

	"acpp/server/internal/orch"
)

type roleHandler struct {
	roles *orch.RoleService
}

func (h roleHandler) list(w http.ResponseWriter, r *http.Request) {
	pageNum, pageSize := pageParams(r)

	roles, total, err := h.roles.ListPage(r.Context(), pageNum, pageSize)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, page[model.Role]{
		Items:    roles,
		Total:    total,
		Page:     pageNum,
		PageSize: pageSize,
	})
}

func (h roleHandler) get(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	role, err := h.roles.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, role)
}

func (h roleHandler) create(w http.ResponseWriter, r *http.Request) {
	var in orch.RoleInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	role, err := h.roles.Create(r.Context(), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusCreated, role)
}

func (h roleHandler) update(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	var in orch.RoleInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	role, err := h.roles.Update(r.Context(), id, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, role)
}

func (h roleHandler) remove(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	if err := h.roles.Delete(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, nil)
}
