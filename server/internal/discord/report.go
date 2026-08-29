package discord

import (
	"context"
	"fmt"
	"log/slog"
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
// 然后异步把报告渲染成整页长图直接发进子区。不放预览链接——链接指向
// 本机后端，频道里的用户多半不在同一局域网，点不开的按钮比没有更糟
// （用户拍板：直接给图片就行）。
func (s *Service) reportOpened(token, threadID string, b Binding, rel, title string) {
	cardID := s.postCard(token, threadID, map[string]any{
		"flags": 1 << 15, "components": reportCard(title, rel, "长图生成中…"),
	})
	go s.postReportImage(token, threadID, b, rel, title, cardID)
}

// reportCard 拼报告卡。status 非空时跟在路径后面（生成中/失败提示），
// 长图发出后清掉——卡上停着一句过期的「生成中」比没有更糟。
func reportCard(title, rel, status string) []map[string]any {
	line := "-# " + trimRunes(rel, 200)
	if status != "" {
		line += " · " + status
	}
	return v2Container(colorGreen, []map[string]any{
		v2Text("### 📊 报告《" + trimRunes(title, 100) + "》"),
		v2Text(line),
	})
}

// patchReportCard 更新报告卡的状态行（尽力而为）。
func (s *Service) patchReportCard(ctx context.Context, token, threadID, cardID, title, rel, status string) {
	if cardID == "" {
		return
	}
	err := botREST(ctx, token, "PATCH", "/channels/"+threadID+"/messages/"+cardID, map[string]any{
		"flags": 1 << 15, "components": reportCard(title, rel, status),
	}, nil)
	if err != nil {
		slog.Warn("报告卡状态更新失败", "err", err)
	}
}

// reportImageMax 是长图直发的体积上限（Discord 免费档附件 25MB，留余量）。
const reportImageMax = 24 << 20

// postReportImage 把报告渲染成整页 PNG 发进子区（带归属小字，免得图
// 和报告卡之间插了别的消息后看不出是谁的）。
func (s *Service) postReportImage(token, threadID string, b Binding, rel, title, cardID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	abs, err := s.ReportPath(b.ChannelID, rel)
	if err != nil {
		slog.Warn("报告长图：路径解析失败", "rel", rel, "err", err)
		s.patchReportCard(ctx, token, threadID, cardID, title, rel, "长图生成失败")
		return
	}
	png, err := webshot.Capture(ctx, "file://"+abs, 1100)
	if err != nil {
		slog.Warn("报告长图：渲染失败", "rel", rel, "err", err)
		s.patchReportCard(ctx, token, threadID, cardID, title, rel, "长图生成失败（本机需要 Chrome）")
		return
	}
	if len(png) > reportImageMax {
		slog.Warn("报告长图：超出附件上限，不发", "bytes", len(png))
		s.patchReportCard(ctx, token, threadID, cardID, title, rel, "报告太大，长图放不下")
		return
	}
	err = botRESTFile(ctx, token, threadID, map[string]any{
		"content":          "-# 📊 《" + trimRunes(title, 80) + "》",
		"attachments":      []map[string]any{{"id": 0, "filename": "report.png"}},
		"allowed_mentions": noMentions(),
	}, "report.png", png)
	if err != nil {
		slog.Warn("报告长图：发送失败", "err", err)
		s.patchReportCard(ctx, token, threadID, cardID, title, rel, "长图发送失败")
		return
	}
	s.patchReportCard(ctx, token, threadID, cardID, title, rel, "")
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
