package discord

import (
	"acpp/server/internal/acp"

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

// 契约：modal 提交的两种组件树形状（Label 包裹 / action row）都要解出
// 答案；多选组件一名多值。

func TestParseModalSubmit(t *testing.T) {
	labelStyle := json.RawMessage(`[
		{"type":18,"component":{"type":4,"custom_id":"repo","value":"org/app"}},
		{"type":18,"component":{"type":21,"custom_id":"model","values":["claude|m1"]}},
		{"type":18,"component":{"type":22,"custom_id":"fruits","values":["Apple","Cherry"]}}
	]`)
	got := parseModalSubmit(labelStyle)
	if firstAnswer(got, "repo") != "org/app" || firstAnswer(got, "model") != "claude|m1" {
		t.Errorf("Label 形状解析 = %v", got)
	}
	if f := got["fruits"]; len(f) != 2 || f[0] != "Apple" || f[1] != "Cherry" {
		t.Errorf("多选提交 = %v", f)
	}

	rowStyle := json.RawMessage(`[
		{"type":1,"components":[{"type":4,"custom_id":"repo","value":"a/b"}]}
	]`)
	if got := parseModalSubmit(rowStyle); firstAnswer(got, "repo") != "a/b" {
		t.Errorf("action row 形状解析 = %v", got)
	}
}

// 契约：一个仓库只克隆一份 bare git 数据（.repo），每个分支一棵
// .worktree/<分支> 工作树；同一分支再绑一次复用同一棵树，不同分支的树
// 内容互不串（这正是「在 prod 频道问却答 live 分支代码」的根治点）。

// seedRepo 造一个带两个分支的真仓库，每个分支的 who.txt 写着自己的分支名
// （用来验证两棵树的内容不串）。返回可当 clone 源用的路径。

// gitRun 在 dir 里跑一条 git（测试用，作者身份写死，免得依赖机器配置）。

// 契约：/git 要如实分出改了什么、加了什么、删了什么，并报出当前分支。
// 这是用户在频道里判断「agent 到底动了哪些文件」的唯一入口，分错类比不报
// 更糟。

// 契约：porcelain 的分支行要解出分支名与领先/落后的提交数——两边都要
// 显示，用户才知道该 pull 还是该 push。

// 契约：分支名要压成安全的单段目录名——斜杠转字符、`..` 不许穿越出去。

// 契约：ls-remote --symref 输出要解出默认分支与全部分支。

// 契约：频道主题是频道侧唯一的常驻信息面，必须整条塞得进平台上限——
// 超了平台会从末尾截，先没的就是手册。极端长的仓库名/分支/目录都要能扛住，
// 靠掐短目录换手册完整。
func TestTopicLineFits(t *testing.T) {
	long := strings.Repeat("very-long-segment/", 12)
	b := Binding{
		Repo:   "ORG-WITH-A-REALLY-LONG-NAME/" + long + "repo",
		Branch: "discord/" + long + "channel",
		Base:   "release/" + long,
		Workdir: "/Users/someone/acpp/discord/ORG-WITH-A-REALLY-LONG-NAME/" +
			long + "repo/.worktree/discord-" + long + "channel",
		Agent: "claude", Model: "claude-opus-4-6-20260514[1m]",
		ModelLabel: "claude · Opus 4.6 (1m context window, extended)",
		Effort:     "xhigh", Access: "auto-edit",
		DataSourceID: 29, DataSourceRef: "some-long-project/production-environment",
	}
	topic := topicLine(b)
	if n := len([]rune(topic)); n > topicLimit {
		t.Errorf("主题 %d 字符，超了上限 %d", n, topicLimit)
	}
	for _, want := range []string{"项目", "仓库：", "分支：", "目录：", "模型：", "数据库：", "discord/", "production-environment"} {
		if !strings.Contains(topic, want) {
			t.Errorf("主题缺少 %q", want)
		}
	}
	// 主题不渲染 markdown，写了格式符号只会原样显示出来。
	if strings.ContainsAny(topic, "`*") {
		t.Error("主题里不该出现 markdown 格式符号")
	}

	// 常规长度的绑定应当留足余量，否则改一句文案就会撞上限。
	normal := Binding{
		Repo: "BDBGAME2024/pp-game", Branch: "discord/pp-prod", Base: "live",
		Workdir: "/Users/luca/acpp/discord/BDBGAME2024/pp-game/.worktree/discord-pp-prod",
		Agent:   "claude", ModelLabel: "claude · Default", Effort: "high", Access: "auto-edit",
		DataSourceID: 29, DataSourceRef: "pp-game/prod",
	}
	if n := len([]rune(topicLine(normal))); n > topicLimit/2 {
		t.Errorf("常规主题 %d 字符，余量不足（上限 %d）", n, topicLimit)
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

// 契约：一次性表单——每题一个组件（多选 CheckboxGroup、单选 RadioGroup、
// 纯输入 TextInput），空位分给「其他」输入框；≤5 题一页装下。

func TestFormModal(t *testing.T) {
	ask := &pendingAsk{
		nonce: "n", partial: map[string][]string{},
		questions: []elicitQuestion{
			{ID: "q0", Title: "单选", Options: []elicitOption{{Const: "a"}, {Const: "b"}}, OtherField: "q0_custom"},
			{ID: "q1", Title: "多选", Multiple: true, Options: []elicitOption{{Const: "x"}, {Const: "y"}}},
			{ID: "q2", Title: "输入"},
		},
	}
	if formPages(ask.questions) != 1 {
		t.Fatalf("3 题应一页装下")
	}
	m := formModal(ask, 0)
	if m["custom_id"] != "em:n:0" {
		t.Errorf("custom_id = %v", m["custom_id"])
	}
	comps := m["components"].([]map[string]any)
	// 3 题 + 1 个「其他」输入框（q0 带 OtherField，空位够）。
	if len(comps) != 4 {
		t.Fatalf("组件数 = %d, want 4", len(comps))
	}
	kind := func(i int) any { return comps[i]["component"].(map[string]any)["type"] }
	// 「其他」输入框紧跟所属题：q0(单选) → q0 其他 → q1(多选) → q2(输入)。
	if kind(0) != 21 || kind(2) != 22 || kind(3) != 4 {
		t.Errorf("组件类型 = %v %v %v, want 21/22/4", kind(0), kind(2), kind(3))
	}
	other := comps[1]["component"].(map[string]any)
	if other["custom_id"] != "q0_custom" {
		t.Errorf("其他输入框应紧跟第一题 = %+v", other)
	}

	// 入口卡：Container + 填表按钮。
	intro := askIntroCard(ask)
	if intro[0]["type"] != 17 {
		t.Errorf("入口卡顶层 = %+v", intro[0])
	}

	// 6 题分两页。
	var many []elicitQuestion
	for i := range 6 {
		many = append(many, elicitQuestion{ID: fmt.Sprintf("m%d", i), Title: "题"})
	}
	if formPages(many) != 2 {
		t.Errorf("6 题应分 2 页")
	}
	if got := len(formModal(&pendingAsk{nonce: "x", questions: many}, 1)["components"].([]map[string]any)); got != 1 {
		t.Errorf("第 2 页组件数 = %d, want 1", got)
	}
}

// 契约：多选题解析与回传——schema 形状取自 2026-08 真实转录（claude 的
// AskUserQuestion：array + items.anyOf + _askUserQuestionCustomAnswer）。

func TestMultiSelectFlow(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{
		"question_0":{"type":"string","title":"Color","oneOf":[{"const":"Red"},{"const":"Blue"}]},
		"question_0_custom":{"type":"string","title":"Other","_meta":{"_askUserQuestionCustomAnswer":{"questionId":"question_0","isCustomAnswer":true}}},
		"question_1":{"type":"array","title":"Fruits","items":{"anyOf":[{"const":"Apple"},{"const":"Banana"},{"const":"Cherry"}]}},
		"question_1_custom":{"type":"string","title":"Other","_meta":{"_askUserQuestionCustomAnswer":{"questionId":"question_1","isCustomAnswer":true}}}
	}}`)
	qs, err := parseElicitSchema(raw)
	if err != nil || len(qs) != 2 {
		t.Fatalf("题目数 = %d, err = %v", len(qs), err)
	}
	var single, multi elicitQuestion
	for _, q := range qs {
		if q.Multiple {
			multi = q
		} else {
			single = q
		}
	}
	if single.ID != "question_0" || single.OtherField != "question_0_custom" || len(single.Options) != 2 || single.Options[0].Const != "Red" {
		t.Errorf("单选题 = %+v", single)
	}
	if multi.ID != "question_1" || len(multi.Options) != 3 || multi.OtherField != "question_1_custom" {
		t.Errorf("多选题 = %+v", multi)
	}

	// 表单提交（多值）→ 回传形状：多选数组、单选与自由输入单值。
	ask := &pendingAsk{questions: []elicitQuestion{single, multi}, partial: map[string][]string{
		"question_0":        {"Red"},
		"question_1":        {"Apple", "Cherry"},
		"question_1_custom": {"Durian"},
	}}
	content := askContent(ask)
	if content["question_0"] != "Red" {
		t.Errorf("单选回传 = %v", content["question_0"])
	}
	if arr, ok := content["question_1"].([]string); !ok || len(arr) != 2 {
		t.Errorf("多选回传 = %v", content["question_1"])
	}
	if content["question_1_custom"] != "Durian" {
		t.Errorf("自由输入回传 = %v", content["question_1_custom"])
	}

	// 单题文本快捷路径的解析：编号（多选可多个）与自由文本。
	if got := parseAnswerText("1 3", multi); len(got) != 2 || got[0] != "Apple" || got[1] != "Cherry" {
		t.Errorf("编号多选解析 = %v", got)
	}
	if got := parseAnswerText("随便写的", multi); len(got) != 1 || got[0] != "随便写的" {
		t.Errorf("自由文本解析 = %v", got)
	}
	if got := parseAnswerText("1 2", single); len(got) != 1 || got[0] != "Red" {
		t.Errorf("单选题多编号应只取第一个 = %v", got)
	}
}

func TestAttachmentHelpers(t *testing.T) {
	if !isTextAttachment("text/plain; charset=utf-8", nil) {
		t.Error("text/* 应判为文本")
	}
	if isTextAttachment("image/png", []byte("abc")) {
		t.Error("image/* 不该判为文本")
	}
	if !isTextAttachment("", []byte("package main\n")) {
		t.Error("无 content_type 的纯文本应判为文本")
	}
	if isTextAttachment("application/octet-stream", []byte{0x1f, 0x00, 0x8b}) {
		t.Error("含 NUL 的内容不该判为文本")
	}

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	att := attachment{ID: "123", Filename: "note.md", ContentType: "text/markdown"}
	path, err := saveUpload(dir, att, []byte("hi"))
	if err != nil {
		t.Fatalf("saveUpload: %v", err)
	}
	if filepath.Base(path) != "123-note.md" {
		t.Errorf("落盘名 = %s", filepath.Base(path))
	}
	// exclude 首次写入，且重复保存不再追加
	if _, err := saveUpload(dir, att, []byte("hi2")); err != nil {
		t.Fatal(err)
	}
	ex, err := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if err != nil {
		t.Fatalf("exclude 未写入: %v", err)
	}
	if got := strings.Count(string(ex), uploadsDir+"/"); got != 1 {
		t.Errorf("exclude 里出现 %d 次，期望 1", got)
	}
}

func TestReportPath(t *testing.T) {
	dir := t.TempDir()
	work, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "报告.html"), []byte("<html/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(filepath.Join(t.TempDir(), "discord.json"), Deps{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.update(func(c *Config) {
		c.Bindings = []Binding{{ChannelID: "ch1", Workdir: work}}
	}); err != nil {
		t.Fatal(err)
	}

	if got, err := s.ReportPath("ch1", "报告.html"); err != nil || got != filepath.Join(work, "报告.html") {
		t.Errorf("正常路径: got %q err %v", got, err)
	}
	for name, rel := range map[string]string{
		"越界":    "../outside.html",
		"非HTML": "note.txt",
		"不存在":   "ghost.html",
		"空路径":   "",
	} {
		if _, err := s.ReportPath("ch1", rel); err == nil {
			t.Errorf("%s（%q）应该被拒", name, rel)
		}
	}
	if _, err := s.ReportPath("nope", "报告.html"); err == nil {
		t.Error("未绑定频道应该被拒")
	}
}

func TestMdToDiscord(t *testing.T) {
	in := strings.Join([]string{
		"### 标题保留",
		"#### 深标题降级",
		"| 名字 | 值 |",
		"|---|---|",
		"| pp-game/local | 127.0.0.1 |",
		"| 中文名 | x |",
		"---",
		"- [ ] 待办",
		"- [x] 已做",
		"```go",
		"#### 代码里的不动",
		"| a | b |",
		"```",
	}, "\n")
	out := mdToDiscord(in)

	for _, want := range []string{
		"### 标题保留",
		"**深标题降级**",
		"☐ 待办",
		"✅ 已做",
		"#### 代码里的不动", // 围栏内原样
		"| a | b |",   // 围栏内原样
	} {
		if !strings.Contains(out, want) {
			t.Errorf("缺少 %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "|---|") || strings.Contains(strings.Split(out, "```")[0], "| 名字 |") {
		t.Errorf("表格没转成代码块：\n%s", out)
	}
	if strings.Contains(out, "\n---\n") {
		t.Errorf("分隔线没去掉：\n%s", out)
	}
	// 表格代码块里两行数据列应对齐（中文按 2 宽）
	lines := strings.Split(out, "\n")
	var col2 []int
	for _, l := range lines {
		if i := strings.Index(l, "127.0.0.1"); i >= 0 {
			col2 = append(col2, displayWidth(l[:i]))
		}
		if i := strings.Index(l, "x"); i >= 0 && strings.Contains(l, "中文名") {
			col2 = append(col2, displayWidth(l[:i]))
		}
	}
	if len(col2) == 2 && col2[0] != col2[1] {
		t.Errorf("表格列没对齐（%v）：\n%s", col2, out)
	}
}

func TestDBToken(t *testing.T) {
	cases := map[string]bool{
		"@db 查一下玩家数":   true,
		"查一下 @数据库 的表":  true,
		"邮箱是 a@db.com": false,
		"平时问答不带令牌":     false,
		"@db":          true,
	}
	for in, want := range cases {
		if got := hasDBToken(in); got != want {
			t.Errorf("hasDBToken(%q) = %v, 期望 %v", in, got, want)
		}
	}
	if got := stripDBToken("@db 查一下玩家数"); got != "查一下玩家数" {
		t.Errorf("stripDBToken = %q", got)
	}
	if got := stripDBToken("先看 @数据库 再说"); got != "先看  再说" && got != "先看 再说" {
		t.Errorf("stripDBToken 中文令牌 = %q", got)
	}
}

func TestTurnSummary(t *testing.T) {
	if got := turnSummary(3*time.Second, 0, 0, nil); got != "" {
		t.Errorf("快问快答不该带小结，got %q", got)
	}
	got := turnSummary(83*time.Second, 5, 2, &acp.Usage{TotalTokens: 12345})
	if got != "-# ⏱ 1m23s · 🔧 5 · ✏️ 2 个文件 · 🧮 12.3k tok" {
		t.Errorf("turnSummary = %q", got)
	}
	if got := turnSummary(45*time.Second, 0, 0, nil); got != "-# ⏱ 45s" {
		t.Errorf("turnSummary = %q", got)
	}
}

func TestResolveInWorkdir(t *testing.T) {
	dir := t.TempDir()
	work, _ := filepath.EvalSymlinks(dir)
	if err := os.WriteFile(filepath.Join(work, "chart.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveInWorkdir(work, "chart.png"); err != nil || got != filepath.Join(work, "chart.png") {
		t.Errorf("正常路径: %q %v", got, err)
	}
	for name, rel := range map[string]string{
		"越界": "../evil.txt", "空": "", "目录": ".", "不存在": "ghost.png",
	} {
		if _, err := resolveInWorkdir(work, rel); err == nil {
			t.Errorf("%s（%q）应该被拒", name, rel)
		}
	}
}

// 命令表是命令说明的唯一事实源：desc 就是输入框里打 `/` 时看到的那句话，
// 手册与 /help 都不再抄清单（抄一份必漂移）。这条测试盯着表本身完整、
// 描述不超平台上限。

func TestSlashCommandTable(t *testing.T) {
	cmds := slashCommands()
	if len(cmds) < 12 {
		t.Fatalf("命令表只有 %d 条，疑似被误删", len(cmds))
	}
	seen := map[string]bool{}
	for _, c := range cmds {
		if c.name == "" || c.desc == "" {
			t.Errorf("命令 %q 的描述缺失（desc=%q）", c.name, c.desc)
		}
		if len(c.desc) > 100 {
			t.Errorf("/%s 的描述超过 Discord 100 字符上限", c.name)
		}
		if seen[c.name] {
			t.Errorf("命令 %q 重复", c.name)
		}
		seen[c.name] = true
	}
	// 手册不再列命令，改为指路输入框——这条防止清单被人重新抄回去。
	md := guideMD(Binding{Repo: "o/r", Branch: "main", Agent: "claude"})
	if strings.Contains(md, "`/mcps`") || strings.Contains(md, "`/usage`") {
		t.Errorf("手册里又出现了命令清单（应指路输入框）：\n%s", md)
	}
	if !strings.Contains(md, "打 `/`") {
		t.Error("手册应告诉用户在输入框打 / 看全部命令")
	}
}

// send_file 的核心契约：文件本体一定作为附件发出去。.html 早先只发长图，
// 用户点名要过原文件——「把 xxx.html 发上来」要的是那个文件，不是它的
// 截图，所以原文件在任何分支（含渲染失败）里都不能丢。

func TestPrepareOutFiles(t *testing.T) {
	dir := t.TempDir()
	work, _ := filepath.EvalSymlinks(dir)
	if err := os.WriteFile(filepath.Join(work, "chart.png"), []byte("png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	html := "<html><head><title>t</title></head><body><h1>报告</h1></body></html>"
	if err := os.WriteFile(filepath.Join(work, "报告.html"), []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := prepareOutFiles(context.Background(), work, "chart.png", true)
	if err != nil || len(got) != 1 || got[0].name != "chart.png" || string(got[0].data) != "png-bytes" {
		t.Fatalf("非 HTML 应原样发一个附件，得到 %+v（err=%v）", names(got), err)
	}

	got, err = prepareOutFiles(context.Background(), work, "报告.html", false)
	if err != nil || len(got) != 1 || got[0].name != "报告.html" || string(got[0].data) != html {
		t.Fatalf("preview=false 应只发原 HTML，得到 %v（err=%v）", names(got), err)
	}

	// preview=true：有 Chrome 时长图排第一（Discord 只把首个附件渲染成
	// 预览大图），没 Chrome 时降级只剩原文件——两种情况下原文件都在。
	got, err = prepareOutFiles(context.Background(), work, "报告.html", true)
	if err != nil {
		t.Fatalf("preview=true 不该失败（渲染不了要降级）：%v", err)
	}
	if got[len(got)-1].name != "报告.html" || string(got[len(got)-1].data) != html {
		t.Fatalf("原 HTML 必须在附件里，得到 %v", names(got))
	}
	if len(got) == 2 && got[0].name != "报告.png" {
		t.Errorf("长图应排在原文件之前并按报告名命名，得到 %v", names(got))
	}

	if _, err := prepareOutFiles(context.Background(), work, "../evil.html", true); err == nil {
		t.Error("越界路径应被拒")
	}
}

func names(files []outFile) []string {
	var out []string
	for _, f := range files {
		out = append(out, f.name)
	}
	return out
}

func TestBatchFiles(t *testing.T) {
	small := make([]outFile, 11)
	for i := range small {
		small[i] = outFile{name: "f.txt", data: []byte("x")}
	}
	if got := batchFiles(small); len(got) != 2 || len(got[0]) != 10 || len(got[1]) != 1 {
		t.Errorf("11 个小文件应切成 10+1 批，得到 %d 批", len(got))
	}

	big := []outFile{
		{name: "a.bin", data: make([]byte, 20<<20)},
		{name: "b.bin", data: make([]byte, 20<<20)},
	}
	if got := batchFiles(big); len(got) != 2 {
		t.Errorf("两个 20MB 合计超过单条上限，应分两批，得到 %d 批", len(got))
	}
	if got := batchFiles(nil); got != nil {
		t.Errorf("空清单应得到空批次，得到 %v", got)
	}
}
