package schedule

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// 契约：cron 按 tz 的墙钟解释；非法表达式与时区在 Add 时就拦住。

func TestNextRespectsTimezone(t *testing.T) {
	// 2026-09-03 00:00 UTC = 上海 08:00，下一次「每天 10:00 上海」是 02:00 UTC。
	after := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	n, err := Next("0 10 * * *", "Asia/Shanghai", after)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 9, 3, 2, 0, 0, 0, time.UTC); !n.Equal(want) {
		t.Errorf("Next = %v, want %v", n, want)
	}
	// 已过今天 10:00（上海 11:00 = 03:00 UTC）→ 明天。
	n, _ = Next("0 10 * * *", "Asia/Shanghai", after.Add(3*time.Hour))
	if want := time.Date(2026, 9, 4, 2, 0, 0, 0, time.UTC); !n.Equal(want) {
		t.Errorf("Next(next day) = %v, want %v", n, want)
	}
	for _, bad := range []string{"", "0 10 * *", "61 * * * *", "TZ=UTC 0 1 * * *"} {
		if _, err := Next(bad, "", after); !errors.Is(err, ErrInvalid) {
			t.Errorf("Next(%q) err = %v, want ErrInvalid", bad, err)
		}
	}
	if _, err := Next("0 10 * * *", "Mars/Olympus", after); !errors.Is(err, ErrInvalid) {
		t.Errorf("坏时区应报 ErrInvalid，得到 %v", err)
	}
}

func TestDescribe(t *testing.T) {
	cases := map[string]string{
		"0 10 * * *":   "每天 10:00 (Asia/Shanghai)",
		"30 9 * * 1":   "每周一 09:30 (Asia/Shanghai)",
		"0 */2 * * *":  "每 2 小时 (Asia/Shanghai)",
		"*/15 * * * *": "每 15 分钟 (Asia/Shanghai)",
		"0 8 1 * *":    "每月 1 日 08:00 (Asia/Shanghai)",
		"5 4 * * 1-5":  "每个工作日 04:05 (Asia/Shanghai)",
		"0 0 1 1 *":    "0 0 1 1 * (Asia/Shanghai)",
	}
	for expr, want := range cases {
		if got := (Job{Cron: expr, TZ: "Asia/Shanghai"}).Describe(); got != want {
			t.Errorf("Describe(%q) = %q, want %q", expr, got, want)
		}
	}
}

// 契约：任务定义落盘后重载还原；校验挡住空名字/空提示词/双计划。

func TestAddUpdateRemovePersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedule.json")
	s, err := New(path, nil, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 3, 1, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	j, err := s.Add(Input{Scope: "ch1", Name: "日报", Cron: "0 10 * * *", TZ: "Asia/Shanghai", Prompt: "写日报", CreatedBy: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if j.NextRunAt == nil || !j.NextRunAt.Equal(time.Date(2026, 9, 3, 2, 0, 0, 0, time.UTC)) {
		t.Errorf("NextRunAt = %v", j.NextRunAt)
	}
	for _, in := range []Input{
		{Scope: "ch1", Cron: "0 10 * * *", Prompt: "x"},
		{Scope: "ch1", Name: "n", Cron: "0 10 * * *"},
		{Scope: "ch1", Name: "n", Prompt: "x"},
		{Scope: "ch1", Name: "n", Cron: "0 10 * * *", At: ptr(now.Add(time.Hour)), Prompt: "x"},
		{Scope: "", Name: "n", Cron: "0 10 * * *", Prompt: "x"},
		{Scope: "ch1", Name: "n", At: ptr(now.Add(-2 * time.Hour)), Prompt: "x"},
	} {
		if _, err := s.Add(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("Add(%+v) err = %v, want ErrInvalid", in, err)
		}
	}

	off := false
	upd, err := s.Update(j.ID, Patch{Enabled: &off})
	if err != nil {
		t.Fatal(err)
	}
	if upd.Enabled || upd.NextRunAt != nil {
		t.Errorf("停用后仍有计划: %+v", upd)
	}
	name := "  周报 "
	cronExpr := "0 9 * * 1"
	on := true
	upd, err = s.Update(j.ID, Patch{Name: &name, Cron: &cronExpr, Enabled: &on})
	if err != nil {
		t.Fatal(err)
	}
	if upd.Name != "周报" || upd.Cron != cronExpr || upd.NextRunAt == nil {
		t.Errorf("Update 结果不对: %+v", upd)
	}
	if _, err := s.Update("nope", Patch{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("改不存在的任务应 ErrNotFound，得到 %v", err)
	}

	s2, err := New(path, nil, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	got := s2.Jobs("ch1")
	if len(got) != 1 || got[0].Name != "周报" || got[0].CreatedBy != "u1" {
		t.Errorf("重载不一致: %+v", got)
	}
	if len(s2.Jobs("other")) != 0 {
		t.Error("scope 过滤失效")
	}
	if err := s2.Remove(j.ID); err != nil {
		t.Fatal(err)
	}
	if err := s2.Remove(j.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("二次删除应 ErrNotFound，得到 %v", err)
	}
}

// fakeRunner 可控地阻塞/放行，记下收到的调用。
type fakeRunner struct {
	mu    sync.Mutex
	calls []Run
	gate  chan struct{}
	res   Result
	err   error
}

func (f *fakeRunner) run(ctx context.Context, job Job, run Run) (Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, run)
	f.mu.Unlock()
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}
	return f.res, f.err
}

func (f *fakeRunner) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("等待超时")
}

func newSvc(t *testing.T, r Runner, hooks Hooks) (*Service, *time.Time) {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "s.json"), r, hooks)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 3, 1, 59, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	return s, &now
}

// 契约：到点起跑一次且只起一次；上一轮没跑完时新的一次记 skipped 而不是并发。

func TestTickLaunchesOnceAndSkipsOverlap(t *testing.T) {
	fr := &fakeRunner{gate: make(chan struct{}), res: Result{Status: StatusOK, Summary: "done"}}
	s, now := newSvc(t, fr.run, Hooks{})
	j, err := s.Add(Input{Scope: "c", Name: "n", Cron: "*/1 * * * *", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	s.tick(*now) // 01:59 < 02:00，不到点
	if fr.count() != 0 {
		t.Fatal("不到点却起跑了")
	}
	*now = now.Add(time.Minute)
	s.tick(*now)
	waitFor(t, func() bool { return fr.count() == 1 })
	s.tick(*now) // 同一分钟重复 tick 不重复起跑
	if fr.count() != 1 {
		t.Fatalf("重复起跑: %d", fr.count())
	}
	got, _ := s.Get(j.ID)
	if !got.Running || len(got.Runs) != 1 || got.Runs[0].Status != StatusRunning {
		t.Errorf("运行中状态不对: %+v", got)
	}

	*now = now.Add(time.Minute) // 02:01 到点，但上一轮还在跑
	s.tick(*now)
	s.tick(*now)
	got, _ = s.Get(j.ID)
	if fr.count() != 1 || len(got.Runs) != 2 || got.Runs[1].Status != StatusSkipped {
		t.Errorf("重叠应记一条 skipped: calls=%d runs=%+v", fr.count(), got.Runs)
	}

	close(fr.gate)
	waitFor(t, func() bool { g, _ := s.Get(j.ID); return !g.Running })
	got, _ = s.Get(j.ID)
	r := got.Runs[0]
	if r.Status != StatusOK || r.Summary != "done" || got.LastStatus != StatusOK || got.FailStreak != 0 {
		t.Errorf("完成后记录不对: %+v", got)
	}
	if got.NextRunAt == nil || !got.NextRunAt.After(*now) {
		t.Errorf("下一次计划应在未来: %v", got.NextRunAt)
	}
}

// 契约：ErrBusy 不算失败——记录撤掉、一分钟后重试；连续失败退避，
// 到阈值自动停用并通知；一次性任务成功即删。

func TestBusyRetryFailureBackoffAndAutoDisable(t *testing.T) {
	fr := &fakeRunner{err: ErrBusy}
	var disabledMu sync.Mutex
	var disabled []string
	s, now := newSvc(t, fr.run, Hooks{Disabled: func(j Job, reason string) {
		disabledMu.Lock()
		disabled = append(disabled, reason)
		disabledMu.Unlock()
	}})
	j, err := s.Add(Input{Scope: "c", Name: "n", Cron: "0 2 * * *", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	*now = time.Date(2026, 9, 3, 2, 0, 0, 0, time.UTC)
	s.tick(*now)
	waitFor(t, func() bool { g, _ := s.Get(j.ID); return !g.Running && fr.count() == 1 })
	got, _ := s.Get(j.ID)
	if len(got.Runs) != 0 || got.FailStreak != 0 {
		t.Errorf("busy 不该留下记录或计失败: %+v", got)
	}
	if got.NextRunAt == nil || !got.NextRunAt.Equal(now.Add(busyRetry)) {
		t.Errorf("busy 后应一分钟重试，得到 %v", got.NextRunAt)
	}

	// 转成真失败：退避 5m → 15m → 60m → 60m，第 5 次停用。
	fr.err = errors.New("boom")
	wantBackoff := []time.Duration{5 * time.Minute, 15 * time.Minute, 60 * time.Minute, 60 * time.Minute}
	for i, bo := range wantBackoff {
		g, _ := s.Get(j.ID)
		*now = *g.NextRunAt
		s.tick(*now)
		waitFor(t, func() bool { g, _ := s.Get(j.ID); return !g.Running && g.FailStreak == i+1 })
		g, _ = s.Get(j.ID)
		if g.NextRunAt == nil || !g.NextRunAt.Equal(now.Add(bo)) {
			t.Fatalf("第 %d 次失败后重试时刻 = %v, want +%v", i+1, g.NextRunAt, bo)
		}
	}
	g, _ := s.Get(j.ID)
	*now = *g.NextRunAt
	s.tick(*now)
	waitFor(t, func() bool { g, _ := s.Get(j.ID); return !g.Enabled })
	g, _ = s.Get(j.ID)
	if g.DisabledReason == "" || g.NextRunAt != nil || g.FailStreak != 5 {
		t.Errorf("应自动停用: %+v", g)
	}
	waitFor(t, func() bool { disabledMu.Lock(); defer disabledMu.Unlock(); return len(disabled) == 1 })
	if !strings.Contains(g.Runs[len(g.Runs)-1].Error, "boom") {
		t.Errorf("失败原因应进记录: %+v", g.Runs)
	}

	// 人工启用清掉停用原因与连败。
	on := true
	g, err = s.Update(j.ID, Patch{Enabled: &on})
	if err != nil {
		t.Fatal(err)
	}
	if g.DisabledReason != "" || g.FailStreak != 0 || g.NextRunAt == nil {
		t.Errorf("启用后应清零: %+v", g)
	}

	// 一次性任务：成功即删。
	fr.err, fr.res = nil, Result{Status: StatusSilent}
	once, err := s.Add(Input{Scope: "c", Name: "once", At: ptr(now.Add(2 * time.Minute)), Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Minute)
	s.tick(*now)
	waitFor(t, func() bool { _, ok := s.Get(once.ID); return !ok })
}

// 契约：错过太久不补跑（记 skipped 并重排）；进程重启把遗留的 running 判成中断；
// 手动触发不看启用状态、撞上运行中报 ErrRunning。

func TestMissedGraceReloadAndRunNow(t *testing.T) {
	fr := &fakeRunner{gate: make(chan struct{}), res: Result{Status: StatusOK}}
	s, now := newSvc(t, fr.run, Hooks{})
	j, err := s.Add(Input{Scope: "c", Name: "n", Cron: "0 2 * * *", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	*now = time.Date(2026, 9, 3, 3, 0, 0, 0, time.UTC) // 晚了一小时
	s.tick(*now)
	got, _ := s.Get(j.ID)
	if fr.count() != 0 || len(got.Runs) != 1 || got.Runs[0].Status != StatusSkipped {
		t.Fatalf("错过应记 skipped 不起跑: calls=%d runs=%+v", fr.count(), got.Runs)
	}
	if got.NextRunAt == nil || !got.NextRunAt.Equal(time.Date(2026, 9, 4, 2, 0, 0, 0, time.UTC)) {
		t.Errorf("错过后应排到明天: %v", got.NextRunAt)
	}

	off := false
	if _, err := s.Update(j.ID, Patch{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunNow(j.ID); err != nil {
		t.Fatalf("停用的任务也应能手动跑: %v", err)
	}
	waitFor(t, func() bool { return fr.count() == 1 })
	if err := s.RunNow(j.ID); !errors.Is(err, ErrRunning) {
		t.Errorf("运行中再触发应 ErrRunning，得到 %v", err)
	}
	if err := s.RunNow("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("不存在应 ErrNotFound，得到 %v", err)
	}

	// 「进程死了」：不放行 runner，直接用同一文件重建服务。
	s2, err := New(s.path, nil, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	g2, _ := s2.Get(j.ID)
	last := g2.Runs[len(g2.Runs)-1]
	if g2.Running || last.Status != StatusError || !strings.Contains(last.Error, "中断") {
		t.Errorf("重启后遗留运行应判中断: running=%v last=%+v", g2.Running, last)
	}
	close(fr.gate)
	waitFor(t, func() bool { g, _ := s.Get(j.ID); return !g.Running })
	// 手动运行的失败/成功不改计划（停用的仍停用）。
	g, _ := s.Get(j.ID)
	if g.Enabled || g.NextRunAt != nil {
		t.Errorf("手动运行不该改启用状态: %+v", g)
	}
	if n := s.RemoveScope("c"); n != 1 {
		t.Errorf("RemoveScope = %d, want 1", n)
	}
}
