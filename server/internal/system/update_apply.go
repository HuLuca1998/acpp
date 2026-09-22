package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
//
// 下载是**可续传**的：半成品落在 <dataDir>/updates/ 而不是随机临时目录，
// 断线自动按 Range 接着下，用户也能暂停 / 继续，进程重启后还认得上次
// 下到哪。实测代理链路只有 130–300 KB/s，一次下完 129MB 是奢望。

// applyCap 是一次更新从下载到装完的安全网上限。它不是给正常慢链路用的
// ——那由停滞检测管：实测 130–300 KB/s 的代理链路上 129MB 要 7–17 分钟，
// 以前 10 分钟的硬上限把两次下载都在 10m0s 整掐断了。
const applyCap = 2 * time.Hour

// downloadStallTimeout 是「多久没有新字节就算这一段连接死了」。速度慢不是
// 错，完全不动才是；停了就断开重连续传。
const downloadStallTimeout = 90 * time.Second

// maxDownloadRetries 是**连续没有任何进展**的重连次数上限；只要某次重连
// 又下到了新字节，计数就归零——慢而不停的链路允许无限次断线续传。
const maxDownloadRetries = 5

// retryBackoff 是断线后等多久再连。
const retryBackoff = 3 * time.Second

var (
	errDownloadStalled = errors.New("download stalled")
	errPaused          = errors.New("paused")
	errDiscarded       = errors.New("discarded")
)

// UpdateProgress 是一键更新的进行态快照。
type UpdateProgress struct {
	// Phase 是当前阶段：idle（没在更新）→ downloading → unpacking →
	// installing → restarting（重启器已拉起，进程马上没了）；下载可以停在
	// paused（半成品留着，再点继续接着下）；装好但没能自动重启时是 done
	//（Message 说明要手动重开），失败是 failed（Error 带原因，重试会续传）。
	Phase   string `json:"phase"`
	Version string `json:"version,omitempty"`
	// Downloaded / Total 是字节数（续传时 Downloaded 从上次的位置起算）；
	// Total 为 0 表示还不知道总长。
	Downloaded int64 `json:"downloaded"`
	Total      int64 `json:"total"`
	// Speed 是最近几秒的平均下载速度（字节/秒），下载阶段之外为 0。
	Speed float64 `json:"speed"`
	// 零值时间不输出（omitzero）：idle 态给前端一个 0001 年的时间只会添乱。
	StartedAt time.Time `json:"startedAt,omitzero"`
	UpdatedAt time.Time `json:"updatedAt,omitzero"`
	// Message 是给人看的一句话：断线续传中、已暂停、装好了怎么重启……
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// partMeta 是半成品旁边的小记录：这半截是从哪个地址下的、总共多长。
// 地址或长度对不上就说明 asset 换过了，半截作废从头下。
type partMeta struct {
	URL   string `json:"url"`
	Total int64  `json:"total"`
}

// Apply 启动（或继续）一次更新：校验后立刻返回起步进度，真正的下载安装
// 在后台跑，之后用 Progress 跟进。已经有一次在进行时直接返回它的进度
// （幂等），不会开第二份下载；暂停或失败后再调就是**续传**。只在桌面版可用。
func (s *Updater) Apply(ctx context.Context) (UpdateProgress, error) {
	s.mu.Lock()
	info, assetURL := s.cached, s.assetURL
	if s.running {
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
	if s.running {
		p := s.progress
		s.mu.Unlock()
		return p, nil
	}
	now := time.Now()
	downloaded, total := s.partState(info.LatestVersion, assetURL)
	s.progress = UpdateProgress{
		Phase: "downloading", Version: info.LatestVersion,
		Downloaded: downloaded, Total: total,
		StartedAt: now, UpdatedAt: now,
	}
	s.meter = speedMeter{}
	// 与请求脱钩：发起更新的那个页面切走、甚至关掉，下载都该继续。
	// 取消原因区分暂停 / 放弃，run 据此决定半成品的去留。
	runCtx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	s.cancel = cancel
	s.running = true
	p := s.progress
	s.mu.Unlock()

	go s.run(runCtx, info.LatestVersion, assetURL, bundle)
	return p, nil
}

// Pause 暂停正在进行的下载，半成品留着。只有下载阶段能暂停——解包与
// 替换只要几秒，停在中间只会留下半个 .app。
func (s *Updater) Pause() (UpdateProgress, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running || s.progress.Phase != "downloading" {
		return s.progress, fmt.Errorf("%w: 只有下载阶段可以暂停", service.ErrInvalid)
	}
	s.cancel(errPaused)
	// 立刻改成 paused 让界面有响应；run 收尾时会再确认一次。
	s.progress.Phase = "paused"
	s.progress.Speed = 0
	s.progress.Message = "已暂停，随时可以继续"
	s.progress.UpdatedAt = time.Now()
	return s.progress, nil
}

// Discard 放弃这次更新：停掉下载（若在进行）并删掉半成品，回到 idle。
func (s *Updater) Discard() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		// 半成品由 run 在收尾时删——它还握着文件句柄。
		s.cancel(errDiscarded)
		s.progress = UpdateProgress{}
		return nil
	}
	if v := s.cached.LatestVersion; v != "" {
		s.removePart(s.partPath(v))
	}
	s.progress = UpdateProgress{}
	return nil
}

// Progress 返回当前更新进度。没在更新时：磁盘上若留着这一版的半成品
// （上次暂停 / 进程重启过），报 paused 并带上下到哪了，否则 idle。
func (s *Updater) Progress() UpdateProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.progress.Phase != "" {
		return s.progress
	}
	if v := s.cached.LatestVersion; v != "" && s.assetURL != "" {
		if downloaded, total := s.partState(v, s.assetURL); downloaded > 0 {
			return UpdateProgress{
				Phase: "paused", Version: v, Downloaded: downloaded, Total: total,
				Message: "上次下到一半，可以接着下",
			}
		}
	}
	return UpdateProgress{Phase: "idle"}
}

func (s *Updater) setPhase(phase string) {
	s.mu.Lock()
	s.progress.Phase = phase
	s.progress.Speed = 0
	s.progress.Message = ""
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

// note 更新给人看的那句话（断线续传中……），阶段不变。
func (s *Updater) note(message string) {
	s.mu.Lock()
	s.progress.Message = message
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
// 给出重试（下载失败重试即续传）；成功的终态是 restarting（进程随即被壳的
// 退出带走）或 done。
func (s *Updater) run(ctx context.Context, version, assetURL, bundle string) {
	ctx, capCancel := context.WithTimeout(ctx, applyCap)
	defer capCancel()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.cancel = nil
		s.mu.Unlock()
	}()

	part := s.partPath(version)
	s.cleanOtherParts(part)

	// 1. 下载（续传 + 断线重连）
	if err := s.download(ctx, assetURL, part); err != nil {
		switch cause := context.Cause(ctx); {
		case errors.Is(cause, errPaused):
			s.finish("paused", "已暂停，随时可以继续", nil)
		case errors.Is(cause, errDiscarded):
			s.removePart(part)
			s.mu.Lock()
			s.progress = UpdateProgress{}
			s.mu.Unlock()
		default:
			s.finish("failed", "", err)
		}
		return
	}

	// 2. 解包并校验形状（ditto 保留签名与扩展属性）
	s.setPhase("unpacking")
	tmpDir, err := os.MkdirTemp("", "acpp-update-*")
	if err != nil {
		s.finish("failed", "", fmt.Errorf("create temp dir: %w", err))
		return
	}
	defer os.RemoveAll(tmpDir)
	unpackDir := filepath.Join(tmpDir, "unpacked")
	if out, err := exec.CommandContext(ctx, "/usr/bin/ditto", "-xk", part, unpackDir).CombinedOutput(); err != nil {
		// 解不开就是包坏了（续传拼错、asset 换过），留着只会让每次重试都
		// 在同一处失败——删掉，下次从头下。
		s.removePart(part)
		s.finish("failed", "", fmt.Errorf("unpack failed: %s: %w", tailString(string(out), 500), err))
		return
	}
	newBundle, err := findAppBundle(unpackDir)
	if err != nil {
		s.removePart(part)
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
	s.removePart(part)

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

// ---- 半成品 ----

func (s *Updater) partPath(version string) string {
	return filepath.Join(s.cacheDir, "ACPP-"+version+".zip.part")
}

func metaPath(part string) string { return part + ".json" }

func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

func readMeta(part string) partMeta {
	var m partMeta
	if raw, err := os.ReadFile(metaPath(part)); err == nil {
		_ = json.Unmarshal(raw, &m)
	}
	return m
}

func writeMeta(part string, m partMeta) {
	if raw, err := json.Marshal(m); err == nil {
		_ = os.WriteFile(metaPath(part), raw, 0o644)
	}
}

func (s *Updater) removePart(part string) {
	_ = os.Remove(part)
	_ = os.Remove(metaPath(part))
}

// partState 读这一版半成品的进度；地址对不上（asset 换过）当作没有。
func (s *Updater) partState(version, assetURL string) (downloaded, total int64) {
	part := s.partPath(version)
	m := readMeta(part)
	if m.URL != assetURL {
		return 0, 0
	}
	return fileSize(part), m.Total
}

// cleanOtherParts 清掉别的版本留下的半成品：目标版本换了，旧的半截再也用不上。
func (s *Updater) cleanOtherParts(keep string) {
	entries, err := os.ReadDir(s.cacheDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		full := filepath.Join(s.cacheDir, e.Name())
		if full == keep || full == metaPath(keep) {
			continue
		}
		_ = os.Remove(full)
	}
}

// ---- 下载 ----

// stallTimeout / maxRetries / backoff 允许测试注入短值。
func (s *Updater) stallTimeout() time.Duration {
	if s.stall > 0 {
		return s.stall
	}
	return downloadStallTimeout
}

func (s *Updater) maxRetries() int {
	if s.retries > 0 {
		return s.retries
	}
	return maxDownloadRetries
}

func (s *Updater) backoff() time.Duration {
	if s.retryWait > 0 {
		return s.retryWait
	}
	return retryBackoff
}

// download 把 asset 下到 part：断线 / 停滞就按已下到的位置续传，连续
// 几次都毫无进展才放弃。非 2xx 的回应（asset 没了、被拒）不重试。
func (s *Updater) download(ctx context.Context, url, part string) error {
	if err := os.MkdirAll(s.cacheDir, 0o755); err != nil {
		return fmt.Errorf("create update cache: %w", err)
	}
	failures := 0
	for {
		before := fileSize(part)
		err := downloadRange(ctx, url, part, s.stallTimeout(), s.reportDownload, s.note)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return err
		}
		var status *httpStatusError
		if errors.As(err, &status) {
			return err
		}
		if fileSize(part) > before {
			failures = 0
		} else {
			failures++
		}
		if failures >= s.maxRetries() {
			return err
		}
		wait := s.backoff()
		s.note(fmt.Sprintf("连接中断（%s），%s 后从 %s 处续传", shortErr(err), wait, humanBytes(fileSize(part))))
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
}

// httpStatusError 是服务端明确拒绝（非 2xx），不值得重试。
type httpStatusError struct{ status string }

func (e *httpStatusError) Error() string { return "download update: " + e.status }

// downloadRange 从 part 现有长度处续传一段，直到下完或出错。
//
// 服务端不认 Range（回 200）就从头来；回 416 或 Content-Range 对不上
// 就说明半截作废，清掉后由调用方重来。失败条件是**停滞**而不是总时长：
// 连续 stall 没有新字节才中断，慢链路只要还在动就让它下完。
func downloadRange(ctx context.Context, url, part string, stall time.Duration,
	report func(n, total int64), status func(string)) error {
	offset := fileSize(part)
	if m := readMeta(part); offset > 0 && m.URL != "" && m.URL != url {
		// asset 地址换了：半截是别的东西，作废。
		_ = os.Remove(part)
		offset = 0
	}

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	watchdog := time.AfterFunc(stall, func() { cancel(errDownloadStalled) })
	defer watchdog.Stop()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build download request: %w", err)
	}
	req.Header.Set("User-Agent", "acpp-updater")
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return stallOr(ctx, stall, fmt.Errorf("download update: %w", err))
	}
	defer resp.Body.Close()

	var total int64
	switch resp.StatusCode {
	case http.StatusPartialContent:
		var from, to int64
		if _, err := fmt.Sscanf(resp.Header.Get("Content-Range"), "bytes %d-%d/%d", &from, &to, &total); err != nil || from != offset {
			_ = os.Remove(part)
			return fmt.Errorf("续传位置对不上（Content-Range %q，本地 %d），半成品作废重下", resp.Header.Get("Content-Range"), offset)
		}
	case http.StatusOK:
		// 服务端不支持 Range：已有的半截没用了，从头写。
		if offset > 0 {
			_ = os.Remove(part)
			offset = 0
		}
		total = max(resp.ContentLength, 0)
	case http.StatusRequestedRangeNotSatisfiable:
		_ = os.Remove(part)
		return fmt.Errorf("服务端拒绝续传位置 %d，半成品作废重下", offset)
	default:
		return &httpStatusError{status: resp.Status}
	}
	if m := readMeta(part); offset > 0 && m.Total > 0 && total > 0 && m.Total != total {
		_ = os.Remove(part)
		return fmt.Errorf("asset 长度变了（%d → %d），半成品作废重下", m.Total, total)
	}
	writeMeta(part, partMeta{URL: url, Total: total})
	status("")

	f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open download file: %w", err)
	}
	defer f.Close()
	// 每来一块字节就把看门狗往后拨；停滞的判定只看字节，不看请求握手。
	onWrite := func(n, total int64) {
		watchdog.Reset(stall)
		report(n, total)
	}
	onWrite(offset, total)
	if _, err := io.Copy(&countingWriter{w: f, n: offset, total: total, report: onWrite}, resp.Body); err != nil {
		return stallOr(ctx, stall, fmt.Errorf("write download file: %w", err))
	}
	return nil
}

// stallOr 把看门狗触发的取消翻译成人话，其余错误原样。
func stallOr(ctx context.Context, stall time.Duration, err error) error {
	if errors.Is(context.Cause(ctx), errDownloadStalled) {
		return fmt.Errorf("下载停滞超过 %s 没有新数据", stall)
	}
	return err
}

// shortErr 取错误链最里层的一句，给进度条上的一行小字用。
func shortErr(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 {
		msg = msg[i+2:]
	}
	return tailString(msg, 60)
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// countingWriter 把每次写入累加后报出去；n 从续传位置起算。
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
