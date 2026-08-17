package httpapi

import (
	"net/http"

	"acpp/server/internal/service"
)

// tenantHandler 是 owner 专属的租户管理面（路由前缀已在 isOwnerOnly 覆盖）。
type tenantHandler struct {
	tenants *service.TenantService
}

func (h tenantHandler) list(w http.ResponseWriter, r *http.Request) {
	tenants, err := h.tenants.List(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, newPage(tenants))
}

func (h tenantHandler) create(w http.ResponseWriter, r *http.Request) {
	var in service.TenantInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	tenant, err := h.tenants.Create(r.Context(), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusCreated, tenant)
}

// rotate 重新生成分享链接：旧链接立刻作废，会话与目录不动。
func (h tenantHandler) rotate(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	tenant, err := h.tenants.Rotate(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, tenant)
}

func (h tenantHandler) update(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	var patch service.TenantPatch
	if err := decodeJSON(r, &patch); err != nil {
		writeError(w, err)
		return
	}
	tenant, err := h.tenants.Update(r.Context(), id, patch)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, tenant)
}

func (h tenantHandler) remove(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	if err := h.tenants.Delete(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, nil)
}
