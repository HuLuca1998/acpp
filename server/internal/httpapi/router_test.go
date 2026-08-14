package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"acpp/server/internal/config"
)

func webDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<title>ACPP</title>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "index-BlCfH.js"), []byte("//js"), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}
	return dir
}

// 契约：静态托管的缓存策略——入口 html（含 SPA 回落路径）必须 no-cache
// 每次协商，否则桌面版更新后 WKWebView 端出旧界面；带内容哈希的
// /assets/ 则永久缓存。
func TestRouter_StaticCacheHeaders(t *testing.T) {
	handler := NewRouter(config.Config{WebDir: webDir(t)}, Services{})

	cases := []struct {
		path      string
		wantCache string
		wantBody  string
	}{
		{"/", "no-cache", "<title>ACPP</title>"},
		{"/sessions/12", "no-cache", "<title>ACPP</title>"}, // SPA 回落
		{"/assets/index-BlCfH.js", "public, max-age=31536000, immutable", "//js"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", tc.path, rec.Code)
			continue
		}
		if got := rec.Header().Get("Cache-Control"); got != tc.wantCache {
			t.Errorf("%s: Cache-Control = %q, want %q", tc.path, got, tc.wantCache)
		}
		if !strings.Contains(rec.Body.String(), tc.wantBody) {
			t.Errorf("%s: body = %q, want contains %q", tc.path, rec.Body.String(), tc.wantBody)
		}
	}
}
