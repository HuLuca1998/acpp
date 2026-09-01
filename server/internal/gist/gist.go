// Package gist 把一份内容发布成 GitHub secret gist，并给出**渲染后**的
// 查看链接——discord 子区里 HTML 报告的默认交付形态。
//
// 为什么是外链：频道里的人多半不在本机局域网，指向后端的预览地址点不开；
// 长图能看不能存、不能交互、复制不了里面的一个数。secret gist 不出现在
// 个人主页，但**拿到链接的人都能访问**——这既是它的交付前提也是它的风险，
// 调用方有义务把这句话透给用户，并且让链接能被撤销。
//
// 渲染链接为什么不是 gist 自己的地址：gist 页面显示的是 HTML 源码，raw
// 地址回的是 text/plain（浏览器当纯文本渲染），githack 系 CDN 则会先插
// 一张「One more step」确认页（2026-08-31 真机实测：点链接看到的是一张
// 警告页，不是报告）。gistpreview 用 GitHub API 拉 gist 再就地渲染，一跳
// 直达，secret gist 匿名可读所以不需要任何凭证。
//
// 状态全部写在 gist 描述里（归属 + 到期时刻），本包不落盘：进程重启、
// 换台机器，链接照样列得出、撤得掉。
//
// 本包不 import 项目内其他包（叶子包，与 webshot 同性质）；gh 不可用时
// 返回错误，由调用方降级成别的交付形态。
package gist

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// viewBase 是渲染入口，gist id 直接跟在问号后面。
const viewBase = "https://gistpreview.github.io/?"

// contentLimit 是单份内容的上限，卡在 1MB 而不是平台的 10MB：超过 1MB
// 的文件 GitHub API 会**截断**（truncated=true），而渲染页正是从 API 取
// 内容的——发上去能成功，点开是半张报告。宁可让调用方降级发附件。
const contentLimit = 1 << 20

// gist 描述的格式：`[acpp] <标题> ch:<归属> expires:<RFC3339>`。它同时是
// 给人看的标题、给 List 认领的归属、给 Cleanup 判死的时刻——**改格式等于
// 认不出既有的 gist**，那些会永远留在账号里。
const (
	descPrefix = "[acpp] "
	ownerKey   = " ch:"
	expiresKey = " expires:"
)

// ghTimeout 是单次 gh 调用的上限。
const ghTimeout = 30 * time.Second

// ErrGone 表示这条链接本来就不在了（撤过、过期清扫过、手动删过）。调用方
// 该按「已经失效」处理而不是报错——用户点两次撤销按钮不该看到一句错误。
var ErrGone = errors.New("链接已经不在了")

// Input 是一次发布。TTL 为 0 表示不过期；Owner 是归属标记（discord 子区
// id），List 与自动清理按它认领。
type Input struct {
	Filename string
	Content  []byte
	Title    string
	Owner    string
	TTL      time.Duration
}

// Link 是一条已发布的链接。ViewURL 是渲染后的页面（给人点的那个），
// GistURL 是 gist 本体（看源码、下载）。
type Link struct {
	ID        string
	Title     string
	Owner     string
	ViewURL   string
	GistURL   string
	ExpiresAt time.Time
}

// Expired 报告这条链接是否已经过期（零值到期时刻 = 永不过期）。
func (l Link) Expired(now time.Time) bool {
	return !l.ExpiresAt.IsZero() && l.ExpiresAt.Before(now)
}

// Available 报告本机能不能发布：gh 装了且登录了。判定要真跑一次 auth
// status——只看可执行文件在不在，会让「装了没登录」一路走到发布才炸。
func Available(ctx context.Context) bool {
	if findGH() == "" {
		return false
	}
	_, err := run(ctx, nil, "auth", "status")
	return err == nil
}

// Publish 发布一份 secret gist。
func Publish(ctx context.Context, in Input) (Link, error) {
	name := strings.TrimSpace(in.Filename)
	if name == "" {
		return Link{}, fmt.Errorf("文件名不能为空")
	}
	if len(in.Content) == 0 {
		return Link{}, fmt.Errorf("内容是空的")
	}
	if len(in.Content) > contentLimit {
		return Link{}, fmt.Errorf("内容 %d KB，超过外链上限 1MB（再大 GitHub 会截断，链接打开是半张页面）", len(in.Content)>>10)
	}

	var expires time.Time
	if in.TTL > 0 {
		expires = time.Now().Add(in.TTL)
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = name
	}
	out, err := run(ctx, in.Content, "gist", "create", "--filename", name,
		"--desc", buildDesc(title, in.Owner, expires), "-")
	if err != nil {
		return Link{}, err
	}
	url := lastURL(string(out))
	if url == "" {
		return Link{}, fmt.Errorf("gh 没有回链接：%s", trim(string(out), 200))
	}
	id := url[strings.LastIndex(url, "/")+1:]
	return Link{ID: id, Title: title, Owner: in.Owner, ViewURL: viewBase + id,
		GistURL: url, ExpiresAt: expires}, nil
}

// List 列出本账号下由 acpp 发布、**尚未过期**的链接。owner 非空时只给它
// 名下的（一个子区看不到别的子区发过什么）。
func List(ctx context.Context, owner string) ([]Link, error) {
	all, err := listAll(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var out []Link
	for _, l := range all {
		if l.Expired(now) || (owner != "" && l.Owner != owner) {
			continue
		}
		out = append(out, l)
	}
	return out, nil
}

// Revoke 撤销一条链接：删掉 gist，外网立刻访问不到。
//
// 删之前**必须**确认这条 gist 是 acpp 发的（描述带前缀）——id 是模型给
// 的参数，而模型的参数会受对话内容影响；没有这道闸，一句「把 xxx 删掉」
// 就能删掉用户自己收藏的 gist。owner 非空时还要求归属相符：一个子区撤不
// 掉另一个子区发的链接。
func Revoke(ctx context.Context, id, owner string) (Link, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Link{}, fmt.Errorf("链接 id 不能为空")
	}
	// 允许直接把链接整条传进来，从里面抠 id——模型手上现成的就是那串 URL。
	if i := strings.LastIndexAny(id, "/?"); i >= 0 {
		id = id[i+1:]
	}
	out, err := run(ctx, nil, "api", "/gists/"+id, "--jq", ".description")
	if err != nil {
		return Link{}, fmt.Errorf("%w（%s）", ErrGone, id)
	}
	desc := strings.TrimSpace(string(out))
	if !strings.HasPrefix(desc, descPrefix) {
		return Link{}, fmt.Errorf("这条 gist 不是 acpp 发布的，不能从这里删")
	}
	link := parseDesc(id, desc)
	if owner != "" && link.Owner != "" && link.Owner != owner {
		return Link{}, fmt.Errorf("这条链接是别的对话发的，在这里撤不掉")
	}
	if _, err := run(ctx, nil, "api", "-X", "DELETE", "/gists/"+id); err != nil {
		return Link{}, err
	}
	return link, nil
}

// Cleanup 删掉本账号下所有已过期的 acpp 链接，返回删除条数。认领范围严格
// 限定在描述带前缀的那些——用户自己的 gist 一条都不能碰。
func Cleanup(ctx context.Context) (int, error) {
	all, err := listAll(ctx)
	if err != nil {
		return 0, err
	}
	now := time.Now()
	deleted := 0
	for _, l := range all {
		if !l.Expired(now) {
			continue
		}
		if _, err := run(ctx, nil, "api", "-X", "DELETE", "/gists/"+l.ID); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

// ParseTTL 解析有效期写法：`7d` / `12h` / `30m` / `never`。空串给默认值。
func ParseTTL(s string, def time.Duration) (time.Duration, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "":
		return def, nil
	case "never", "永久", "0":
		return 0, nil
	}
	if num, ok := strings.CutSuffix(s, "d"); ok {
		var days int
		if _, err := fmt.Sscanf(num, "%d", &days); err == nil && days > 0 {
			return time.Duration(days) * 24 * time.Hour, nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return d, nil
	}
	return 0, fmt.Errorf("认不出的有效期 %q（用 7d / 12h / never 这种写法）", s)
}

// listAll 列出账号下所有 acpp 发布的 gist（含已过期的）。
func listAll(ctx context.Context) ([]Link, error) {
	out, err := run(ctx, nil, "api", "/gists", "--paginate",
		"--jq", `.[] | "\(.id)\t\(.description)"`)
	if err != nil {
		return nil, err
	}
	var links []Link
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		id, desc, ok := strings.Cut(line, "\t")
		if !ok || !strings.HasPrefix(desc, descPrefix) {
			continue
		}
		links = append(links, parseDesc(id, desc))
	}
	return links, nil
}

// buildDesc 拼 gist 描述（格式见上面的常量注释）。
func buildDesc(title, owner string, expires time.Time) string {
	desc := descPrefix + trim(title, 120)
	if owner != "" {
		desc += ownerKey + owner
	}
	if !expires.IsZero() {
		desc += expiresKey + expires.UTC().Format(time.RFC3339)
	}
	return desc
}

// parseDesc 把描述解回一条 Link。认不出的字段留空，不报错——描述是人也能
// 编辑的东西，被改花了最多是「列不出归属」，不该让整条记录消失。
func parseDesc(id, desc string) Link {
	l := Link{ID: id, ViewURL: viewBase + id}
	rest := strings.TrimPrefix(desc, descPrefix)
	if raw, ok := field(desc, expiresKey); ok {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			l.ExpiresAt = t
		}
		rest, _, _ = strings.Cut(rest, expiresKey)
	}
	if raw, ok := field(desc, ownerKey); ok {
		l.Owner = raw
		rest, _, _ = strings.Cut(rest, ownerKey)
	}
	l.Title = strings.TrimSpace(rest)
	return l
}

// field 取描述里某个键后面那一段（到下一个空格为止）。
func field(desc, key string) (string, bool) {
	_, raw, ok := strings.Cut(desc, key)
	if !ok {
		return "", false
	}
	if i := strings.IndexAny(raw, " \t"); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.TrimSpace(raw)
	return raw, raw != ""
}

// lastURL 从 gh 的输出里挑最后一个 https 开头的词——`gist create` 会先打
// 一行进度再打链接，两者在 stdout/stderr 之间挪过窝，所以不认行只认词。
func lastURL(out string) string {
	var url string
	for _, f := range strings.Fields(out) {
		if strings.HasPrefix(f, "https://") {
			url = strings.TrimRight(f, ".,)")
		}
	}
	return url
}

// run 跑一条 gh 命令。stdin 非空时喂给它。错误翻译成能直接显示给用户的
// 话——「exit status 1」对着聊天窗口没有任何意义。
func run(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	bin := findGH()
	if bin == "" {
		return nil, fmt.Errorf("本机没有 gh CLI（brew install gh）")
	}
	cctx, cancel := context.WithTimeout(ctx, ghTimeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, bin, args...)
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		// 链接可能落在 stderr（gh 的进度与结果分流各版本不一），两边都看。
		return append(out, stderr.Bytes()...), nil
	}
	msg := trim(strings.TrimSpace(stderr.String()), 300)
	switch {
	case cctx.Err() != nil:
		return nil, fmt.Errorf("gh 超时（%s）", ghTimeout)
	case strings.Contains(msg, "auth login") || strings.Contains(msg, "authentication"):
		return nil, fmt.Errorf("gh 没有登录（gh auth login）")
	case strings.Contains(msg, "scope"):
		return nil, fmt.Errorf("gh 登录缺 gist 权限（gh auth refresh -s gist）")
	case msg != "":
		return nil, fmt.Errorf("gh: %s", msg)
	}
	return nil, fmt.Errorf("跑 gh 失败: %w", err)
}

// findGH 定位 gh 可执行文件。PATH 之外还要翻常见落点：打包成 .app 之后
// 进程继承的是 launchd 的精简 PATH，homebrew 的 bin 不在里面（webshot
// 找 Chrome 踩过同一个坑）。
func findGH() string {
	if p, err := exec.LookPath("gh"); err == nil {
		return p
	}
	for _, p := range []string{"/opt/homebrew/bin/gh", "/usr/local/bin/gh", "/usr/bin/gh"} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

func trim(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
