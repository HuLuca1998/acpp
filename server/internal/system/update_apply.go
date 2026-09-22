package system

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"acpp/server/internal/service"
)

// 一键更新的执行面：下载 → 解包 → 替换 .app → 拉起重启器。
//
// 整个过程在后台 goroutine 里跑，`Apply` 立刻返回，前端轮询 `Progress` 画
// 进度条。以前是同步的——一个 HTTP 请求扛着 129MB 的下载，界面只有一个
// 转圈，网络慢时用户分不清是在下还是卡死了；请求一断（切页、断网）下载
// 也跟着作废。

// applyCap 是一次更新从下载到装完的安全网上限。它不是给正常慢链路用的
// ——那由停滞检测管：实测 130–300 KB/s 的代理链路上 129MB 要 7–17 分钟，
// 以前 10 分钟的硬上限把两次下载都在 10m0s 整掐断了。
const applyCap = 2 * time.Hour

// downloadStallTimeout 是「多久没有新字节就算下载死了」。速度慢不是错，
// 完全不动才是。
const downloadStallTimeout = 90 * time.Second

var errDownloadStalled = errors.New("download stalled")

// UpdateProgress 是一键更新的进行态快照。
type UpdateProgress struct {
	// Phase 是当前阶段：idle（没在更新）→ downloading → unpacking →
	// installing → restarting（重启器已拉起，进程马上没了）；装好但没能
	// 自动重启时是 done（Message 说明要手动重开），失败是 failed（Error 带原因）。
	Phase   string `json:"phase"`
	Version string `json:"version,omitempty"`
	// Downloaded / Total 是字节数；Total 为 0 表示服务端没给长度。
	Downloaded int64 `json:"downloaded"`
	Total      int64 `json:"total"`
	// Speed 是最近几秒的平均下载速度（字节/秒），下载阶段之外为 0。
	Speed float64 `json:"speed"`
	// 零值时间不输出（omitzero）：idle 态给前端一个 0001 年的时间只会添乱。
	StartedAt time.Time `json:"startedAt,omitzero"`
	UpdatedAt time.Time `json:"updatedAt,omitzero"`
	Message   string    `json:"message,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// activeUpdate 报告这份进度是否还在进行中（再点一次「更新」不该重开一份）。
func (p UpdateProgress) active() bool {
	switch p.Phase {
	case "downloading", "unpacking", "installing", "restarting":
		return true
	}
	return false
}

// Apply 启动一次更新：校验后立刻返回初始进度，真正的下载安装在后台跑，
// 之后用 Progress 跟进。已经有一次在进行时直接返回它的进度（幂等），
// 不会开第二份下载。只在桌面版可用。
func (s *Updater) Apply(ctx context.Context) (UpdateProgress, error) {
	s.mu.Lock()
	info, assetURL := s.cached, s.assetURL
	if s.progress.active() {
		p := s.progress
		s.mu.Unlock()
		return p, nil
	}
	s.mu.Unlock()

	if !info.HasUpdate || assetURL == "" {
		return UpdateProgress{}, fmt.Errorf("%w: no update available", service.ErrInvalid)
	}
	bundle, err := s.bundlePath()
	if err != nil {
		return UpdateProgress{}, fmt.Errorf("%w: 一键更新仅桌面版支持，开发态请 git pull 后重启", service.ErrInvalid)
	}

	s.mu.Lock()
	if s.progress.active() {
		p := s.progress
		s.mu.Unlock()
		return p, nil
	}
	now := time.Now()
	s.progress = UpdateProgress{Phase: "downloading", Version: info.LatestVersion, StartedAt: now, UpdatedAt: now}
	s.meter = speedMeter{}
	p := s.progress
	s.mu.Unlock()

	// 与请求脱钩：发起更新的那个页面切走、甚至关掉，下载都该继续。
	go s.run(context.WithoutCancel(ctx), info.LatestVersion, assetURL, bundle)
	return p, nil
}

// Progress 返回当前更新进度；没在更新时 Phase 为 idle（或上一次的终态）。
func (s *Updater) Progress() UpdateProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.progress.Phase == "" {
		return UpdateProgress{Phase: "idle"}
	}
	return s.progress
}

func (s *Updater) setPhase(phase string) {
	s.mu.Lock()
	s.progress.Phase = phase
	s.progress.Speed = 0
	s.progress.UpdatedAt = time.Now()
	s.mu.Unlock()
}

func (s *Updater) finish(phase, message string, err error) {
	s.mu.Lock()
	s.progress.Phase = phase
	s.progress.Speed = 0
	s.progress.Message = message
	if err != nil {
		s.progress.Error = err.Error()
	}
	s.progress.UpdatedAt = time.Now()
	s.mu.Unlock()
}

// reportDownload 是下载回调：记字节数、算速度。
func (s *Updater) reportDownload(n, total int64) {
	now := time.Now()
	s.mu.Lock()
	s.progress.Downloaded = n
	s.progress.Total = total
	s.progress.Speed = s.meter.add(now, n)
	s.progress.UpdatedAt = now
	s.mu.Unlock()
}

// run 是后台的更新流水线。每一步失败都落成 failed 并保留原因，界面据此
// 给出重试；成功的终态是 restarting（进程随即被壳的退出带走）或 done。
func (s *Updater) run(ctx context.Context, version, assetURL, bundle string) {
	ctx, cancel := context.WithTimeout(ctx, applyCap)
	defer cancel()

	// 1. 下载
	tmpDir, err := os.MkdirTemp("", "acpp-update-*")
	if err != nil {
		s.finish("failed", "", fmt.Errorf("create temp dir: %w", err))
		return
	}
	defer os.RemoveAll(tmpDir)
	zipPath := filepath.Join(tmpDir, "update.zip")
	if err := downloadFile(ctx, assetURL, zipPath, s.stallTimeout(), s.reportDownload); err != nil {
		s.finish("failed", "", err)
		return
	}

	// 2. 解包并校验形状（ditto 保留签名与扩展属性）
	s.setPhase("unpacking")
	unpackDir := filepath.Join(tmpDir, "unpacked")
	if out, err := exec.CommandContext(ctx, "/usr/bin/ditto", "-xk", zipPath, unpackDir).CombinedOutput(); err != nil {
		s.finish("failed", "", fmt.Errorf("unpack failed: %s: %w", tailString(string(out), 500), err))
		return
	}
	newBundle, err := findAppBundle(unpackDir)
	if err != nil {
		s.finish("failed", "", err)
		return
	}

	// 3. 原地替换：旧包先挪走，ditto 拷入新包，失败回滚
	s.setPhase("installing")
	backup := bundle + ".old"
	_ = os.RemoveAll(backup)
	if err := os.Rename(bundle, backup); err != nil {
		s.finish("failed", "", fmt.Errorf("move old bundle: %w", err))
		return
	}
	if out, err := exec.CommandContext(ctx, "/usr/bin/ditto", newBundle, bundle).CombinedOutput(); err != nil {
		_ = os.Rename(backup, bundle)
		s.finish("failed", "", fmt.Errorf("install new bundle: %s: %w", tailString(string(out), 500), err))
		return
	}
	_ = os.RemoveAll(backup)

	// 4. 分离重启器：TERM 让壳走正常退出路径（回收本进程与 agent 子进程），
	//    随后 open 新包。Setsid 保证它不随本进程一起死。
	//
	//    必须**等壳真正退出**再 open：壳的退出要回收 acp-server 连带全部
	//    agent 子进程，耗时轻松超过固定 sleep；旧实例还活着时 open 只会
	//    「激活」它而不启动新进程，结果就是只更新不重启。上限 60 秒，
	//    超时 SIGKILL 兜底（孤儿端口下次启动由壳的清理逻辑接管）。
	shellPID := shellProcessID(bundle)
	if shellPID <= 0 {
		// 找不到壳就绝不乱发信号：早先这里直接用 getppid()，server 一旦
		// 孤儿化（父进程死过一轮，ppid 变成 1）就成了对 launchd 发 TERM，
		// 杀不动也等不到它退出，界面只能空转满 60 秒。宁可让用户手动重启。
		slog.Warn("apply update: 找不到壳进程，跳过自动重启", "bundle", bundle)
		s.finish("done", "更新已安装，但没找到应用进程——请手动退出并重新打开 ACPP", nil)
		return
	}
	script := fmt.Sprintf(
		"sleep 1; kill -TERM %[1]d 2>/dev/null; "+
			"i=0; while kill -0 %[1]d 2>/dev/null; do "+
			"i=$((i+1)); if [ $i -ge 120 ]; then kill -KILL %[1]d 2>/dev/null; sleep 1; break; fi; "+
			"sleep 0.5; done; "+
			"sleep 1; /usr/bin/open %[2]s",
		shellPID, strconv.Quote(bundle))
	restarter := exec.Command("/bin/sh", "-c", script)
	restarter.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := restarter.Start(); err != nil {
		slog.Error("start restarter", "err", err)
		s.finish("done", "更新已安装，但自动重启失败——请手动退出并重新打开 ACPP", nil)
		return
	}
	s.finish("restarting", fmt.Sprintf("已更新到 %s，应用即将自动重启", version), nil)
}

// stallTimeout 是停滞判定时长；测试注入短值。
func (s *Updater) stallTimeout() time.Duration {
	if s.stall > 0 {
		return s.stall
	}
	return downloadStallTimeout
}

// downloadFile 下载到 dest，每写一块就回调一次 (已下载字节, 总字节)；
// 服务端没给 Content-Length 时 total 为 0。失败条件是**停滞**而不是总时长：
// 连续 stall 没有新字节才中断，慢链路只要还在动就让它下完。
func downloadFile(ctx context.Context, url, dest string, stall time.Duration, report func(n, total int64)) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	watchdog := time.AfterFunc(stall, func() { cancel(errDownloadStalled) })
	defer watchdog.Stop()
	// 每来一块字节就把看门狗往后拨；停滞的判定只看字节，不看请求握手。
	onWrite := func(n, total int64) {
		watchdog.Reset(stall)
		report(n, total)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build download request: %w", err)
	}
	req.Header.Set("User-Agent", "acpp-updater")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download update: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download update: %s", resp.Status)
	}
	total := max(resp.ContentLength, 0)
	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create download file: %w", err)
	}
	defer f.Close()
	onWrite(0, total)
	if _, err := io.Copy(&countingWriter{w: f, total: total, report: onWrite}, resp.Body); err != nil {
		if errors.Is(context.Cause(ctx), errDownloadStalled) {
			return fmt.Errorf("下载停滞超过 %s 没有新数据，已中断——网络太慢或代理不稳，稍后重试", stall)
		}
		return fmt.Errorf("write download file: %w", err)
	}
	return nil
}

// countingWriter 把每次写入累加后报出去。
type countingWriter struct {
	w      io.Writer
	n      int64
	total  int64
	report func(n, total int64)
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	c.report(c.n, c.total)
	return n, err
}

// speedWindow 是速度取样的时间窗：太短跳得没法看，太长追不上网速变化。
const speedWindow = 3 * time.Second

// speedMeter 按最近 speedWindow 内的字节增量算平均速度。样本按时间淘汰，
// 窗内不足半秒时沿用上一次的值——刚开始那几十 KB 除以几毫秒会算出一个
// 吓人的数字。
type speedMeter struct {
	samples []speedSample
	rate    float64
}

type speedSample struct {
	at    time.Time
	bytes int64
}

// add 记入一个样本并返回当前速度（字节/秒）。
func (m *speedMeter) add(at time.Time, bytes int64) float64 {
	m.samples = append(m.samples, speedSample{at: at, bytes: bytes})
	cut := 0
	for cut < len(m.samples)-1 && at.Sub(m.samples[cut].at) > speedWindow {
		cut++
	}
	m.samples = m.samples[cut:]
	first := m.samples[0]
	if dt := at.Sub(first.at).Seconds(); dt >= 0.5 {
		m.rate = float64(bytes-first.bytes) / dt
	}
	return m.rate
}
