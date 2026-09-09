package apilog

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"acpp/server/internal/model"
)

func newService(t *testing.T) *Service {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "log.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.APILog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewService(gdb)
}

// 契约：凭证头只留键名不留值，其余头原样；多值头并成一个字符串。
func TestHeadersJSON_RedactsCredentials(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer secret")
	h.Set("Cookie", "acpp=abc")
	h.Add("Accept", "text/html")
	h.Add("Accept", "application/json")
	got := HeadersJSON(h)
	if strings.Contains(got, "secret") || strings.Contains(got, "acpp=abc") {
		t.Fatalf("credentials leaked: %s", got)
	}
	for _, want := range []string{`"Authorization":"[redacted]"`, `"Cookie":"[redacted]"`, `"Accept":"text/html, application/json"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
}

// 契约：JSON / 文本 / 表单存正文，二进制与空类型不存。
func TestIsTextual(t *testing.T) {
	cases := map[string]bool{
		"application/json; charset=utf-8":   true,
		"text/plain":                        true,
		"application/problem+json":          true,
		"application/x-www-form-urlencoded": true,
		"image/png":                         false,
		"application/octet-stream":          false,
		"":                                  false,
	}
	for ct, want := range cases {
		if got := IsTextual(ct); got != want {
			t.Errorf("IsTextual(%q) = %v, want %v", ct, got, want)
		}
	}
}

// 契约：正文落库前按 BodyLimit 截断；列表不带正文与头，详情带；筛选按路径子串、
// 方法、状态码百位与身份，且叠加取交集。
func TestService_RecordListGet(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	long := strings.Repeat("x", BodyLimit+100)
	svc.Record(ctx, model.APILog{Method: "POST", Path: "/api/sessions", Status: 201, Identity: "owner", RequestBody: long, RequestHeaders: `{"A":"b"}`})
	svc.Record(ctx, model.APILog{Method: "GET", Path: "/api/skills", Status: 200, Identity: "alice"})
	svc.Record(ctx, model.APILog{Method: "GET", Path: "/api/sessions/9", Status: 404, Identity: "owner"})

	rows, total, err := svc.List(ctx, Filter{}, 1, 10, "")
	if err != nil || total != 3 || len(rows) != 3 {
		t.Fatalf("List all: total=%d len=%d err=%v", total, len(rows), err)
	}
	if rows[0].Path != "/api/sessions/9" {
		t.Fatalf("default order must be newest first, got %s", rows[0].Path)
	}
	for _, r := range rows {
		if r.RequestBody != "" || r.RequestHeaders != "" {
			t.Fatalf("list must omit body/headers, got %+v", r)
		}
	}

	_, total, _ = svc.List(ctx, Filter{Keyword: "session"}, 1, 10, "")
	if total != 2 {
		t.Fatalf("keyword: total=%d", total)
	}
	_, total, _ = svc.List(ctx, Filter{Keyword: "session", StatusClass: "4"}, 1, 10, "")
	if total != 1 {
		t.Fatalf("keyword+status: total=%d", total)
	}
	_, total, _ = svc.List(ctx, Filter{Method: "get", Identity: "alice"}, 1, 10, "")
	if total != 1 {
		t.Fatalf("method+identity: total=%d", total)
	}

	first := rows[len(rows)-1]
	rec, err := svc.Get(ctx, first.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(rec.RequestBody) > BodyLimit+32 || !strings.HasSuffix(rec.RequestBody, "…（已截断）") {
		t.Fatalf("body must be truncated with marker, len=%d", len(rec.RequestBody))
	}
	if rec.RequestHeaders != `{"A":"b"}` {
		t.Fatalf("Get must carry headers, got %q", rec.RequestHeaders)
	}

	if err := svc.Clear(ctx); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, total, _ = svc.List(ctx, Filter{}, 1, 10, ""); total != 0 {
		t.Fatalf("after Clear total=%d", total)
	}
}
