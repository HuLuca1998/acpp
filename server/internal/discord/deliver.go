package discord

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"acpp/server/internal/gist"
	"acpp/server/internal/webshot"
)

// 交付面：把成果从「磁盘上的路径」变成「用户手上的东西」。三种形态各有
// 各的不可替代性，谁也代不了谁：
//
//	file   附件上传 —— 能存档、能转发、能再编辑。用户开口要「文件」时要的是它
//	link   外链    —— 渲染后的页面，手机上点开即看，能选中文字、能交互
//	image  整页长图 —— 不出网、不依赖第三方，但只能看，复制不了里面的一个数
//
// 默认（auto）：.html 走 link，其余走 file。理由是 HTML 附件在手机上点开
// 多半是一屏源码，而报告的价值全在渲染后的样子；而一个 .png/.csv/.zip 本
// 来就是拿来存的，绕一趟外链毫无意义。
//
// 外链是 secret gist（internal/gist）：不公开列出，但**拿到链接的人都能
// 打开**，所以三件事必须齐：默认带有效期、卡片上明说这一点、随时能撤销。
//
// 报告（report_open）的落地在本文件末尾——它是交付的一种，不是另一件事。

const (
	deliverAuto  = "auto"
	deliverFile  = "file"
	deliverLink  = "link"
	deliverImage = "image"
)

// linkTTLDefault 是外链的默认有效期。7 天是「看完还能回头翻一次」与
// 「别让内容无限期挂在外网」之间的折中；agent 可按需改，用户说了算。
const linkTTLDefault = 7 * 24 * time.Hour

// linkCleanupEvery 是过期链接的清扫间隔。链接过期后**只是到点该删**，
// 真正断开访问要靠这一趟——所以间隔不能太长。
const linkCleanupEvery = 30 * time.Minute

// sendMaxFiles 是一次调用能交付的文件数上限（Discord 单条消息 10 个附件，
// 发送时还会按体积再分批）。
const sendMaxFiles = 10

// delivery 是一个文件的交付结果：要么变成附件，要么变成一条外链。
type delivery struct {
	name string
	// files 是要上传的附件（长图 + 原文件都可能在里面）。
	files []outFile
	link  *gist.Link
	// note 是降级说明（比如「渲染不了，改发原文件」），进回执给模型看。
	note string
}

// deliverFiles 把一批路径按指定形态交付进子区。单个文件失败不拖累别的：
// 发得出去的先发出去，失败的在回执里说清楚。
func (s *Service) deliverFiles(ctx context.Context, threadID, cwd string,
	rels []string, caption, mode, expire string) (string, error) {
	if len(rels) == 0 {
		return "", fmt.Errorf("paths 不能为空")
	}
	if len(rels) > sendMaxFiles {
		return "", fmt.Errorf("一次最多发 %d 个文件，这次传了 %d 个", sendMaxFiles, len(rels))
	}
	mode = strings.ToLower(strings.TrimSpace(orDefault(mode, deliverAuto)))
	switch mode {
	case deliverAuto, deliverFile, deliverLink, deliverImage:
	default:
		return "", fmt.Errorf("认不出的 as=%q（只能是 auto / file / link / image）", mode)
	}
	ttl, err := gist.ParseTTL(expire, linkTTLDefault)
	if err != nil {
		return "", err
	}

	var attachments []outFile
	var links []gist.Link
	var done, failed, notes []string
	for _, rel := range rels {
		d, err := s.deliverOne(ctx, threadID, cwd, rel, mode, ttl)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s（%s）", rel, err.Error()))
			continue
		}
		attachments = append(attachments, d.files...)
		if d.link != nil {
			links = append(links, *d.link)
		}
		done = append(done, d.name)
		if d.note != "" {
			notes = append(notes, d.name+"："+d.note)
		}
	}
	if len(attachments) == 0 && len(links) == 0 {
		return "", fmt.Errorf("一个都没交付出去：%s", strings.Join(failed, "；"))
	}

	token := s.store.config().BotToken
	if len(attachments) > 0 {
		content := "-# 📎 " + trimRunes(orDefault(caption, strings.Join(done, "、")), 150)
		if err := s.postAttachments(ctx, token, threadID, content, attachments); err != nil {
			return "", fmt.Errorf("发送失败：%w", err)
		}
	}
	for _, l := range links {
		s.postLinkCard(ctx, token, threadID, l, caption)
	}
	return deliverReceipt(done, links, notes, failed), nil
}

// deliverReceipt 拼给模型的回执。链接的 id 必须原样给出去——用户回头说
// 「删了吧」时，模型手上得有东西可传给 revoke_link。
func deliverReceipt(done []string, links []gist.Link, notes, failed []string) string {
	var w strings.Builder
	fmt.Fprintf(&w, "已交付：%s。", strings.Join(done, "、"))
	for _, l := range links {
		fmt.Fprintf(&w, "\n外链 %s → %s（id %s", l.Title, l.ViewURL, l.ID)
		if l.ExpiresAt.IsZero() {
			w.WriteString("，不过期")
		} else {
			fmt.Fprintf(&w, "，%s 到期", l.ExpiresAt.Local().Format("01-02 15:04"))
		}
		w.WriteString("）")
	}
	if len(links) > 0 {
		w.WriteString("\n用户说看完了 / 不用了，就用 revoke_link 撤掉（外网立刻访问不到）。")
	}
	if len(notes) > 0 {
		w.WriteString("\n降级：" + strings.Join(notes, "；"))
	}
	if len(failed) > 0 {
		w.WriteString("\n没发出去：" + strings.Join(failed, "；"))
	}
	return w.String()
}

// deliverOne 按形态交付一个文件。auto 在这里落成具体形态。
func (s *Service) deliverOne(ctx context.Context, threadID, cwd, rel, mode string,
	ttl time.Duration) (delivery, error) {
	abs, err := resolveInWorkdir(cwd, rel)
	if err != nil {
		return delivery{}, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return delivery{}, fmt.Errorf("读文件失败")
	}
	name := filepath.Base(abs)
	isHTML := strings.EqualFold(filepath.Ext(abs), ".html")
	if mode == deliverAuto {
		if isHTML {
			mode = deliverLink
		} else {
			mode = deliverFile
		}
	}

	switch mode {
	case deliverLink:
		d, err := publishLink(ctx, threadID, name, data, isHTML, ttl)
		if err == nil {
			return d, nil
		}
		// 外链发不出去（没 gh、没登录、内容太大）不能就此空手——用户要的是
		// 东西本身，降级成附件至少东西到手了。
		att, ferr := attachDelivery(name, data)
		if ferr != nil {
			return delivery{}, err
		}
		att.note = "外链发布失败（" + err.Error() + "），改发原文件"
		return att, nil
	case deliverImage:
		if !isHTML {
			return delivery{}, fmt.Errorf("只有 .html 能渲染成长图")
		}
		png, err := renderPNG(ctx, abs)
		if err != nil {
			return delivery{}, err
		}
		return delivery{name: name, files: []outFile{{name: pngName(name), data: png}}}, nil
	default:
		return attachDelivery(name, data)
	}
}

// publishLink 把内容发布成外链。HTML 给渲染页，其余给 gist 页——.md 与
// 代码在 gist 页上本来就有渲染与高亮，套一层 HTML 预览反而不对。
func publishLink(ctx context.Context, threadID, name string, data []byte,
	isHTML bool, ttl time.Duration) (delivery, error) {
	if !isTextAttachment("", data) {
		return delivery{}, fmt.Errorf("二进制文件发不了外链（gist 只收文本），用 as=file 直接发文件")
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	link, err := gist.Publish(cctx, gist.Input{
		Filename: name, Content: data, Title: name, Owner: threadID, TTL: ttl,
	})
	if err != nil {
		return delivery{}, err
	}
	if !isHTML {
		link.ViewURL = link.GistURL
	}
	return delivery{name: name, link: &link}, nil
}

func attachDelivery(name string, data []byte) (delivery, error) {
	if len(data) > sendAttachMax {
		return delivery{}, fmt.Errorf("%d MB，超过附件上限 24MB", len(data)>>20)
	}
	return delivery{name: name, files: []outFile{{name: name, data: data}}}, nil
}

func renderPNG(ctx context.Context, abs string) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	png, err := webshot.Capture(cctx, "file://"+abs, 1100)
	if err != nil {
		return nil, fmt.Errorf("渲染长图失败（本机需要 Chrome）")
	}
	if len(png) > sendAttachMax {
		return nil, fmt.Errorf("长图 %d MB，超过附件上限", len(png)>>20)
	}
	return png, nil
}

func pngName(name string) string {
	return strings.TrimSuffix(name, filepath.Ext(name)) + ".png"
}

// linkCard 拼外链卡：标题 + 「打开」按钮 + 小字（有效期与「拿到链接的人
// 都能看」）+ 分隔线下的「立即失效」。
//
// 两个按钮**刻意不放同一行**：并排时手机上一指宽的距离就能把「打开」点成
// 「失效」，而失效不可逆——链接一撤，已经发给别人的那条就永久打不开了。
// 所以失效按钮沉到分隔线以下，再加一道二次确认（见 revokeClicked）。
func linkCard(title string, l gist.Link) []map[string]any {
	inner := []map[string]any{
		v2Section("### 🔗 "+trimRunes(title, 120), v2LinkButton("打开", l.ViewURL)),
		v2Text("-# " + linkFootnote(l)),
		v2Sep(),
		v2Row(v2DangerButton("让这条链接立即失效", revokePrefix+l.ID)),
	}
	return v2Container(colorGreen, inner)
}

// revokedCard 是撤销后的终态卡：链接没了，卡上就不该再留按钮。
func revokedCard(title, by string) []map[string]any {
	return v2Container(colorGrey, []map[string]any{
		v2Text("### 🔒 链接已失效"),
		v2Text("-# 《" + trimRunes(title, 100) + "》· 外网不再能打开" + by),
	})
}

// postLinkCard 把一条外链发成卡片。
func (s *Service) postLinkCard(ctx context.Context, token, threadID string, l gist.Link, caption string) {
	title := orDefault(caption, l.Title)
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	err := botREST(cctx, token, "POST", "/channels/"+threadID+"/messages", map[string]any{
		"flags": 1 << 15, "components": linkCard(title, l),
		"allowed_mentions": noMentions(),
	}, nil)
	if err != nil {
		slog.Warn("外链卡发送失败", "err", err)
	}
}

// linkFootnote 是链接卡的小字：什么时候失效、谁能看见。
func linkFootnote(l gist.Link) string {
	life := "不会自动失效"
	if !l.ExpiresAt.IsZero() {
		life = l.ExpiresAt.Local().Format("01-02 15:04") + " 失效"
	}
	return life + " · 不公开列出，但拿到链接的人都能打开 · 说一声可随时撤销"
}

// listLinks 列出本子区还有效的外链（给模型看的清单，撤销要用里面的 id）。
func (s *Service) listLinks(ctx context.Context, threadID string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	links, err := gist.List(cctx, threadID)
	if err != nil {
		return "", err
	}
	if len(links) == 0 {
		return "这个对话目前没有还有效的外链。", nil
	}
	var w strings.Builder
	fmt.Fprintf(&w, "本对话还有 %d 条有效外链：", len(links))
	for _, l := range links {
		fmt.Fprintf(&w, "\n- %s | id %s | %s | %s", l.Title, l.ID, l.ViewURL, linkLife(l))
	}
	return w.String(), nil
}

func linkLife(l gist.Link) string {
	if l.ExpiresAt.IsZero() {
		return "不过期"
	}
	return l.ExpiresAt.Local().Format("01-02 15:04") + " 到期"
}

// revokeLinks 撤销外链：删掉 gist，外网立刻访问不到。target 传 all 就撤
// 本子区全部。
func (s *Service) revokeLinks(ctx context.Context, threadID, target string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("要撤销哪条？传 link 的 id，或 all 撤销本对话全部")
	}
	if !strings.EqualFold(target, "all") {
		l, err := gist.Revoke(cctx, target, threadID)
		if err != nil {
			return "", err
		}
		s.postPlain(cctx, threadID, "-# 🔒 外链《"+trimRunes(l.Title, 80)+"》已撤销，外网不再能打开")
		return fmt.Sprintf("已撤销外链《%s》，链接立刻失效。", l.Title), nil
	}

	links, err := gist.List(cctx, threadID)
	if err != nil {
		return "", err
	}
	if len(links) == 0 {
		return "这个对话没有还有效的外链，不用撤。", nil
	}
	var revoked, failed []string
	for _, l := range links {
		if _, err := gist.Revoke(cctx, l.ID, threadID); err != nil {
			failed = append(failed, l.Title)
			continue
		}
		revoked = append(revoked, l.Title)
	}
	if len(revoked) > 0 {
		s.postPlain(cctx, threadID, fmt.Sprintf("-# 🔒 已撤销 %d 条外链，外网不再能打开", len(revoked)))
	}
	msg := fmt.Sprintf("已撤销 %d 条外链：%s。", len(revoked), strings.Join(revoked, "、"))
	if len(failed) > 0 {
		msg += " 撤不掉的：" + strings.Join(failed, "、")
	}
	return msg, nil
}

// postPlain 往子区发一条纯文本（撤销留痕这类）。发不出去只记日志——痕迹
// 没留下不该让工具调用报错。
func (s *Service) postPlain(ctx context.Context, threadID, content string) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := botREST(cctx, s.store.config().BotToken, "POST", "/channels/"+threadID+"/messages",
		map[string]any{"content": content, "allowed_mentions": noMentions()}, nil)
	if err != nil {
		slog.Warn("撤销留痕发送失败", "err", err)
	}
}

// startLinkCleanup 起一趟定时清扫：删掉所有过期的 acpp 外链。链接的有效期
// 是承诺，到点没人删就等于没有有效期。gh 不可用时静默跳过（这台机器本来
// 也发不出外链）。
func (s *Service) startLinkCleanup(ctx context.Context) {
	go func() {
		t := time.NewTicker(linkCleanupEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				n, err := gist.Cleanup(ctx)
				if err != nil {
					slog.Debug("过期外链清扫失败", "err", err)
					continue
				}
				if n > 0 {
					slog.Info("过期外链已清扫", "count", n)
				}
			}
		}
	}()
}

// ---- 报告的子区落地（report_open 的回调）----

// 报告面：agent 在子区会话里调 report_open 后，把「报告出炉了」这件事
// 变成子区里的一张卡。网页会话是就地弹预览面板；discord 没有面板，卡上
// 给一个**渲染后的外链**——点开就是排好版的报告，手机上能读、能选中文字、
// 能把折叠块展开。
//
// 为什么不是长图：长图能看不能用，复制不了里面的一个数，交互全废，而报告
// 的价值恰恰在结构。为什么不是原文件：HTML 附件在手机上点开多半是一屏源码
// （用户拍板：报表直接给链接）。长图与原文件退居降级位——gh 不可用、内容
// 超过外链上限时才上场，那时候「有东西」比「好看」重要。
//
// 外链带默认 7 天有效期，并且归属登记成本子区：用户看完说一句「删了吧」，
// agent 用 revoke_link 就能收回（见 deliver.go）。

// reportOpened 是 report_open 的回调（经 Deps.Mounts 注册）：往子区发卡，
// 然后异步把报告发布成外链、把卡换成带「打开报告」按钮的终态。
func (s *Service) reportOpened(token, threadID string, b Binding, rel, title string) {
	cardID := s.postCard(token, threadID, map[string]any{
		"flags": 1 << 15, "components": reportCard(title, rel, "生成链接中…", nil),
	})
	go s.postReport(token, threadID, b, rel, title, cardID)
}

// reportCard 拼报告卡。link 非空时挂一个「打开报告」按钮，并把有效期与
// 「拿到链接的人都能打开」写进小字；status 是过程/失败说明，终态清掉——
// 卡上停着一句过期的「生成中」比没有更糟。
func reportCard(title, rel, status string, link *gist.Link) []map[string]any {
	head := "### 📊 报告《" + trimRunes(title, 100) + "》"
	var inner []map[string]any
	if link != nil {
		inner = append(inner, v2Section(head, v2LinkButton("打开报告", link.ViewURL)))
	} else {
		inner = append(inner, v2Text(head))
	}
	line := "-# " + trimRunes(rel, 200)
	switch {
	case status != "":
		line += " · " + status
	case link != nil:
		line += " · " + linkFootnote(*link)
	}
	inner = append(inner, v2Text(line))
	if link != nil {
		// 失效按钮与「打开报告」隔一条线，理由见 linkCard。
		inner = append(inner, v2Sep(), v2Row(v2DangerButton("让这条链接立即失效", revokePrefix+link.ID)))
	}
	return v2Container(colorGreen, inner)
}

// patchReportCard 更新报告卡（尽力而为）。
func (s *Service) patchReportCard(ctx context.Context, token, threadID, cardID, title, rel, status string, link *gist.Link) {
	if cardID == "" {
		return
	}
	err := botREST(ctx, token, "PATCH", "/channels/"+threadID+"/messages/"+cardID, map[string]any{
		"flags": 1 << 15, "components": reportCard(title, rel, status, link),
	}, nil)
	if err != nil {
		slog.Warn("报告卡状态更新失败", "err", err)
	}
}

// postReport 把报告交付进子区：先试外链，不成再退回原文件 + 长图。
func (s *Service) postReport(token, threadID string, b Binding, rel, title, cardID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	abs, err := s.ReportPath(b.ChannelID, rel)
	if err != nil {
		slog.Warn("报告交付：路径解析失败", "rel", rel, "err", err)
		s.patchReportCard(ctx, token, threadID, cardID, title, rel, "报告文件找不到了", nil)
		return
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		slog.Warn("报告交付：读文件失败", "rel", rel, "err", err)
		s.patchReportCard(ctx, token, threadID, cardID, title, rel, "报告读不出来", nil)
		return
	}

	link, err := gist.Publish(ctx, gist.Input{
		Filename: filepath.Base(abs), Content: data, Title: title,
		Owner: threadID, TTL: linkTTLDefault,
	})
	if err == nil {
		s.patchReportCard(ctx, token, threadID, cardID, title, rel, "", &link)
		return
	}
	slog.Warn("报告交付：外链发布失败，降级发文件", "rel", rel, "err", err)
	s.postReportFallback(ctx, token, threadID, cardID, abs, data, title, rel, err)
}

// postReportFallback 是外链走不通时的退路：原文件 + 整页长图一起发进子区。
// 长图渲染要靠本机 Chrome，失败也不拦——原文件是一定拿得到的那份。
func (s *Service) postReportFallback(ctx context.Context, token, threadID, cardID string,
	abs string, data []byte, title, rel string, cause error) {
	base := filepath.Base(abs)
	budget := sendAttachMax
	var files []outFile
	notes := []string{"外链发不出去（" + trimRunes(cause.Error(), 60) + "），改发文件"}

	if len(data) > budget {
		notes = append(notes, "原文件太大发不了")
	} else {
		files = append(files, outFile{name: base, data: data})
		budget -= len(data)
	}
	if png, err := renderPNG(ctx, abs); err != nil {
		notes = append(notes, "长图也没生成（本机需要 Chrome）")
	} else if len(png) <= budget {
		// 长图排前面：Discord 只把第一个附件渲染成预览大图。
		files = append([]outFile{{name: pngName(base), data: png}}, files...)
	}

	if len(files) == 0 {
		s.patchReportCard(ctx, token, threadID, cardID, title, rel, strings.Join(notes, " · "), nil)
		return
	}
	err := botRESTFiles(ctx, token, threadID, map[string]any{
		"content":          "-# 📊 《" + trimRunes(title, 80) + "》",
		"allowed_mentions": noMentions(),
	}, files)
	if err != nil {
		slog.Warn("报告交付：附件发送失败", "err", err)
		notes = append(notes, "附件也没发出去")
	}
	s.patchReportCard(ctx, token, threadID, cardID, title, rel, strings.Join(notes, " · "), nil)
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
