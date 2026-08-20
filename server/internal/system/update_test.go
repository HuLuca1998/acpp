package system

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
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
	release := `[{
		"tag_name": "v99.0.0",
		"name": "ACPP 99.0.0",
		"body": "- 支持一键更新\n- 修复若干问题",
		"html_url": "https://github.com/HuLuca1998/acpp/releases/tag/v99.0.0",
		"published_at": "2026-08-13T10:00:00Z",
		"assets": [
			{"name": "ACPP-99.0.0.zip", "browser_download_url": "https://example.com/ACPP-99.0.0.zip"}
		]
	}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/HuLuca1998/acpp/releases" {
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

// 契约：跨版本更新时，当前版本与最新版本之间的**每一版**日志都要带出来
// （按版本过滤而不是按列表顺序——补发的旧版本会插在中间），超过上限的
// 折成计数；已经装上的版本不进这份清单。
func TestUpdater_Info_CollectsPendingNotes(t *testing.T) {
	// 故意打乱顺序并混入 draft / prerelease 与一个比当前版本旧的。
	body := `[
		{"tag_name":"v99.0.3","body":"三","published_at":"2026-08-20T03:00:00Z"},
		{"tag_name":"v99.0.9","body":"草稿","draft":true},
		{"tag_name":"v99.0.1","body":"一","published_at":"2026-08-20T01:00:00Z"},
		{"tag_name":"v99.0.8","body":"预发布","prerelease":true},
		{"tag_name":"v99.0.2","body":"二","published_at":"2026-08-20T02:00:00Z"},
		{"tag_name":"v0.0.1","body":"太老","published_at":"2020-01-01T00:00:00Z"}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/HuLuca1998/acpp/releases" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	svc := NewUpdater("HuLuca1998/acpp")
	svc.apiBase = srv.URL
	info := svc.Info(context.Background(), true)

	if info.CheckError != "" {
		t.Fatalf("CheckError = %q", info.CheckError)
	}
	// 列表首条即最新（draft/prerelease 已滤掉）。
	if info.LatestVersion != "99.0.3" {
		t.Errorf("LatestVersion = %q，期望 99.0.3", info.LatestVersion)
	}
	var got []string
	for _, n := range info.Pending {
		got = append(got, n.Version)
	}
	// 三条待更新（0.0.1 比当前版本旧，不算），草稿与预发布不出现。
	want := []string{"99.0.3", "99.0.1", "99.0.2"}
	if len(got) != len(want) {
		t.Fatalf("Pending = %v，期望 %v", got, want)
	}
	for _, v := range want {
		if !slices.Contains(got, v) {
			t.Errorf("Pending 缺 %s：%v", v, got)
		}
	}
	if slices.Contains(got, "0.0.1") || slices.Contains(got, "99.0.9") {
		t.Errorf("Pending 混进了旧版本或草稿：%v", got)
	}
}
