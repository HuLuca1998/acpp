package system

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"acpp/server/internal/service"
)

// 契约：版本比较按点分数字段逐段进行——0.10.0 大于 0.2.0（数字比较，
// 不是字符串），相等不算更新。
func TestVersionLess_NumericSegments(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.1.0", "0.1.1", true},
		{"0.2.0", "0.10.0", true},
		{"0.10.0", "0.2.0", false},
		{"1.0.0", "1.0.0", false},
		{"0.9", "0.9.1", true},
		{"1.0.0", "0.9.9", false},
	}
	for _, tc := range cases {
		if got := versionLess(tc.a, tc.b); got != tc.want {
			t.Errorf("versionLess(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// 契约：Info 从 GitHub Releases 解析出最新版本、描述与 zip asset，
// tag 的 v 前缀被剥掉，版本更新判定基于当前构建版本。
func TestUpdater_Info_ParsesLatestRelease(t *testing.T) {
	release := `{
		"tag_name": "v99.0.0",
		"name": "ACPP 99.0.0",
		"body": "- 支持一键更新\n- 修复若干问题",
		"html_url": "https://github.com/HuLuca1998/acpp/releases/tag/v99.0.0",
		"published_at": "2026-08-13T10:00:00Z",
		"assets": [
			{"name": "ACPP-99.0.0.zip", "browser_download_url": "https://example.com/ACPP-99.0.0.zip"}
		]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/HuLuca1998/acpp/releases/latest" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, release)
	}))
	defer srv.Close()

	svc := NewUpdater("HuLuca1998/acpp")
	svc.apiBase = srv.URL

	info := svc.Info(context.Background(), true)
	if info.CheckError != "" {
		t.Fatalf("CheckError = %q", info.CheckError)
	}
	if info.LatestVersion != "99.0.0" || !info.HasUpdate {
		t.Errorf("latest = %q hasUpdate = %v, want 99.0.0/true", info.LatestVersion, info.HasUpdate)
	}
	if info.AssetName != "ACPP-99.0.0.zip" || info.Notes == "" || info.PublishedAt == "" {
		t.Errorf("release 字段缺失: %+v", info)
	}
}

// 契约：仓库还没有 release（404）不是崩溃，而是缓存里一条可读的 CheckError。
func TestUpdater_Info_NoReleaseYet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	svc := NewUpdater("HuLuca1998/acpp")
	svc.apiBase = srv.URL

	info := svc.Info(context.Background(), true)
	if info.CheckError == "" || info.HasUpdate {
		t.Errorf("want 可读 CheckError 且无更新, got %+v", info)
	}
}

// 契约：开发态（进程不在 .app bundle 里）拒绝一键更新；无可用更新同样拒绝。
func TestUpdater_Apply_RejectsOutsideBundle(t *testing.T) {
	svc := NewUpdater("HuLuca1998/acpp")
	if _, err := svc.Apply(context.Background()); !errors.Is(err, service.ErrInvalid) {
		t.Errorf("无更新时 err = %v, want service.ErrInvalid", err)
	}

	// 伪造「有更新」缓存，仍应因不在 bundle 内被拒（测试进程是 go test 二进制）
	svc.mu.Lock()
	svc.cached = UpdateInfo{HasUpdate: true, LatestVersion: "99.0.0"}
	svc.assetURL = "https://example.com/ACPP.zip"
	svc.mu.Unlock()
	if _, err := svc.Apply(context.Background()); !errors.Is(err, service.ErrInvalid) {
		t.Errorf("非 bundle 进程 err = %v, want service.ErrInvalid", err)
	}
}
