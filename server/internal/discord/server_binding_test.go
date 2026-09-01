package discord

import (
	"path/filepath"
	"strings"
	"testing"
)

// 频道锁定一台服务器（adr-019）相关的契约。单独成文件是因为主测试文件
// 已到行数硬线——按主题拆，不是按语言再切一刀。

// 契约：频道锁定的服务器要能落盘并重载回来（adr-019）。名字是展示快照，
// 服务器被删掉之后主题与 /status 仍要说得清原本绑的是谁。
func TestBindingServerRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "discord.json")
	s1, err := newStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s1.update(func(c *Config) {
		c.upsertBinding(Binding{
			ChannelID: "c1", Repo: "org/app", Workdir: "/tmp/x",
			Agent: "claude", Model: "m1",
			ServerID: 7, ServerName: "pp-game-live",
		})
	}); err != nil {
		t.Fatal(err)
	}

	s2, err := newStore(path)
	if err != nil {
		t.Fatal(err)
	}
	b, ok := s2.config().binding("c1")
	if !ok {
		t.Fatal("绑定没读回来")
	}
	if b.ServerID != 7 || b.ServerName != "pp-game-live" {
		t.Fatalf("服务器绑定丢了: %+v", b)
	}
}

// 契约：主题只在锁定服务器时才写那一段。不锁定是常态（服务器不做项目
// 隔离），每个频道都挂一句「不锁定」只是噪声。
func TestTopicLine_ServerOnlyWhenLocked(t *testing.T) {
	base := Binding{
		Repo: "org/app", Branch: "discord/prod", Base: "live",
		Workdir: "/tmp/w", Agent: "claude", Model: "m",
		DataSourceID: 29, DataSourceRef: "pp-game/prod",
	}
	if got := topicLine(base); strings.Contains(got, "服务器") {
		t.Errorf("没锁定服务器时主题不该提它: %s", got)
	}

	locked := base
	locked.ServerID = 7
	locked.ServerName = "pp-game-live"
	got := topicLine(locked)
	if !strings.Contains(got, "服务器：pp-game-live") {
		t.Errorf("锁定后主题要写明是哪台: %s", got)
	}
	if n := len([]rune(got)); n > topicLimit {
		t.Errorf("加了服务器之后主题 %d 字符，超了上限 %d", n, topicLimit)
	}
}

// 契约：服务器被删掉后只剩 id，主题仍要给出可辨认的东西而不是空白。
func TestServerLine_Fallbacks(t *testing.T) {
	if got := serverLine(Binding{}); got != "不锁定" {
		t.Errorf("未锁定应说不锁定，得到 %q", got)
	}
	if got := serverLine(Binding{ServerID: 7}); got != "#7" {
		t.Errorf("名字快照丢了应退回 id，得到 %q", got)
	}
	if got := serverLine(Binding{ServerID: 7, ServerName: "box"}); got != "box" {
		t.Errorf("有名字就用名字，得到 %q", got)
	}
}

// 契约：子区的作用域说明要把两把锁分别说清。工具面已经锁死范围，
// 这段话是让模型别去猜别的环境/别的机器。
func TestDiscordInstructions_ScopeLines(t *testing.T) {
	plain := discordInstructionsFor(Binding{})
	if strings.Contains(plain, "锁定数据源") || strings.Contains(plain, "锁定服务器") {
		t.Errorf("什么都没锁时不该有作用域说明: %s", plain)
	}

	both := discordInstructionsFor(Binding{
		DataSourceID: 1, DataSourceRef: "pp-game/prod",
		ServerID: 7, ServerName: "pp-game-live",
	})
	if !strings.Contains(both, "锁定数据源 **pp-game/prod**") {
		t.Errorf("缺数据源作用域说明: %s", both)
	}
	if !strings.Contains(both, "锁定服务器 **pp-game-live**") {
		t.Errorf("缺服务器作用域说明: %s", both)
	}

	// 只锁一样时只说那一样。
	onlyServer := discordInstructionsFor(Binding{ServerID: 7, ServerName: "box"})
	if strings.Contains(onlyServer, "锁定数据源") {
		t.Errorf("没锁数据源却说了: %s", onlyServer)
	}
}

// 契约：服务器选项的编码与解码对称，且「不锁定」解出来是 0。
func TestServerChoices(t *testing.T) {
	hosts := []ServerOption{
		{ID: 1, Name: "a", Host: "10.0.0.1", Note: "生产机"},
		{ID: 2, Name: "b", Host: "10.0.0.2"},
	}
	opts := serverChoices(hosts, 2)
	if len(opts) != 3 {
		t.Fatalf("两台机器应给三个选项（含不锁定），得到 %d", len(opts))
	}
	// 当前绑定的那台排最前并标注。
	if opts[0].Label != "b（当前）" {
		t.Errorf("当前绑定应排最前并标注，得到 %q", opts[0].Label)
	}
	if !strings.Contains(opts[1].Description, "生产机") {
		t.Errorf("备注要进描述（那是用户区分机器的依据）: %+v", opts[1])
	}

	id, name := parseServerChoice(opts[1].Value)
	if id != 1 || name != "a" {
		t.Errorf("编解码不对称: %d/%q", id, name)
	}
	if id, _ := parseServerChoice(dbNoneValue); id != 0 {
		t.Errorf("不锁定应解成 0，得到 %d", id)
	}
}
