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
	// Shareable 表示这条链接现在真的能发出去用。服务只监听回环时它是
	// false——那种情况下给一条 `http://192.168.x.x:...` 的链接是骗人的，
	// 谁点都连不上。界面据此说明「先开启局域网访问」。
	Shareable bool `json:"shareable"`
}

func (h tenantHandler) list(w http.ResponseWriter, r *http.Request) {
	pageNum, pageSize := pageParams(r)
	sort := sortParams(r, "name", "root", "disabled", "last_seen_at", "created_at")
	views, total, err := h.tenants.List(r.Context(), pageNum, pageSize, sort.OrderBy(""))
	if err != nil {
		writeError(w, err)
		return
	}
	// 邀请链接由后端拼：host 取自监听地址，前端不必知道服务跑在哪。
	out := make([]tenantView, 0, len(views))
	for _, v := range views {
		out = append(out, h.withInviteURL(v))
	}
	writeData(w, http.StatusOK, page[tenantView]{
		Items:    out,
		Total:    total,
		Page:     pageNum,
		PageSize: pageSize,
	})
}

// withInviteURL 拼 `http://<局域网IP>:<端口>/?invite=<token>`，并如实标出
// 这条链接当下能不能真的发出去。
func (h tenantHandler) withInviteURL(tenant service.TenantView) tenantView {
	host, port, err := net.SplitHostPort(h.addr)
	if err != nil || port == "" {
		port = "48080"
	}
	shareable := listensBeyondLoopback(host)

	// 只监听回环时给本机地址：链接至少能在这台机器上点开（owner 想验一眼
	// 访客视角就靠它），而不是一条谁都连不上的局域网地址。
	target := lanIP()
	if !shareable {
		target = "127.0.0.1"
	}
	return tenantView{
		TenantView: tenant,
		InviteURL:  fmt.Sprintf("http://%s:%s/?invite=%s", target, port, tenant.InviteToken),
		Shareable:  shareable,
	}
}

// listensBeyondLoopback 判断监听地址是否对本机之外开放。
// 空 host（":48080"）与 0.0.0.0 都是全网卡监听。
func listensBeyondLoopback(host string) bool {
	if host == "" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// 主机名（少见）：无从判断，按能分享处理，界面不该无端阻拦。
		return true
	}
	return !ip.IsLoopback()
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
