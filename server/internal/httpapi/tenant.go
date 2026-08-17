package httpapi

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"acpp/server/internal/service"
)

// tenantHandler 是 owner 专属的租户管理面（路由前缀已在 isOwnerOnly 覆盖）。
type tenantHandler struct {
	tenants *service.TenantService
	// addr 是服务监听地址，用来拼可以直接发出去的邀请链接。
	addr string
}

// tenantView 在租户之上补一条可直接转发的邀请链接。owner 在本机看到的是
// 127.0.0.1，把那个地址发给局域网里的人是打不开的——链接由后端拼好，
// 界面不用猜自己该显示哪个 IP。
type tenantView struct {
	service.TenantView
	InviteURL string `json:"inviteUrl"`
}

func (h tenantHandler) list(w http.ResponseWriter, r *http.Request) {
	tenants, err := h.tenants.List(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	views := make([]tenantView, 0, len(tenants))
	for _, tenant := range tenants {
		views = append(views, h.withInviteURL(tenant))
	}
	writeData(w, http.StatusOK, newPage(views))
}

// withInviteURL 拼 `http://<局域网IP>:<端口>/?invite=<token>`。
func (h tenantHandler) withInviteURL(tenant service.TenantView) tenantView {
	host := lanIP()
	_, port, err := net.SplitHostPort(h.addr)
	if err != nil || port == "" {
		port = "48080"
	}
	return tenantView{
		TenantView: tenant,
		InviteURL:  fmt.Sprintf("http://%s:%s/?invite=%s", host, port, tenant.InviteToken),
	}
}

// lanIP 取本机第一个非回环 IPv4。拿不到就退回 localhost——链接会「只能
// 本机用」，但至少是个能点开的链接，比空字符串强。
func lanIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "localhost"
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() || ipNet.IP.To4() == nil {
			continue
		}
		// 链路本地地址（169.254.x.x）是没配到 DHCP 时的自说自话，发出去没用。
		if strings.HasPrefix(ipNet.IP.String(), "169.254.") {
			continue
		}
		return ipNet.IP.String()
	}
	return "localhost"
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
	writeData(w, http.StatusCreated, h.withInviteURL(*tenant))
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
	writeData(w, http.StatusOK, h.withInviteURL(*tenant))
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
