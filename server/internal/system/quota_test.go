package system

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"acpp/server/internal/config"

	"acpp/server/internal/service"
)

// quotaSvc 造一个两条取数路径都指向本地替身的 Service：claude 的 CLI 用
// 注入的载荷代替，codex 的远端接口用 httptest。**测试不许真拉 claude 进程
// 或打 chatgpt.com**——那要本机登录态，结果看天吃饭。
func quotaSvc(t *testing.T, claude func(ctx context.Context) ([]byte, error), codex http.Handler) *Service {
	t.Helper()
	svc := NewService(nil, config.Config{DataDir: t.TempDir()})
	svc.quota.claudeRun = claude
	if codex != nil {
		ts := httptest.NewServer(codex)
		t.Cleanup(ts.Close)
		svc.quota.codexAPI = ts.URL
	}
	return svc
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return raw
}

func staticClaude(raw []byte) func(ctx context.Context) ([]byte, error) {
	return func(context.Context) ([]byte, error) { return raw, nil }
}

// fakeJWT 造一个只有 exp 声明的 JWT：取数前的本地过期判断只看这一个字段。
func fakeJWT(exp time.Time) string {
	payload, _ := json.Marshal(map[string]int64{"exp": exp.Unix()})
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"RS256"}`)) + "." + enc(payload) + ".sig"
}

// writeCodexHome 在 Service 的 codex-home 里放一份 auth.json（可选 config.toml），
// 与 acpp 隔离后的真实布局一致。
func writeCodexHome(t *testing.T, svc *Service, auth string, configToml string) {
	t.Helper()
	home := svc.codexHomeDir()
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir codex home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(auth), 0o600); err != nil {
		t.Fatalf("write auth.json: %v", err)
	}
	if configToml != "" {
		if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(configToml), 0o600); err != nil {
			t.Fatalf("write config.toml: %v", err)
		}
	}
}

func chatgptAuth(token string) string {
	return fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"access_token":%q,"account_id":"3ede3973-b0fb-4a22-a6ab-ff87adc31e19"}}`, token)
}

// 契约：claude 的水位以服务端 limits[] 为准——会话窗 / 周窗 / 按模型的周窗
// 各成一行，带百分比、重置时刻与「当前卡着的是哪个」；套餐名透传。
func TestService_PlanQuota_ClaudeMapsLimitsToWindows(t *testing.T) {
	svc := quotaSvc(t, staticClaude(fixture(t, "claude-usage.json")), nil)

	q, err := svc.PlanQuota(t.Context(), "claude", false)
	if err != nil {
		t.Fatalf("PlanQuota: %v", err)
	}
	t.Logf("quota: %+v", q)
	if q.Flavor != "claude" || q.Status != "ok" || q.Plan != "max" {
		t.Fatalf("flavor/status/plan = %s/%s/%s, want claude/ok/max", q.Flavor, q.Status, q.Plan)
	}
	want := []QuotaWindow{
		{Kind: "session", Percent: 5},
		{Kind: "weekly", Percent: 85},
		{Kind: "weekly_model", Model: "Fable", Percent: 91, Active: true},
	}
	if len(q.Windows) != len(want) {
		t.Fatalf("windows = %d, want %d: %+v", len(q.Windows), len(want), q.Windows)
	}
	for i, w := range want {
		got := q.Windows[i]
		if got.Kind != w.Kind || got.Model != w.Model || got.Percent != w.Percent || got.Active != w.Active {
			t.Errorf("window[%d] = %+v, want %+v", i, got, w)
		}
		if got.ResetsAt == nil {
			t.Errorf("window[%d] has no resetsAt", i)
		}
	}
	// 5 小时窗的重置时刻是夹具里的 07:30Z——带微秒与 +00:00 偏移的 ISO 时间要解得出来。
	if got := q.Windows[0].ResetsAt.UTC(); got.Hour() != 7 || got.Minute() != 30 || got.Day() != 22 {
		t.Errorf("session resetsAt = %s, want 2026-09-22T07:30Z", got)
	}
	// 额外用量没开就不该出现——出现了界面会画一条 0% 的空行。
	if q.Extra != nil {
		t.Errorf("extra = %+v, want nil when extra usage is disabled", q.Extra)
	}
	if q.FetchedAt.IsZero() {
		t.Error("fetchedAt not set")
	}
}

// 契约：老版本 CLI 没有 limits[] 时退到具名窗口字段，型号窗仍能列出来。
func TestService_PlanQuota_ClaudeFallsBackToNamedWindows(t *testing.T) {
	payload := []byte(`{
	  "subscription_type": "pro",
	  "rate_limits_available": true,
	  "rate_limits": {
	    "five_hour": {"utilization": 12, "resets_at": "2026-09-22T07:30:00+00:00"},
	    "seven_day": {"utilization": 40, "resets_at": "2026-09-23T14:00:00+00:00"},
	    "seven_day_opus": null,
	    "seven_day_sonnet": {"utilization": 3, "resets_at": null},
	    "model_scoped": [{"display_name": "Fable", "utilization": 66, "resets_at": "2026-09-23T14:00:00+00:00"}],
	    "extra_usage": {"is_enabled": true, "monthly_limit": 50, "used_credits": 12.5, "utilization": 25, "currency": "USD"}
	  }
	}`)
	svc := quotaSvc(t, staticClaude(payload), nil)

	q, err := svc.PlanQuota(t.Context(), "claude", false)
	if err != nil {
		t.Fatalf("PlanQuota: %v", err)
	}
	t.Logf("quota: %+v", q)
	kinds := make([]string, 0, len(q.Windows))
	for _, w := range q.Windows {
		kinds = append(kinds, w.Kind+":"+w.Model)
	}
	want := []string{"session:", "weekly:", "weekly_model:Sonnet", "weekly_model:Fable"}
	if fmt.Sprint(kinds) != fmt.Sprint(want) {
		t.Fatalf("windows = %v, want %v", kinds, want)
	}
	if q.Windows[2].ResetsAt != nil {
		t.Errorf("sonnet resetsAt = %v, want nil when server gives null", q.Windows[2].ResetsAt)
	}
	if q.Extra == nil || q.Extra.Percent != 25 || q.Extra.Used != 12.5 || q.Extra.Limit != 50 || q.Extra.Currency != "USD" {
		t.Errorf("extra = %+v, want enabled extra usage carried over", q.Extra)
	}
}

// 契约：API key / 第三方接入没有套餐限额，报 unavailable 而不是空数据或报错。
func TestService_PlanQuota_ClaudeWithoutPlanLimitsIsUnavailable(t *testing.T) {
	svc := quotaSvc(t, staticClaude([]byte(`{"subscription_type":null,"rate_limits_available":false,"rate_limits":null}`)), nil)

	q, err := svc.PlanQuota(t.Context(), "claude", false)
	if err != nil {
		t.Fatalf("PlanQuota: %v", err)
	}
	if q.Status != "unavailable" || len(q.Windows) != 0 || q.Windows == nil {
		t.Fatalf("quota = %+v, want status unavailable with empty (non-nil) windows", q)
	}
}

// 契约：拉 CLI 失败不是 500——没装与没登录各归各类，让界面给引导；其余算 error 并带原因。
func TestService_PlanQuota_ClaudeRunFailureIsClassified(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status string
	}{
		{"cli missing", errClaudeMissing, "unavailable"},
		{"not logged in", errors.New("claude: Not logged in · Please run /login"), "not_logged_in"},
		{"other", errors.New("claude 没有返回用量数据：exit status 1"), "error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := quotaSvc(t, func(context.Context) ([]byte, error) { return nil, tc.err }, nil)
			q, err := svc.PlanQuota(t.Context(), "claude", false)
			if err != nil {
				t.Fatalf("PlanQuota: %v", err)
			}
			if q.Status != tc.status || q.Error != tc.err.Error() {
				t.Fatalf("quota = %+v, want status %s with the cause", q, tc.status)
			}
		})
	}
}

// 契约：codex 用 auth.json 里的登录态去问账号的用量接口（带 bearer 与账号头），
// 窗口按长度归类、重置时刻从 unix 秒还原，额度余额与第三方 provider 一并带出。
func TestService_PlanQuota_CodexReadsAccountUsage(t *testing.T) {
	var gotAuth, gotAccount atomic.Value
	svc := quotaSvc(t, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		gotAccount.Store(r.Header.Get("ChatGPT-Account-Id"))
		_, _ = w.Write(fixture(t, "codex-usage.json"))
	}))
	token := fakeJWT(time.Now().Add(72 * time.Hour))
	writeCodexHome(t, svc, chatgptAuth(token), "model = \"glm-5.3-flash\"\nmodel_provider = \"cliproxyapi\"\n\n[model_providers.cliproxyapi]\nname = \"cliproxyapi\"\n")

	q, err := svc.PlanQuota(t.Context(), "codex", false)
	if err != nil {
		t.Fatalf("PlanQuota: %v", err)
	}
	t.Logf("quota: %+v", q)
	if q.Flavor != "codex" || q.Status != "ok" || q.Plan != "prolite" {
		t.Fatalf("flavor/status/plan = %s/%s/%s, want codex/ok/prolite", q.Flavor, q.Status, q.Plan)
	}
	if gotAuth.Load() != "Bearer "+token || gotAccount.Load() != "3ede3973-b0fb-4a22-a6ab-ff87adc31e19" {
		t.Errorf("request headers = %v / %v, want the auth.json token and account id", gotAuth.Load(), gotAccount.Load())
	}
	if len(q.Windows) != 1 {
		t.Fatalf("windows = %+v, want exactly the primary window (secondary is null)", q.Windows)
	}
	w := q.Windows[0]
	if w.Kind != "weekly" || w.WindowSeconds != 604800 || w.Percent != 100 {
		t.Errorf("window = %+v, want weekly/604800s/100%%", w)
	}
	if w.ResetsAt == nil || w.ResetsAt.Unix() != 1790331976 {
		t.Errorf("resetsAt = %v, want unix 1790331976", w.ResetsAt)
	}
	if q.Credits == nil || q.Credits.Balance != "1000" || q.Credits.Unlimited {
		t.Errorf("credits = %+v, want balance 1000", q.Credits)
	}
	if q.Provider != "cliproxyapi" {
		t.Errorf("provider = %q, want cliproxyapi from config.toml", q.Provider)
	}
}

// 契约：官方 provider（或没配）时不标 provider——那时消耗的正是这份额度。
func TestService_PlanQuota_CodexOfficialProviderIsNotFlagged(t *testing.T) {
	svc := quotaSvc(t, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fixture(t, "codex-usage.json"))
	}))
	writeCodexHome(t, svc, chatgptAuth(fakeJWT(time.Now().Add(time.Hour))), "model_provider = \"openai\"\n[projects.\"/tmp\"]\ntrust_level = \"trusted\"\n")

	q, err := svc.PlanQuota(t.Context(), "codex", false)
	if err != nil {
		t.Fatalf("PlanQuota: %v", err)
	}
	if q.Provider != "" {
		t.Fatalf("provider = %q, want empty for openai", q.Provider)
	}
}

// 契约：本地令牌已过期就不去打接口（注定 401），直接报 expired 让用户跑一次 codex。
func TestService_PlanQuota_CodexExpiredTokenSkipsRequest(t *testing.T) {
	var hits atomic.Int32
	svc := quotaSvc(t, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	writeCodexHome(t, svc, chatgptAuth(fakeJWT(time.Now().Add(-time.Minute))), "")

	q, err := svc.PlanQuota(t.Context(), "codex", false)
	if err != nil {
		t.Fatalf("PlanQuota: %v", err)
	}
	if q.Status != "expired" || hits.Load() != 0 {
		t.Fatalf("status = %s, hits = %d; want expired without a request", q.Status, hits.Load())
	}
}

// 契约：令牌没到期却被服务端拒了（撤销、换设备）也按 expired 处理——用户要做的事一样。
func TestService_PlanQuota_CodexRejectedTokenIsExpired(t *testing.T) {
	svc := quotaSvc(t, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	writeCodexHome(t, svc, chatgptAuth(fakeJWT(time.Now().Add(time.Hour))), "")

	q, err := svc.PlanQuota(t.Context(), "codex", false)
	if err != nil {
		t.Fatalf("PlanQuota: %v", err)
	}
	if q.Status != "expired" {
		t.Fatalf("status = %s, want expired on 401", q.Status)
	}
}

// 契约：没有 auth.json 是没登录；API key 登录是没有套餐额度。两者都不算错误。
func TestService_PlanQuota_CodexLoginStateIsReported(t *testing.T) {
	t.Run("no auth file", func(t *testing.T) {
		svc := quotaSvc(t, nil, nil)
		// codex-home 还没搭起来时退到系统 home——这里把它指到空目录。
		t.Setenv("CODEX_HOME", t.TempDir())
		q, err := svc.PlanQuota(t.Context(), "codex", false)
		if err != nil {
			t.Fatalf("PlanQuota: %v", err)
		}
		if q.Status != "not_logged_in" {
			t.Fatalf("status = %s, want not_logged_in", q.Status)
		}
	})
	t.Run("api key", func(t *testing.T) {
		svc := quotaSvc(t, nil, nil)
		writeCodexHome(t, svc, `{"auth_mode":"apikey","OPENAI_API_KEY":"sk-test"}`, "")
		q, err := svc.PlanQuota(t.Context(), "codex", false)
		if err != nil {
			t.Fatalf("PlanQuota: %v", err)
		}
		if q.Status != "unavailable" {
			t.Fatalf("status = %s, want unavailable", q.Status)
		}
	})
}

// 契约：同一方言一分钟内复用缓存、refresh 绕过；两家各自缓存互不影响。
func TestService_PlanQuota_CachesPerFlavorAndRefreshBypasses(t *testing.T) {
	var claudeRuns, codexHits atomic.Int32
	svc := quotaSvc(t, func(context.Context) ([]byte, error) {
		claudeRuns.Add(1)
		return fixture(t, "claude-usage.json"), nil
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		codexHits.Add(1)
		_, _ = w.Write(fixture(t, "codex-usage.json"))
	}))
	writeCodexHome(t, svc, chatgptAuth(fakeJWT(time.Now().Add(time.Hour))), "")

	for range 3 {
		if _, err := svc.PlanQuota(t.Context(), "claude", false); err != nil {
			t.Fatalf("PlanQuota claude: %v", err)
		}
	}
	if claudeRuns.Load() != 1 {
		t.Fatalf("claude runs = %d after 3 calls, want 1 (cached)", claudeRuns.Load())
	}
	if _, err := svc.PlanQuota(t.Context(), "claude", true); err != nil {
		t.Fatalf("PlanQuota claude refresh: %v", err)
	}
	if claudeRuns.Load() != 2 {
		t.Fatalf("claude runs = %d after refresh, want 2", claudeRuns.Load())
	}
	if _, err := svc.PlanQuota(t.Context(), "codex", false); err != nil {
		t.Fatalf("PlanQuota codex: %v", err)
	}
	if codexHits.Load() != 1 || claudeRuns.Load() != 2 {
		t.Fatalf("codex hits = %d, claude runs = %d; want codex fetched once without touching claude", codexHits.Load(), claudeRuns.Load())
	}
}

// 契约：只认 claude / codex，别的方言是入参错误（400），不是 500。
func TestService_PlanQuota_RejectsUnknownFlavor(t *testing.T) {
	svc := quotaSvc(t, nil, nil)
	for _, flavor := range []string{"", "generic", "Claude"} {
		if _, err := svc.PlanQuota(t.Context(), flavor, false); !errors.Is(err, service.ErrInvalid) {
			t.Errorf("flavor %q: err = %v, want ErrInvalid", flavor, err)
		}
	}
}
