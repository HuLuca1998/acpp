package system

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"acpp/server/internal/config"

	"acpp/server/internal/service"
)

// UpdateInfo 是版本检查结果，直接下发前端。
type UpdateInfo struct {
	CurrentVersion string `json:"currentVersion"`
	Repo           string `json:"repo"`
	LatestVersion  string `json:"latestVersion,omitempty"`
	HasUpdate      bool   `json:"hasUpdate"`
	// Notes 是 release 描述（markdown 原文，前端按纯文本展示）。
	Notes       string `json:"notes,omitempty"`
	PublishedAt string `json:"publishedAt,omitempty"`
	ReleaseURL  string `json:"releaseUrl,omitempty"`
	AssetName   string `json:"assetName,omitempty"`
	CheckedAt   string `json:"checkedAt,omitempty"`
	CheckError  string `json:"checkError,omitempty"`
	// CanApply 表示当前进程跑在 .app bundle 里，支持一键更新重启；
	// 开发态（go run / make serve）只能看不能装。
	CanApply bool `json:"canApply"`
}

// Updater 负责版本检查（GitHub Releases）与 macOS 桌面版的自更新。
type Updater struct {
	repo string
	// apiBase 可注入以便测试，默认 GitHub 官方 API。
	apiBase string

	mu       sync.Mutex
	cached   UpdateInfo
	assetURL string
}

func NewUpdater(repo string) *Updater {
	return &Updater{repo: repo, apiBase: "https://api.github.com"}
}

// StartPeriodicCheck 启动即查一次，之后按 interval 周期刷新缓存。
func (s *Updater) StartPeriodicCheck(ctx context.Context, interval time.Duration) {
	go func() {
		s.refresh(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.refresh(ctx)
			}
		}
	}()
}

// Info 返回缓存的检查结果；force 时现查。从未查过也现查一次。
func (s *Updater) Info(ctx context.Context, force bool) UpdateInfo {
	s.mu.Lock()
	stale := s.cached.CheckedAt == ""
	s.mu.Unlock()
	if force || stale {
		s.refresh(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cached
}

// githubRelease 是 GitHub Releases API 的响应子集。
type githubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func (s *Updater) refresh(ctx context.Context) {
	info := UpdateInfo{
		CurrentVersion: config.Version,
		Repo:           s.repo,
		CheckedAt:      time.Now().Format(time.RFC3339),
		CanApply:       runningInAppBundle(),
	}
	assetURL := ""

	release, err := s.fetchLatest(ctx)
	if err != nil {
		info.CheckError = err.Error()
	} else {
		latest := strings.TrimPrefix(release.TagName, "v")
		info.LatestVersion = latest
		info.HasUpdate = versionLess(config.Version, latest)
		info.Notes = release.Body
		info.PublishedAt = release.PublishedAt
		info.ReleaseURL = release.HTMLURL
		for _, asset := range release.Assets {
			if strings.HasSuffix(asset.Name, ".zip") {
				info.AssetName = asset.Name
				assetURL = asset.BrowserDownloadURL
				break
			}
		}
	}

	s.mu.Lock()
	s.cached = info
	s.assetURL = assetURL
	s.mu.Unlock()
}

func (s *Updater) fetchLatest(ctx context.Context) (*githubRelease, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	url := fmt.Sprintf("%s/repos/%s/releases/latest", s.apiBase, s.repo)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	// GitHub API 要求带 UA，匿名访问公共仓库即可（60 次/小时的限额足够）。
	req.Header.Set("User-Agent", "acpp-updater")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("check releases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("仓库 %s 还没有发布任何版本", s.repo)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API %s", resp.Status)
	}
	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("parse release: %w", err)
	}
	return &release, nil
}

// Apply 下载最新 release 的 zip，原地替换 .app bundle，然后拉起分离的
// 重启器（先让壳走正常退出回收子进程，再 open 新包）。只在桌面版可用。
func (s *Updater) Apply(ctx context.Context) (string, error) {
	s.mu.Lock()
	info, assetURL := s.cached, s.assetURL
	s.mu.Unlock()

	if !info.HasUpdate || assetURL == "" {
		return "", fmt.Errorf("%w: no update available", service.ErrInvalid)
	}
	if !runningInAppBundle() {
		return "", fmt.Errorf("%w: 一键更新仅桌面版支持，开发态请 git pull 后重启", service.ErrInvalid)
	}
	bundle, err := currentBundlePath()
	if err != nil {
		return "", err
	}

	// 1. 下载
	tmpDir, err := os.MkdirTemp("", "acpp-update-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	zipPath := filepath.Join(tmpDir, "update.zip")
	if err := downloadFile(ctx, assetURL, zipPath); err != nil {
		return "", err
	}

	// 2. 解包并校验形状（ditto 保留签名与扩展属性）
	unpackDir := filepath.Join(tmpDir, "unpacked")
	if out, err := exec.CommandContext(ctx, "/usr/bin/ditto", "-xk", zipPath, unpackDir).CombinedOutput(); err != nil {
		return "", fmt.Errorf("unpack failed: %s: %w", tailString(string(out), 500), err)
	}
	newBundle, err := findAppBundle(unpackDir)
	if err != nil {
		return "", err
	}

	// 3. 原地替换：旧包先挪走，ditto 拷入新包，失败回滚
	backup := bundle + ".old"
	_ = os.RemoveAll(backup)
	if err := os.Rename(bundle, backup); err != nil {
		return "", fmt.Errorf("move old bundle: %w", err)
	}
	if out, err := exec.CommandContext(ctx, "/usr/bin/ditto", newBundle, bundle).CombinedOutput(); err != nil {
		_ = os.Rename(backup, bundle)
		return "", fmt.Errorf("install new bundle: %s: %w", tailString(string(out), 500), err)
	}
	_ = os.RemoveAll(backup)
	_ = os.RemoveAll(tmpDir)

	// 4. 分离重启器：TERM 让壳走正常退出路径（回收本进程与 agent 子进程），
	//    随后 open 新包。Setsid 保证它不随本进程一起死。
	//
	//    必须**等壳真正退出**再 open：壳的退出要回收 acp-server 连带全部
	//    agent 子进程，耗时轻松超过固定 sleep；旧实例还活着时 open 只会
	//    「激活」它而不启动新进程，结果就是只更新不重启。上限 60 秒，
	//    超时 SIGKILL 兜底（孤儿端口下次启动由壳的清理逻辑接管）。
	shellPID := os.Getppid()
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
		return "更新已安装，但自动重启失败——请手动退出并重新打开 ACPP", nil
	}
	return fmt.Sprintf("已更新到 %s，应用即将自动重启", info.LatestVersion), nil
}

// versionLess 报告 a < b（点分数字段逐段比较，段数不齐补零；
// 非数字段退化为字符串比较）。
func versionLess(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var sa, sb string
		if i < len(as) {
			sa = as[i]
		}
		if i < len(bs) {
			sb = bs[i]
		}
		na, errA := strconv.Atoi(sa)
		nb, errB := strconv.Atoi(sb)
		if errA != nil || errB != nil {
			if sa != sb {
				return sa < sb
			}
			continue
		}
		if na != nb {
			return na < nb
		}
	}
	return false
}

// runningInAppBundle 判断当前进程是否住在 .app 里（桌面版打包形态）。
func runningInAppBundle() bool {
	exe, err := os.Executable()
	return err == nil && strings.Contains(exe, ".app/Contents/MacOS/")
}

func currentBundlePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable: %w", err)
	}
	idx := strings.LastIndex(exe, ".app/Contents/MacOS/")
	if idx < 0 {
		return "", fmt.Errorf("%w: not running inside an app bundle", service.ErrInvalid)
	}
	return exe[:idx+len(".app")], nil
}

func findAppBundle(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read unpacked dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasSuffix(e.Name(), ".app") {
			bundle := filepath.Join(dir, e.Name())
			// 形状校验：壳与 server 都得在，防止把残缺包装进系统
			if _, err := os.Stat(filepath.Join(bundle, "Contents/MacOS/acp-server")); err != nil {
				return "", fmt.Errorf("asset 里的 %s 缺少 acp-server，拒绝安装", e.Name())
			}
			return bundle, nil
		}
	}
	return "", fmt.Errorf("release asset 里没有 .app bundle")
}

func downloadFile(ctx context.Context, url, dest string) error {
	dlCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, url, nil)
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
	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create download file: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write download file: %w", err)
	}
	return nil
}
