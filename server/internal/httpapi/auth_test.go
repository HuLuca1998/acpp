package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"acpp/server/internal/config"
	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

// authRouter 建一个只装了租户服务的路由：身份中间件的判定与其他业务
// 服务无关，缺席的服务只会在真的走到 handler 时报错，不影响这些断言。
func authRouter(t *testing.T) (http.Handler, *service.TenantService) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "auth.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.Tenant{}, &model.Session{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	tenants := service.NewTenantService(gdb, t.TempDir())
	return NewRouter(config.Config{WebDir: webDir(t)}, Services{Tenants: tenants}), tenants
}

func do(handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// 契约：本机访问即 owner——桌面壳与主机浏览器从回环地址进来，不需要
// 任何凭证就能用 owner 专属端点。
func TestAuth_LoopbackIsOwner(t *testing.T) {
	handler, _ := authRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/tenants", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	if got := do(handler, req).Code; got != http.StatusOK {
		t.Fatalf("owner GET /api/tenants = %d, want 200", got)
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meReq.RemoteAddr = "[::1]:54321"
	me := decodeMe(t, do(handler, meReq))
	if !me.Owner || !me.Authenticated {
		t.Fatalf("me = %+v, want owner", me)
	}
}

// 契约：局域网访客没有凭证时 API 一律 401，但**前端页面照常可加载**——
// 否则邀请链接 `/?invite=xxx` 会先撞 401 白屏，兑换都发不出去。
func TestAuth_AnonymousBlockedButAppLoads(t *testing.T) {
	handler, _ := authRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	if got := do(handler, req).Code; got != http.StatusUnauthorized {
		t.Fatalf("anonymous GET /api/sessions = %d, want 401", got)
	}

	page := do(handler, httptest.NewRequest(http.MethodGet, "/", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "ACPP") {
		t.Fatalf("index = %d %q, want 200 with app html", page.Code, page.Body.String())
	}

	health := do(handler, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("anonymous health = %d, want 200", health.Code)
	}

	me := do(handler, httptest.NewRequest(http.MethodGet, "/api/auth/me", nil))
	if me.Code != http.StatusOK {
		t.Fatalf("anonymous me = %d, want 200 (前端据此渲染邀请页)", me.Code)
	}
}

// 契约：邀请 token 兑换成 cookie 后，后续请求凭 cookie 就是租户身份。
// cookie 是唯一能让 REST / SSE / WebSocket 三条通道统一鉴权的载体。
func TestAuth_RedeemGrantsTenantIdentity(t *testing.T) {
	handler, tenants := authRouter(t)

	created, err := tenants.Create(t.Context(), service.TenantInput{Name: "alice"})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	redeem := httptest.NewRequest(http.MethodPost, "/api/auth/redeem",
		strings.NewReader(`{"token":"`+created.InviteToken+`"}`))
	rec := do(handler, redeem)
	if rec.Code != http.StatusOK {
		t.Fatalf("redeem = %d, want 200", rec.Code)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != tenantCookie || !cookies[0].HttpOnly {
		t.Fatalf("cookies = %+v, want HttpOnly %s", cookies, tenantCookie)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.AddCookie(cookies[0])
	me := decodeMe(t, do(handler, req))
	if me.Owner || !me.Authenticated || me.TenantName != "alice" || me.Root != created.Root {
		t.Fatalf("me = %+v, want tenant alice at %s", me, created.Root)
	}

	bad := httptest.NewRequest(http.MethodPost, "/api/auth/redeem", strings.NewReader(`{"token":"nope"}`))
	if got := do(handler, bad).Code; got != http.StatusUnauthorized {
		t.Fatalf("redeem(bad) = %d, want 401", got)
	}
}

// 契约：被 owner 停用后，凭证仍被认出来但权限为零——API 返回 403 且
// /api/auth/me 带 revoked，界面据此显示「无权访问」而不是「请用邀请链接」。
func TestAuth_DisabledTenantIsRevokedNotAnonymous(t *testing.T) {
	handler, tenants := authRouter(t)
	created, err := tenants.Create(t.Context(), service.TenantInput{Name: "carol"})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	disabled := true
	if _, err := tenants.Update(t.Context(), created.ID, service.TenantPatch{Disabled: &disabled}); err != nil {
		t.Fatalf("disable tenant: %v", err)
	}
	cookie := &http.Cookie{Name: tenantCookie, Value: created.InviteToken}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.AddCookie(cookie)
	if got := do(handler, req).Code; got != http.StatusForbidden {
		t.Fatalf("disabled tenant GET /api/sessions = %d, want 403", got)
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meReq.AddCookie(cookie)
	me := decodeMe(t, do(handler, meReq))
	if me.Authenticated || !me.Revoked || me.TenantName != "carol" {
		t.Fatalf("me = %+v, want revoked identity for carol", me)
	}
	if me.Root != "" {
		t.Fatalf("me.Root = %q, want empty for revoked tenant", me.Root)
	}

	// 兑换被停用的链接同样是 403（而不是 401「链接无效」）。
	redeem := httptest.NewRequest(http.MethodPost, "/api/auth/redeem",
		strings.NewReader(`{"token":"`+created.InviteToken+`"}`))
	if got := do(handler, redeem).Code; got != http.StatusForbidden {
		t.Fatalf("redeem(disabled) = %d, want 403", got)
	}
}

// 契约：owner 专属面对租户是 403，共享资源对租户只读（GET 放行、写拒绝）。
// 判定集中在 isOwnerOnly 的前缀表里，新增路由自动继承同一策略。
func TestAuth_OwnerOnlySurfaces(t *testing.T) {
	handler, tenants := authRouter(t)
	created, err := tenants.Create(t.Context(), service.TenantInput{Name: "bob"})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	cookie := &http.Cookie{Name: tenantCookie, Value: created.InviteToken}

	forbidden := []struct{ method, path string }{
		{http.MethodGet, "/api/tenants"},
		{http.MethodPost, "/api/tenants"},
		{http.MethodGet, "/api/system"},
		{http.MethodPost, "/api/system/env/install"},
		{http.MethodPost, "/api/skills"},
		{http.MethodPut, "/api/skills/demo"},
		{http.MethodPut, "/api/agents/1/catalog"},
		{http.MethodPost, "/api/skills/demo/scripts/run"},
	}
	for _, tc := range forbidden {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
		req.AddCookie(cookie)
		if got := do(handler, req).Code; got != http.StatusForbidden {
			t.Fatalf("tenant %s %s = %d, want 403", tc.method, tc.path, got)
		}
	}

	// 共享资源的读不该被身份层拦下（这里没装对应服务，走到 handler 会
	// 500——只要不是 401/403 就说明身份层放行了）。
	for _, path := range []string{"/api/skills", "/api/agents"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		if got := do(handler, req).Code; got == http.StatusForbidden || got == http.StatusUnauthorized {
			t.Fatalf("tenant GET %s = %d, want pass-through", path, got)
		}
	}
}

// decodeMe 拆统一响应外壳（所有接口都是 {"data": ...}）取身份视图。
func decodeMe(t *testing.T, rec *httptest.ResponseRecorder) meResponse {
	t.Helper()
	var body struct {
		Data meResponse `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	return body.Data
}
