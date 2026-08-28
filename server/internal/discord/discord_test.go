package discord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 契约：简写按 GitHub https 解析，完整 URL 原样放行，危险与畸形输入拒收。
func TestResolveRepo(t *testing.T) {
	cases := []struct {
		in       string
		name     string
		cloneURL string
		wantErr  bool
	}{
		{in: "BDBGAME2024/pp-game", name: "BDBGAME2024/pp-game", cloneURL: "https://github.com/BDBGAME2024/pp-game.git"},
		{in: "  owner/repo.git ", name: "owner/repo", cloneURL: "https://github.com/owner/repo.git"},
		{in: "https://github.com/org/app.git", name: "org/app", cloneURL: "https://github.com/org/app.git"},
		{in: "git@github.com:org/app.git", name: "org/app", cloneURL: "git@github.com:org/app.git"},
		{in: "", wantErr: true},
		{in: "justaname", wantErr: true},
		{in: "file:///etc/passwd", wantErr: true},
		{in: "../escape/repo", wantErr: true},
		{in: "https://host/../..", wantErr: true},
	}
	for _, c := range cases {
		name, url, err := resolveRepo(c.in)
		if c.wantErr {
			if !errors.Is(err, ErrInvalid) {
				t.Errorf("resolveRepo(%q) err = %v, want ErrInvalid", c.in, err)
			}
			continue
		}
		if err != nil || name != c.name || url != c.cloneURL {
			t.Errorf("resolveRepo(%q) = (%q, %q, %v), want (%q, %q)", c.in, name, url, err, c.name, c.cloneURL)
		}
	}
}

// 契约：配置写盘后重新加载还原样，token 文件必须只有本人可读。
func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "discord.json")
	s1, err := newStore(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s1.update(func(c *Config) {
		c.Enabled = true
		c.BotToken = "abc.def.ghi"
		c.upsertBinding(Binding{ChannelID: "c1", Repo: "org/app", Workdir: "/tmp/x", Agent: "claude", Model: "m1"})
	})
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("配置文件权限 = %o, want 600", perm)
	}

	s2, err := newStore(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := s2.config()
	if !cfg.Enabled || cfg.BotToken != "abc.def.ghi" || len(cfg.Bindings) != 1 || cfg.Bindings[0].Repo != "org/app" {
		t.Errorf("重载配置不一致: %+v", cfg)
	}

	// 覆盖同频道绑定不产生第二条。
	if _, err := s2.update(func(c *Config) {
		c.upsertBinding(Binding{ChannelID: "c1", Repo: "org/other", Agent: "codex", Model: "m2"})
	}); err != nil {
		t.Fatal(err)
	}
	cfg = s2.config()
	if len(cfg.Bindings) != 1 || cfg.Bindings[0].Repo != "org/other" {
		t.Errorf("upsert 后绑定 = %+v", cfg.Bindings)
	}
	if _, err := s2.update(func(c *Config) {
		if !c.removeBinding("c1") {
			t.Error("removeBinding 没找到已存在的绑定")
		}
	}); err != nil {
		t.Fatal(err)
	}
	if got := len(s2.config().Bindings); got != 0 {
		t.Errorf("解绑后剩 %d 条", got)
	}
}

// 契约：SaveConfig 对 token 做形状检查，对工作根要求绝对路径；
// enabled 开关不动其他字段。
func TestSaveConfigValidation(t *testing.T) {
	svc, err := New(filepath.Join(t.TempDir(), "discord.json"), Deps{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	bad := "not-a-token"
	if _, err := svc.SaveConfig(ctx, ConfigPatch{BotToken: &bad}); !errors.Is(err, ErrInvalid) {
		t.Errorf("坏 token err = %v, want ErrInvalid", err)
	}
	rel := "relative/path"
	if _, err := svc.SaveConfig(ctx, ConfigPatch{WorkRoot: &rel}); !errors.Is(err, ErrInvalid) {
		t.Errorf("相对工作根 err = %v, want ErrInvalid", err)
	}

	tok := strings.Repeat("a", 30) + "." + strings.Repeat("b", 10) + "." + strings.Repeat("c", 30)
	on := true
	info, err := svc.SaveConfig(ctx, ConfigPatch{Enabled: &on, BotToken: &tok})
	if err != nil {
		t.Fatal(err)
	}
	if !info.Config.Enabled || !info.Config.TokenSet {
		t.Errorf("保存后视图 = %+v", info.Config)
	}
	// token 不回传，视图里只有 tokenSet。
	if strings.Contains(info.Config.WorkRoot, tok) {
		t.Error("token 泄漏进视图")
	}

	off := false
	info, err = svc.SaveConfig(ctx, ConfigPatch{Enabled: &off})
	if err != nil {
		t.Fatal(err)
	}
	if info.Config.Enabled || !info.Config.TokenSet {
		t.Errorf("关掉开关后 token 应保留: %+v", info.Config)
	}
}

// 契约：绑定编辑只动模型三件套，解绑不认识的频道报 ErrNotFound。
func TestBindingEdits(t *testing.T) {
	svc, err := New(filepath.Join(t.TempDir(), "discord.json"), Deps{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := svc.store.update(func(c *Config) {
		c.upsertBinding(Binding{ChannelID: "c1", Repo: "org/app", Workdir: "/tmp/x", Agent: "claude", Model: "m1"})
	}); err != nil {
		t.Fatal(err)
	}

	b, err := svc.UpdateBinding(ctx, "c1", BindingPatch{Agent: "codex", Model: "m2", ModelLabel: "codex · GPT", Effort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if b.Agent != "codex" || b.Model != "m2" || b.Effort != "high" || b.Repo != "org/app" {
		t.Errorf("编辑后绑定 = %+v", b)
	}
	if _, err := svc.UpdateBinding(ctx, "ghost", BindingPatch{Agent: "a", Model: "m"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("编辑不存在的频道 err = %v, want ErrNotFound", err)
	}
	if err := svc.RemoveBinding("ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("解绑不存在的频道 err = %v, want ErrNotFound", err)
	}
	if err := svc.RemoveBinding("c1"); err != nil {
		t.Fatal(err)
	}
}

// 契约：modal 提交的两种组件树形状（Label 包裹 / action row）都要解出答案。
func TestParseModalSubmit(t *testing.T) {
	labelStyle := json.RawMessage(`[
		{"type":18,"component":{"type":4,"custom_id":"repo","value":"org/app"}},
		{"type":18,"component":{"type":21,"custom_id":"model","values":["claude|m1"]}},
		{"type":18,"component":{"type":21,"custom_id":"effort","values":["default"]}}
	]`)
	got := parseModalSubmit(labelStyle)
	if got["repo"] != "org/app" || got["model"] != "claude|m1" || got["effort"] != "default" {
		t.Errorf("Label 形状解析 = %v", got)
	}

	rowStyle := json.RawMessage(`[
		{"type":1,"components":[{"type":4,"custom_id":"repo","value":"a/b"}]}
	]`)
	if got := parseModalSubmit(rowStyle); got["repo"] != "a/b" {
		t.Errorf("action row 形状解析 = %v", got)
	}
}

// 契约：已是 git 仓库的目录直接复用；存在但不是仓库的目录明确报错。
func TestEnsureWorkdirReuse(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "org", "app")
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	reused, err := ensureWorkdir(context.Background(), "https://example.com/x.git", "", dir)
	if err != nil || !reused {
		t.Errorf("已有克隆应复用: reused=%v err=%v", reused, err)
	}

	plain := filepath.Join(t.TempDir(), "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureWorkdir(context.Background(), "https://example.com/x.git", "", plain); err == nil {
		t.Error("非 git 目录应报错")
	}
}

// 契约：ls-remote --symref 输出要解出默认分支与全部分支；落点命名默认分支
// 用 `<组织>/<仓库>`，指定分支加 @ 后缀且斜杠转安全字符。
func TestParseLsRemoteAndWorkdirName(t *testing.T) {
	out := "ref: refs/heads/main\tHEAD\n" +
		"aaaa\tHEAD\n" +
		"aaaa\trefs/heads/main\n" +
		"bbbb\trefs/heads/feat/login\n" +
		"cccc\trefs/tags/v1.0\n"
	def, branches := parseLsRemote(out)
	if def != "main" {
		t.Errorf("默认分支 = %q, want main", def)
	}
	if len(branches) != 2 || branches[0] != "main" || branches[1] != "feat/login" {
		t.Errorf("分支清单 = %v", branches)
	}

	if got := workdirName("org/app", ""); got != "org/app" {
		t.Errorf("默认分支落点 = %q", got)
	}
	if got := workdirName("org/app", "feat/login"); got != "org/app@feat-login" {
		t.Errorf("分支落点 = %q", got)
	}
}

// 契约：模型选项 value 编码为 agent|modelID，超 25 截断；效率清单含默认档。
func TestModelChoices(t *testing.T) {
	catalog := []AgentOption{
		{Agent: "claude", Models: []ModelOption{{ID: "opus", Label: "Opus 5"}}},
		{Agent: "codex", Models: []ModelOption{{ID: "gpt", Label: "GPT-5"}}},
	}
	got := modelChoices(catalog)
	if len(got) != 2 || got[0].Value != "claude|opus" || got[0].Label != "claude · Opus 5" {
		t.Errorf("modelChoices = %+v", got)
	}

	var many []ModelOption
	for i := range 30 {
		many = append(many, ModelOption{ID: string(rune('a' + i)), Label: "m"})
	}
	if got := modelChoices([]AgentOption{{Agent: "x", Models: many}}); len(got) != 25 {
		t.Errorf("超限截断后 %d 项, want 25", len(got))
	}
}

// 契约：429 错误按响应体里的 retry_after 给出重试间隔，非 429 不重试，
// 解不出时长给保守值。
func TestRetryAfter(t *testing.T) {
	d, ok := retryAfter(errors.New(`PATCH /channels/1 → 429 Too Many Requests: {"message":"rate limited.","retry_after":295.292,"global":false}`))
	if !ok || d < 295*time.Second || d > 296*time.Second {
		t.Errorf("retryAfter = %v, %v", d, ok)
	}
	if _, ok := retryAfter(errors.New("PATCH /channels/1 → 500 oops")); ok {
		t.Error("非 429 不该重试")
	}
	if d, ok := retryAfter(errors.New("429 Too Many Requests: <html>")); !ok || d != 5*time.Minute {
		t.Errorf("解不出时长应给保守值: %v, %v", d, ok)
	}
}

// 契约：@bot 标记（两种写法）要摘干净；子区标题取首句且限长。
func TestStripMentionAndTitle(t *testing.T) {
	if got := stripMention("<@123> 帮我看看 <@!123> 这个", "123"); got != "帮我看看  这个" {
		t.Errorf("stripMention = %q", got)
	}
	if got := threadTitle("第一行问题\n第二行补充"); got != "第一行问题" {
		t.Errorf("threadTitle = %q", got)
	}
	long := strings.Repeat("问", 100)
	if got := threadTitle(long); len([]rune(got)) != 60 {
		t.Errorf("超长标题应截到 60，得 %d", len([]rune(got)))
	}
}

// 契约：长回复按行分段不超限；被切开的代码围栏每段补闭合、下段重开。
func TestSplitMessage(t *testing.T) {
	if got := splitMessage("短消息", 100); len(got) != 1 || got[0] != "短消息" {
		t.Errorf("短消息不该切: %v", got)
	}

	var b strings.Builder
	b.WriteString("说明\n```go\n")
	for i := range 50 {
		fmt.Fprintf(&b, "line%d := %d\n", i, i)
	}
	b.WriteString("```")
	segs := splitMessage(b.String(), 200)
	if len(segs) < 2 {
		t.Fatalf("应该切成多段, got %d", len(segs))
	}
	for i, seg := range segs {
		if len([]rune(seg)) > 210 {
			t.Errorf("第 %d 段超限: %d", i, len([]rune(seg)))
		}
		// 每段的围栏必须自洽（``` 出现偶数次）。
		if strings.Count(seg, "```")%2 != 0 {
			t.Errorf("第 %d 段围栏不闭合:\n%s", i, seg)
		}
	}
}

// 契约：requestedSchema 解析——codex 的 __other 标记与 claude 的 _custom
// 命名都归位成题目的自由输入栏，不算独立题目。
func TestParseElicitSchema(t *testing.T) {
	raw := json.RawMessage(`{"properties":{
		"color":{"title":"选个颜色","oneOf":[{"const":"red"},{"const":"blue"}]},
		"color_custom":{"title":"其他"},
		"__other":{"_meta":{"codex":{"isOtherAnswer":true,"questionId":"color"}}}
	},"required":["color"]}`)
	qs, err := parseElicitSchema(raw)
	if err != nil || len(qs) != 1 {
		t.Fatalf("题目数 = %d, err = %v", len(qs), err)
	}
	q := qs[0]
	if q.ID != "color" || len(q.Options) != 2 || !q.Required || q.OtherField == "" {
		t.Errorf("题目解析 = %+v", q)
	}
}

// 契约：逐题卡——标题带进度（k/M），已答的题累积进正文；≤5 个选项给
// 按钮行，带自由输入的题附「✍️ 输入」按钮，>5 个选项换下拉。
func TestQuestionCard(t *testing.T) {
	ask := &pendingAsk{
		nonce: "n", partial: map[string]string{},
		questions: []elicitQuestion{
			{ID: "q0", Title: "第一题", Options: []string{"a", "b"}, OtherField: "q0_custom"},
			{ID: "q1", Title: "第二题"},
		},
	}
	embed := questionCardEmbed(ask)
	if !strings.Contains(embed["title"].(string), "1/2") {
		t.Errorf("标题缺进度: %v", embed["title"])
	}
	rows := questionCardComponents(ask)
	// 选项按钮一行 + ✍️ 输入一行。
	if len(rows) != 2 {
		t.Fatalf("组件行数 = %d, want 2", len(rows))
	}
	buttons := rows[0]["components"].([]map[string]any)
	if len(buttons) != 2 || buttons[0]["custom_id"] != "ea:n:0" {
		t.Errorf("选项按钮 = %+v", buttons)
	}

	// 答完第一题：进度推进，正文里带已答记录；纯输入题只有 ✍️ 行。
	ask.partial["q0"] = "a"
	ask.cursor = 1
	embed = questionCardEmbed(ask)
	desc := embed["description"].(string)
	if !strings.Contains(embed["title"].(string), "2/2") || !strings.Contains(desc, "第一题") || !strings.Contains(desc, "a") {
		t.Errorf("翻题后卡片 = %v / %s", embed["title"], desc)
	}
	rows = questionCardComponents(ask)
	if len(rows) != 1 || rows[0]["components"].([]map[string]any)[0]["custom_id"] != "ei:n" {
		t.Errorf("纯输入题组件 = %+v", rows)
	}

	// >5 个选项换下拉。
	many := &pendingAsk{nonce: "m", partial: map[string]string{}, questions: []elicitQuestion{{
		ID: "q", Title: "多选项", Options: []string{"1", "2", "3", "4", "5", "6"},
	}}}
	rows = questionCardComponents(many)
	if rows[0]["components"].([]map[string]any)[0]["custom_id"] != "es:m" {
		t.Errorf("多选项应为下拉: %+v", rows)
	}
}
