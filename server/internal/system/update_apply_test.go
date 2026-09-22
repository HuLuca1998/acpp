package system

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeAppZip 造一个 release asset：里面是一个形状合格的 .app（壳 + acp-server）。
func fakeAppZip(t *testing.T, name string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		name + ".app/Contents/Info.plist":               "<plist/>",
		name + ".app/Contents/MacOS/" + name:            "shell",
		name + ".app/Contents/MacOS/acp-server":         "new-server",
		name + ".app/Contents/Resources/web/index.html": "<html/>",
	}
	for path, content := range files {
		w, err := zw.Create(path)
		if err != nil {
			t.Fatalf("zip create %s: %v", path, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", path, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// fakeUpdater 造一个「有更新可装、bundle 在临时目录里」的 Updater。
func fakeUpdater(assetURL, bundle string) *Updater {
	u := NewUpdater("HuLuca1998/acpp")
	u.bundlePath = func() (string, error) { return bundle, nil }
	u.mu.Lock()
	u.cached = UpdateInfo{HasUpdate: true, LatestVersion: "99.0.0"}
	u.assetURL = assetURL
	u.mu.Unlock()
	return u
}

// waitSettled 轮询到更新走到终态（done / restarting / failed）。
func waitSettled(t *testing.T, u *Updater) UpdateProgress {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		p := u.Progress()
		switch p.Phase {
		case "done", "restarting", "failed":
			return p
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("update never settled: %+v", u.Progress())
	return UpdateProgress{}
}

// 契约：Apply 立刻返回「下载中」，流水线在后台跑完：下载字节数与 asset
// 大小一致、新包替换掉旧包、备份清掉；找不到壳进程时终态是 done 并说明
// 要手动重开（而不是给无关进程发信号）。解包与安装靠 ditto，只在 macOS 跑。
func TestUpdater_Apply_InstallsInBackgroundAndReportsProgress(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("安装流程依赖 /usr/bin/ditto")
	}
	asset := fakeAppZip(t, "Fake")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "ACPP-99.0.0.zip", time.Time{}, bytes.NewReader(asset))
	}))
	defer srv.Close()

	bundle := filepath.Join(t.TempDir(), "Fake.app")
	oldServer := filepath.Join(bundle, "Contents", "MacOS", "acp-server")
	if err := os.MkdirAll(filepath.Dir(oldServer), 0o755); err != nil {
		t.Fatalf("mkdir old bundle: %v", err)
	}
	if err := os.WriteFile(oldServer, []byte("old-server"), 0o755); err != nil {
		t.Fatalf("write old server: %v", err)
	}
	u := fakeUpdater(srv.URL+"/asset.zip", bundle)

	first, err := u.Apply(context.Background())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if first.Phase != "downloading" || first.Version != "99.0.0" || first.StartedAt.IsZero() {
		t.Fatalf("initial progress = %+v, want downloading v99.0.0 with startedAt", first)
	}

	final := waitSettled(t, u)
	t.Logf("final: %+v", final)
	if final.Phase != "done" || !strings.Contains(final.Message, "手动") {
		t.Fatalf("final = %+v, want done with a manual-restart message (no shell process)", final)
	}
	if final.Total != int64(len(asset)) || final.Downloaded != final.Total {
		t.Errorf("downloaded/total = %d/%d, want both %d", final.Downloaded, final.Total, len(asset))
	}
	if final.Error != "" {
		t.Errorf("error = %q, want empty", final.Error)
	}
	installed, err := os.ReadFile(oldServer)
	if err != nil || string(installed) != "new-server" {
		t.Errorf("installed acp-server = %q (%v), want new-server", installed, err)
	}
	if _, err := os.Stat(bundle + ".old"); !os.IsNotExist(err) {
		t.Errorf("backup %s.old still exists (err=%v), want removed", bundle, err)
	}
}

// 契约：更新进行中再调 Apply 不会开第二份下载——返回同一份进度，asset 只被请求一次。
func TestUpdater_Apply_IsIdempotentWhileRunning(t *testing.T) {
	var hits atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-release
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	u := fakeUpdater(srv.URL+"/asset.zip", filepath.Join(t.TempDir(), "Fake.app"))

	if _, err := u.Apply(context.Background()); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	// 等后台真的发出请求，再重复调用。
	deadline := time.Now().Add(5 * time.Second)
	for hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	again, err := u.Apply(context.Background())
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if again.Phase != "downloading" {
		t.Errorf("second Apply phase = %s, want the in-flight downloading", again.Phase)
	}
	close(release)
	final := waitSettled(t, u)
	if hits.Load() != 1 {
		t.Errorf("asset requested %d times, want 1", hits.Load())
	}
	if final.Phase != "failed" {
		t.Errorf("final phase = %s, want failed after 500", final.Phase)
	}
}

// 契约：下载失败落成 failed 并带原因；失败后再点一次能重新开始（不卡在旧状态）。
func TestUpdater_Apply_FailureIsReportedAndRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	u := fakeUpdater(srv.URL+"/missing.zip", filepath.Join(t.TempDir(), "Fake.app"))

	if _, err := u.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	failed := waitSettled(t, u)
	if failed.Phase != "failed" || !strings.Contains(failed.Error, "404") {
		t.Fatalf("progress = %+v, want failed with the 404 in error", failed)
	}
	retry, err := u.Apply(context.Background())
	if err != nil {
		t.Fatalf("retry Apply: %v", err)
	}
	if retry.Phase != "downloading" || retry.Error != "" {
		t.Fatalf("retry progress = %+v, want a fresh downloading state", retry)
	}
	waitSettled(t, u)
}

// 契约：Progress 在从没更新过时报 idle，而不是空阶段。
func TestUpdater_Progress_IdleBeforeAnyApply(t *testing.T) {
	if got := NewUpdater("HuLuca1998/acpp").Progress().Phase; got != "idle" {
		t.Fatalf("phase = %q, want idle", got)
	}
}

// 契约：速度是最近 3 秒窗内的平均——匀速 1MB/s 报 1MB/s，中途停滞后
// 只按窗内的增量算（旧样本淘汰），窗内不足半秒时沿用上一次的值。
func TestSpeedMeter_AveragesOverRecentWindow(t *testing.T) {
	const mb = 1 << 20
	t0 := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	var m speedMeter

	if got := m.add(t0, 0); got != 0 {
		t.Errorf("first sample rate = %v, want 0", got)
	}
	if got := m.add(t0.Add(100*time.Millisecond), 512*1024); got != 0 {
		t.Errorf("rate within 0.5s = %v, want previous value 0 (too little time to judge)", got)
	}
	m.add(t0.Add(1*time.Second), 1*mb)
	m.add(t0.Add(2*time.Second), 2*mb)
	if got := m.add(t0.Add(3*time.Second), 3*mb); got != float64(mb) {
		t.Errorf("steady rate = %v B/s, want %d", got, mb)
	}
	// 停滞 2 秒：窗只剩 2s/3s/5s 三个样本，增量 1MB / 3s。
	got := m.add(t0.Add(5*time.Second), 3*mb)
	want := float64(mb) / 3
	if diff := got - want; diff > 1 || diff < -1 {
		t.Errorf("rate after stall = %v B/s, want ~%v", got, want)
	}
}
