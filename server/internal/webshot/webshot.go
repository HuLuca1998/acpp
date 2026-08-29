// Package webshot 把一个页面渲染成整页 PNG：驱动本机已装的 Chrome
// （headless + CDP），不引浏览器自动化框架——只用得到四个 CDP 命令，
// 一个 mini 客户端（cdp.go）配现有的 websocket 依赖就够了，chromedp
// 连带的 cdproto 生成代码比本包大两个数量级。
//
// 找不到 Chrome 不是错误场景的主角：调用方应当把「渲染不了」当降级
// 处理（discord 的报告卡没有长图仍有预览链接）。
package webshot

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"os/exec"
	"regexp"
	"syscall"
	"time"
)

// chromeCandidates 是各平台常见的 Chrome 系浏览器落点，按优先级排列。
var chromeCandidates = []string{
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	"/Applications/Chromium.app/Contents/MacOS/Chromium",
	"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
}

var chromeNames = []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"}

// FindChrome 返回本机可用的 Chrome 可执行路径，找不到给空串。
func FindChrome() string {
	for _, p := range chromeCandidates {
		if _, err := exec.LookPath(p); err == nil {
			return p
		}
	}
	for _, n := range chromeNames {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	return ""
}

// maxCSSHeight 是整页截图的高度上限（css 像素）：CDP 截图受 GPU 纹理
// 尺寸约束，太高的页面截出来是黑块；超限就只截到这里，报告顶部才是重点。
const maxCSSHeight = 12000

// Capture 把 url（通常是 file://）渲染成整页 PNG。width 是 css 视口宽。
// 页面高度超过 maxCSSHeight 截断；常规高度用 2x 缩放（清晰），超高页
// 降回 1x 控制内存与文件体积。
func Capture(ctx context.Context, url string, width int) ([]byte, error) {
	chrome := FindChrome()
	if chrome == "" {
		return nil, fmt.Errorf("本机没有找到 Chrome/Chromium")
	}

	cmd := exec.CommandContext(ctx, chrome,
		"--headless=new", "--remote-debugging-port=0",
		"--no-first-run", "--no-default-browser-check",
		"--disable-gpu", "--hide-scrollbars", "--mute-audio",
		"--disable-extensions", "--disable-background-networking",
		"about:blank",
	)
	// 独立进程组：Chrome 会再拉自己的子进程，退出时要整组回收。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 chrome: %w", err)
	}
	defer func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	}()

	wsURL, err := devtoolsURL(ctx, stderr)
	if err != nil {
		return nil, err
	}
	c, err := dialCDP(ctx, wsURL)
	if err != nil {
		return nil, err
	}
	defer c.close()

	target, err := c.call(ctx, "", "Target.createTarget", map[string]any{"url": url})
	if err != nil {
		return nil, fmt.Errorf("开页面: %w", err)
	}
	attach, err := c.call(ctx, "", "Target.attachToTarget", map[string]any{
		"targetId": target["targetId"], "flatten": true,
	})
	if err != nil {
		return nil, fmt.Errorf("attach: %w", err)
	}
	sess, _ := attach["sessionId"].(string)

	if _, err := c.call(ctx, sess, "Page.enable", nil); err != nil {
		return nil, err
	}
	// 等加载完成事件；file:// 页面通常瞬间就绪，超时兜底继续截。
	c.waitEvent(ctx, sess, "Page.loadEventFired", 10*time.Second)
	// 给 web font / 首帧渲染留一拍。
	select {
	case <-time.After(400 * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	metrics, err := c.call(ctx, sess, "Page.getLayoutMetrics", nil)
	if err != nil {
		return nil, err
	}
	height := contentHeight(metrics)
	if height <= 0 {
		height = 800
	}
	if height > maxCSSHeight {
		height = maxCSSHeight
	}
	scale := 2
	if height > 6000 {
		scale = 1
	}
	if _, err := c.call(ctx, sess, "Emulation.setDeviceMetricsOverride", map[string]any{
		"width": width, "height": height, "deviceScaleFactor": scale, "mobile": false,
	}); err != nil {
		return nil, err
	}
	shot, err := c.call(ctx, sess, "Page.captureScreenshot", map[string]any{
		"format": "png", "captureBeyondViewport": true,
	})
	if err != nil {
		return nil, fmt.Errorf("截图: %w", err)
	}
	data, _ := shot["data"].(string)
	if data == "" {
		return nil, fmt.Errorf("截图返回空")
	}
	return base64.StdEncoding.DecodeString(data)
}

var devtoolsLine = regexp.MustCompile(`DevTools listening on (ws://\S+)`)

// devtoolsURL 从 chrome stderr 里等出 DevTools 的 ws 地址。
func devtoolsURL(ctx context.Context, stderr interface{ Read([]byte) (int, error) }) (string, error) {
	ch := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			if m := devtoolsLine.FindStringSubmatch(sc.Text()); m != nil {
				ch <- m[1]
				break
			}
		}
		close(ch)
	}()
	select {
	case u, ok := <-ch:
		if !ok || u == "" {
			return "", fmt.Errorf("chrome 没有报出 DevTools 地址")
		}
		return u, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// contentHeight 从 getLayoutMetrics 结果里挖内容高度（css 像素）。
func contentHeight(metrics map[string]any) int {
	for _, key := range []string{"cssContentSize", "contentSize"} {
		if cs, ok := metrics[key].(map[string]any); ok {
			if h, ok := cs["height"].(float64); ok {
				return int(h)
			}
		}
	}
	return 0
}

// Available 报告本机能不能渲染（设置页/日志用）。
func Available() bool { return FindChrome() != "" }
