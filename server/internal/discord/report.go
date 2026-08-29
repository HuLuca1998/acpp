package discord

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"acpp/server/internal/webshot"
)

// 报告面：agent 在子区会话里调 report_open 后，把「报告出炉了」这件事
// 变成子区里的一张卡。网页会话是就地弹预览面板；discord 没有面板，卡上
// 给一个跳浏览器的链接（本机点开即回环地址，owner 判定零摩擦）。

// reportOpened 是 report_open 的回调（经 Deps.Mounts 注册）：往子区发卡，
// 然后异步把报告渲染成整页长图直接发进子区（用户拍板的首选形态——
// 手机上不用跳浏览器）。渲染依赖本机 Chrome，失败只降级：卡上仍有
// 预览链接。
func (s *Service) reportOpened(token, threadID string, b Binding, rel, title string) {
	inner := []map[string]any{
		v2Text("### 📊 报告《" + trimRunes(title, 100) + "》"),
		v2Text("-# " + trimRunes(rel, 200)),
	}
	if s.deps.PreviewBase != "" {
		href := strings.TrimRight(s.deps.PreviewBase, "/") +
			"/api/discord/reports/" + b.ChannelID + "?path=" + url.QueryEscape(rel)
		inner = append(inner, map[string]any{"type": 1, "components": []map[string]any{{
			"type": 2, "style": 5, "label": "🔗 打开预览", "url": href,
		}}})
	}
	s.postCard(token, threadID, map[string]any{
		"flags": 1 << 15, "components": v2Container(colorBlurbe, inner),
	})
	go s.postReportImage(token, threadID, b, rel)
}

// reportImageMax 是长图直发的体积上限（Discord 免费档附件 25MB，留余量）。
const reportImageMax = 24 << 20

// postReportImage 把报告渲染成整页 PNG 发进子区。
func (s *Service) postReportImage(token, threadID string, b Binding, rel string) {
	abs, err := s.ReportPath(b.ChannelID, rel)
	if err != nil {
		slog.Warn("报告长图：路径解析失败", "rel", rel, "err", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	png, err := webshot.Capture(ctx, "file://"+abs, 1100)
	if err != nil {
		slog.Warn("报告长图：渲染失败（卡上仍有预览链接）", "rel", rel, "err", err)
		return
	}
	if len(png) > reportImageMax {
		slog.Warn("报告长图：超出附件上限，不发", "bytes", len(png))
		return
	}
	err = botRESTFile(ctx, token, threadID, map[string]any{
		"attachments":      []map[string]any{{"id": 0, "filename": "report.png"}},
		"allowed_mentions": noMentions(),
	}, "report.png", png)
	if err != nil {
		slog.Warn("报告长图：发送失败", "err", err)
	}
}

// ReportPath 把预览请求解析成一个确认落在绑定工作目录内的 .html 绝对
// 路径（httpapi 预览端点用）。护栏口径与 report 包的 resolveInCwd 一致：
// 软链先解析再比对，防止用工作目录里的软链把任意文件端出去。
func (s *Service) ReportPath(channelID, rel string) (string, error) {
	b, ok := s.store.config().binding(channelID)
	if ok && b.Workdir == "" {
		ok = false
	}
	if !ok {
		return "", fmt.Errorf("%w: 频道没有绑定", ErrNotFound)
	}
	if strings.TrimSpace(rel) == "" {
		return "", fmt.Errorf("%w: path 不能为空", ErrInvalid)
	}
	abs := rel
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(b.Workdir, rel)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("%w: 报告不存在", ErrNotFound)
	}
	root, err := filepath.EvalSymlinks(b.Workdir)
	if err != nil {
		return "", fmt.Errorf("解析工作目录: %w", err)
	}
	if resolved != root && !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: 路径在工作目录之外", ErrInvalid)
	}
	if !strings.EqualFold(filepath.Ext(resolved), ".html") {
		return "", fmt.Errorf("%w: 只能预览 .html 报告", ErrInvalid)
	}
	if info, err := os.Stat(resolved); err != nil || info.IsDir() {
		return "", fmt.Errorf("%w: 报告不存在", ErrNotFound)
	}
	return resolved, nil
}
