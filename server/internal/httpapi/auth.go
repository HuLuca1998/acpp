package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

// tenantCookie 是身份凭证的载体。选 cookie 而不是 Authorization header，
// 是因为 SSE（EventSource）与工作区终端（WebSocket）都带不了自定义
// header——三条通道要统一鉴权，只有 cookie 能做到（adr-007）。
const tenantCookie = "acpp_tenant"

// cookieMaxAge 是凭证有效期。停用租户是即时生效的（Authenticate 查库），
// 所以这里可以给得长，省得局域网用户隔三差五重新点邀请链接。
const cookieMaxAge = 365 * 24 * time.Hour

// identity 是一次请求的身份。四种状态：owner（本机 loopback，全权）、
// 租户（带有效凭证）、被停用（凭证认识但 owner 关了他）、匿名（两者
// 都不是，只能访问公开路径）。
//
// 停用之所以单列而不是并进匿名：界面要说的是「你的访问已被关闭」，
// 不是「请用邀请链接」——后者会让人以为链接坏了，跑去要新链接。
type identity struct {
	owner   bool
	tenant  *model.Tenant
	revoked bool
}

func (i identity) authed() bool { return i.owner || (i.tenant != nil && !i.revoked) }

// scope 是该身份的数据与路径边界，传给 service 层执行隔离。
func (i identity) scope() service.Scope {
	switch {
	case i.owner:
		return service.OwnerScope()
	case i.tenant != nil && !i.revoked:
		return service.TenantScope(i.tenant.ID, i.tenant.Root)
	default:
		// 匿名与被停用的 root 都为空，任何路径操作都会被 guard 拒掉。
		return service.Scope{}
	}
}

type identityKey struct{}

func identityOf(r *http.Request) identity {
	id, _ := r.Context().Value(identityKey{}).(identity)
	return id
}

// scopeOf 是 handler 取隔离范围的唯一入口。
func scopeOf(r *http.Request) service.Scope { return identityOf(r).scope() }

// withIdentity 解析身份、拦截未认证与越权请求，并把身份放进 context。
func withIdentity(tenants *service.TenantService, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := resolveIdentity(r, tenants)

		if !id.authed() && !isPublicPath(r.URL.Path) {
			if id.revoked {
				writeError(w, fmt.Errorf("%w: access disabled by owner", service.ErrForbidden))
				return
			}
			writeError(w, fmt.Errorf("%w: invite required", service.ErrUnauthorized))
			return
		}
		if !id.owner && isOwnerOnly(r) {
			writeError(w, fmt.Errorf("%w: owner only", service.ErrForbidden))
			return
		}

		ctx := context.WithValue(r.Context(), identityKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func resolveIdentity(r *http.Request, tenants *service.TenantService) identity {
	cookie, err := r.Cookie(tenantCookie)
	hasCookie := err == nil && cookie.Value != ""

	// 本机访问即 owner：桌面壳与主机浏览器天然从回环地址进来，判定零配置、
	// 不会丢，也不需要维护一份 owner 凭证。
	//
	// 但**带了租户凭证就按租户算**，哪怕请求来自回环。两个原因：
	// 一是 owner 想在本机验一眼访客视角，点自己发出去的链接就行；
	// 二是任何反向代理（含开发态的 vite proxy）都会把来源改写成回环，
	// 若只看地址，代理后面的每个访客都会被提权成 owner。
	if isLoopback(r.RemoteAddr) && !hasCookie {
		return identity{owner: true}
	}
	if !hasCookie {
		return identity{}
	}
	tenant, err := tenants.Authenticate(r.Context(), cookie.Value)
	switch {
	case err == nil:
		return identity{tenant: tenant}
	case errors.Is(err, service.ErrForbidden) && tenant != nil:
		// 凭证认识但被 owner 关了：保留身份用于界面提示，权限为零。
		return identity{tenant: tenant, revoked: true}
	default:
		return identity{}
	}
}

// isPublicPath 列出不需要身份的路径。
// `/api/mcp/` 是 agent 子进程回连的编排端点（adr-006），自带一次性 token
// 鉴权且发不出 cookie——要求 cookie 会直接把编排打死。
func isPublicPath(path string) bool {
	return path == "/api/health" ||
		strings.HasPrefix(path, "/api/auth/") ||
		strings.HasPrefix(path, "/api/mcp/")
}

// isOwnerOnly 判定一个请求是否只有 owner 能发。
//
// 刻意用前缀 + 方法的集中表，而不是在每个 handler 里 if 一下：新增路由
// 自动继承同一策略，漏写不会变成越权。共享资源（技能库、角色、内置工具
// 配置）对租户只读——GET 放行，写一律 owner。
func isOwnerOnly(r *http.Request) bool {
	path := r.URL.Path
	switch {
	case strings.HasPrefix(path, "/api/orchestrator/"),
		strings.HasPrefix(path, "/api/tenants"),
		strings.HasPrefix(path, "/api/system"):
		return true
	case strings.HasPrefix(path, "/api/skills"),
		strings.HasPrefix(path, "/api/roles"),
		strings.HasPrefix(path, "/api/agents"):
		return r.Method != http.MethodGet
	}
	return false
}

func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// authHandler 处理邀请兑换与身份自查。
type authHandler struct {
	tenants *service.TenantService
}

// meResponse 是前端启动时的第一个问题：我是谁、能干什么。
type meResponse struct {
	Authenticated bool `json:"authenticated"`
	Owner         bool `json:"owner"`
	// Revoked 表示凭证认识但访问被 owner 关闭——界面显示「无权访问」，
	// 而不是让人再去要一次邀请链接。
	Revoked bool `json:"revoked,omitempty"`
	// TenantName / Root 只有租户身份才有；owner 不受目录限制。
	TenantName string `json:"tenantName,omitempty"`
	Root       string `json:"root,omitempty"`
}

func meOf(id identity) meResponse {
	res := meResponse{Authenticated: id.authed(), Owner: id.owner, Revoked: id.revoked}
	if id.tenant != nil {
		res.TenantName = id.tenant.Name
		if !id.revoked {
			res.Root = id.tenant.Root
		}
	}
	return res
}

// me 未认证时同样返回 200——前端据此渲染「需要邀请链接」页面，
// 401 会被通用错误处理当成故障弹提示。
func (h authHandler) me(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, meOf(identityOf(r)))
}

// redeem 把邀请链接里的 token 兑换成 cookie。前端拿到响应后立刻用
// history.replaceState 抹掉地址栏里的 token，避免用户把带凭证的 URL 转发出去。
func (h authHandler) redeem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}

	tenant, err := h.tenants.Authenticate(r.Context(), strings.TrimSpace(req.Token))
	if err != nil && !errors.Is(err, service.ErrForbidden) {
		writeError(w, fmt.Errorf("%w: invalid invite", service.ErrUnauthorized))
		return
	}

	// 被停用的凭证同样种 cookie：身份是认得的，只是权限为零。这样刷新
	// 后依然显示「无权访问」，而不是退回「请用邀请链接」——后者会让人
	// 以为链接坏了，跑去要一条新的。
	http.SetCookie(w, &http.Cookie{
		Name:     tenantCookie,
		Value:    tenant.Token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(cookieMaxAge.Seconds()),
	})
	if err != nil {
		writeError(w, fmt.Errorf("%w: access disabled by owner", service.ErrForbidden))
		return
	}
	writeData(w, http.StatusOK, meOf(identity{tenant: tenant}))
}

// logout 清掉凭证，用于换人使用同一台设备。
func (h authHandler) logout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     tenantCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	writeData(w, http.StatusOK, meResponse{})
}
