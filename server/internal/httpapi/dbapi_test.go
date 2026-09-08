package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"acpp/server/internal/model"
)

// 契约：/api/db 是租户与脚本的只读数据库面（adr-021）——租户 token 经
// Authorization: Bearer 从局域网地址就能列数据源、按标识发查询；写语句
// 被拒；管理面照旧对租户关门。

// asBearer 模拟局域网里的脚本：非回环来源，只带 Bearer 头。
func (e *flowEnv) asBearer(t *testing.T, token, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "192.168.2.50:6000"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func seedDatasource(t *testing.T, env *flowEnv, body string) model.DataSource {
	t.Helper()
	rec := env.as(t, nil, http.MethodPost, "/api/datasources", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("owner create datasource = %d: %s", rec.Code, rec.Body.String())
	}
	return decodeData[model.DataSource](t, rec)
}

func TestDBAPI_TenantListsEnabledSources(t *testing.T) {
	env := newFlowEnv(t)
	seedDatasource(t, env, `{"project":"BDBGAME2024/pp-game","env":"dev","host":"127.0.0.1","port":3306,"user":"ro","password":"secret-pw","database":"pp_game"}`)
	seedDatasource(t, env, `{"project":"BDBGAME2024/pp-game","env":"prod","host":"127.0.0.1","port":3306,"user":"ro","password":"secret-pw","database":"pp_game"}`)
	seedDatasource(t, env, `{"project":"onepay","env":"prod","host":"127.0.0.1","port":3306,"user":"ro","password":"secret-pw","database":"onepay"}`)
	off := seedDatasource(t, env, `{"project":"onepay","env":"old","host":"127.0.0.1","port":3306,"user":"ro","password":"secret-pw","database":"onepay","disabled":true}`)
	if !off.Disabled {
		t.Fatalf("seed disabled datasource: %+v", off)
	}

	// 匿名（局域网、无凭证）进不来。
	if rec := env.asBearer(t, "", http.MethodGet, "/api/db/sources", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET /api/db/sources = %d, want 401", rec.Code)
	}

	rec := env.asBearer(t, env.alice.Value, http.MethodGet, "/api/db/sources", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("alice GET /api/db/sources = %d: %s", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()
	if strings.Contains(raw, "secret-pw") {
		t.Fatal("/api/db/sources leaks the password")
	}
	got := decodeData[[]model.DataSource](t, rec)
	refs := make([]string, 0, len(got))
	for _, s := range got {
		refs = append(refs, s.Ref)
	}
	want := "BDBGAME2024/pp-game/dev,BDBGAME2024/pp-game/prod,onepay/prod"
	if strings.Join(refs, ",") != want {
		t.Fatalf("refs = %v, want %s（停用的不出现，按项目、环境排序）", refs, want)
	}

	rec = env.asBearer(t, env.alice.Value, http.MethodGet, "/api/db/sources?project=ONEPAY", "")
	got = decodeData[[]model.DataSource](t, rec)
	if len(got) != 1 || got[0].Ref != "onepay/prod" {
		t.Fatalf("project filter = %+v, want only onepay/prod（不区分大小写）", got)
	}

	// 管理面照旧对租户关门——哪怕凭证是 Bearer 给的。
	if rec := env.asBearer(t, env.alice.Value, http.MethodGet, "/api/datasources", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("alice GET /api/datasources = %d, want 403", rec.Code)
	}
}

func TestDBAPI_QueryIsReadOnlyAndAddressable(t *testing.T) {
	env := newFlowEnv(t)
	ro := seedDatasource(t, env, `{"project":"BDBGAME2024/pp-game","env":"dev","host":"127.0.0.1","port":3306,"user":"ro","password":"pw","database":"pp_game"}`)
	rw := seedDatasource(t, env, `{"project":"BDBGAME2024/pp-game","env":"prod","host":"127.0.0.1","port":3306,"user":"rw","password":"pw","database":"pp_game"}`)
	// 新连接一律只读（Create 的约定），要可写得再改一次。
	rec := env.as(t, nil, http.MethodPut, "/api/datasources/"+itoa(rw.ID),
		`{"project":"BDBGAME2024/pp-game","env":"prod","host":"127.0.0.1","port":3306,"user":"rw","database":"pp_game","readOnly":false}`)
	if rec.Code != http.StatusOK || decodeData[model.DataSource](t, rec).ReadOnly {
		t.Fatalf("make prod writable = %d: %s", rec.Code, rec.Body.String())
	}

	cases := []struct {
		name string
		body string
		want int
		msg  string
	}{
		// 写语句在连库之前就被拦：只读源 403，可写源也只是「查询通道」400。
		{"write on read-only source", `{"source":"BDBGAME2024/pp-game/dev","sql":"UPDATE t SET a=1"}`, http.StatusForbidden, "配置为只读"},
		{"write on writable source", `{"source":"prod","sql":"DELETE FROM t"}`, http.StatusBadRequest, "查询通道"},
		{"unknown source", `{"source":"nope","sql":"SELECT 1"}`, http.StatusNotFound, "没有叫"},
		{"unknown id", `{"source":"999","sql":"SELECT 1"}`, http.StatusNotFound, "启用数据源"},
		{"ambiguous when omitted", `{"sql":"SELECT 1"}`, http.StatusBadRequest, "有多个数据源"},
		{"empty sql", `{"source":"dev","sql":"  "}`, http.StatusBadRequest, "没有可执行的语句"},
		{"unknown field", `{"source":"dev","sql":"SELECT 1","database":"x"}`, http.StatusBadRequest, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.asBearer(t, env.bob.Value, http.MethodPost, "/api/db/query", tc.body)
			if rec.Code != tc.want {
				t.Fatalf("code = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
			if tc.msg != "" && !strings.Contains(rec.Body.String(), tc.msg) {
				t.Fatalf("body = %s, want it to mention %q", rec.Body.String(), tc.msg)
			}
		})
	}

	// 用 id 寻址走到同一条护栏，证明 id 与 ref 落在同一条数据源上。
	rec = env.asBearer(t, env.bob.Value, http.MethodPost, "/api/db/query",
		`{"source":"`+itoa(ro.ID)+`","sql":"INSERT INTO t VALUES (1)"}`)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), ro.Ref) {
		t.Fatalf("query by id = %d: %s, want 403 naming %s", rec.Code, rec.Body.String(), ro.Ref)
	}
}
