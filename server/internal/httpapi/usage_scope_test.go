package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"acpp/server/internal/model"
)

// seedUsage 给某个身份造一行账目。租户 id 在这里是**唯一**的归属依据，
// 隔离成不成立全看查询有没有带上它。
func seedUsage(t *testing.T, env *flowEnv, tenantID uint, seq int, costMicro int64) {
	t.Helper()
	row := model.TokenUsage{
		SessionID:   uint(100 + tenantID),
		TurnSeq:     seq,
		TenantID:    tenantID,
		Flavor:      "claude",
		Project:     "acme/demo",
		Origin:      model.OriginUI,
		StartedAt:   time.Now().Add(-time.Hour),
		EndedAt:     time.Now().Add(-time.Hour).Add(time.Minute),
		TotalTokens: 1000,
		CostMicro:   costMicro,
		CostSource:  model.CostReported,
		StopReason:  "end_turn",
	}
	if err := env.db.Create(&row).Error; err != nil {
		t.Fatalf("seed usage: %v", err)
	}
}

// usageTotals 发一个请求并取回合计里的成本与轮数。
func usageTotals(t *testing.T, env *flowEnv, cookie *http.Cookie, path string) (turns int64, cost int64) {
	t.Helper()
	rec := env.as(t, cookie, http.MethodGet, path, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d，期望 200：%s", path, rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			Totals struct {
				Turns     int64 `json:"turns"`
				CostMicro int64 `json:"costMicro"`
			} `json:"totals"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return body.Data.Totals.Turns, body.Data.Totals.CostMicro
}

// 租户看得到的只有自己的账。这条在界面上测不出来（本机访问永远被判成
// owner），所以隔离的回归保护只能落在这里。
func TestUsageScopedToTenant(t *testing.T) {
	env := newFlowEnv(t)
	aliceID, bobID := tenantRowID(t, env, "alice"), tenantRowID(t, env, "bob")
	seedUsage(t, env, aliceID, 1, 1_000_000)
	seedUsage(t, env, bobID, 1, 7_000_000)
	seedUsage(t, env, 0, 1, 500_000) // owner 自己的

	const path = "/api/usage/summary?from=0"

	turns, cost := usageTotals(t, env, env.alice, path)
	if turns != 1 || cost != 1_000_000 {
		t.Errorf("alice 看到 %d 轮 / %d micro，期望只有自己的 1 轮 / 1000000", turns, cost)
	}

	turns, cost = usageTotals(t, env, env.bob, path)
	if turns != 1 || cost != 7_000_000 {
		t.Errorf("bob 看到 %d 轮 / %d micro，期望只有自己的", turns, cost)
	}

	turns, cost = usageTotals(t, env, nil, path)
	if turns != 3 || cost != 8_500_000 {
		t.Errorf("owner 看到 %d 轮 / %d micro，期望全部 3 轮 / 8500000", turns, cost)
	}
}

// 租户把别人的 tenant id 塞进查询参数也没用：范围是先于筛选加上去的。
func TestUsageTenantParamCannotEscapeScope(t *testing.T) {
	env := newFlowEnv(t)
	aliceID, bobID := tenantRowID(t, env, "alice"), tenantRowID(t, env, "bob")
	seedUsage(t, env, aliceID, 1, 1_000_000)
	seedUsage(t, env, bobID, 1, 7_000_000)

	for _, path := range []string{
		"/api/usage/summary?from=0&tenant=" + itoa(bobID),
		"/api/usage/breakdown?from=0&by=tenant&tenant=" + itoa(bobID),
	} {
		rec := env.as(t, env.alice, http.MethodGet, path, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", path, rec.Code)
		}
		if body := rec.Body.String(); strings.Contains(body, "7000000") {
			t.Errorf("alice 经 %s 看到了 bob 的钱：%s", path, body)
		}
	}
}

// 分组维度本身也要过范围：按身份分组时，租户只该看到自己那一组。
func TestUsageBreakdownByTenantStaysScoped(t *testing.T) {
	env := newFlowEnv(t)
	aliceID, bobID := tenantRowID(t, env, "alice"), tenantRowID(t, env, "bob")
	seedUsage(t, env, aliceID, 1, 1_000_000)
	seedUsage(t, env, bobID, 1, 7_000_000)

	rec := env.as(t, env.alice, http.MethodGet, "/api/usage/breakdown?from=0&by=tenant", "")
	var body struct {
		Data struct {
			Items []struct {
				Key string `json:"key"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data.Items) != 1 || body.Data.Items[0].Key != itoa(aliceID) {
		t.Errorf("alice 按身份分组拿到 %+v，期望只有自己那一组", body.Data.Items)
	}
}

// 重算历史是全库操作，租户够不着（isOwnerOnly 按方法判）。
func TestUsageBackfillIsOwnerOnly(t *testing.T) {
	env := newFlowEnv(t)

	if rec := env.as(t, env.alice, http.MethodPost, "/api/usage/backfill", ""); rec.Code != http.StatusForbidden {
		t.Errorf("租户 POST backfill = %d，期望 403", rec.Code)
	}
	if rec := env.as(t, nil, http.MethodPost, "/api/usage/backfill", ""); rec.Code != http.StatusOK {
		t.Errorf("owner POST backfill = %d，期望 200：%s", rec.Code, rec.Body.String())
	}
}

// 认不出的分组维度要当场拒绝——那一栏直接进 SQL 的 group by。
func TestUsageBreakdownRejectsUnknownDimension(t *testing.T) {
	env := newFlowEnv(t)
	rec := env.as(t, nil, http.MethodGet, "/api/usage/breakdown?by=cwd%3B+drop+table", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("认不出的维度 = %d，期望 400", rec.Code)
	}
}

func tenantRowID(t *testing.T, env *flowEnv, name string) uint {
	t.Helper()
	var tenant model.Tenant
	if err := env.db.Where("name = ?", name).First(&tenant).Error; err != nil {
		t.Fatalf("load tenant %s: %v", name, err)
	}
	return tenant.ID
}
