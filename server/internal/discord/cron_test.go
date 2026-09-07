package discord

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"acpp/server/internal/schedule"
)

// 契约：NO_REPORT 只认「整条回复就是它」；摘要取第一行有内容的正文并剥掉
// 标题/加粗记号；起始消息一行状态 + 一行小字。

func TestIsNoReportAndSummary(t *testing.T) {
	for _, yes := range []string{"NO_REPORT", "no_report。", "NO REPORT\n-# 自上次以来无新增错误"} {
		if !isNoReport(yes) {
			t.Errorf("isNoReport(%q) 应为真", yes)
		}
	}
	for _, no := range []string{"", "发现 3 条 ERROR\nNO_REPORT", "NO_REPORT " + strings.Repeat("x", 400)} {
		if isNoReport(no) {
			t.Errorf("isNoReport(%q) 应为假", no)
		}
	}
	reply := "```\ncode\n```\n\n## **日报 09-03**\n\n- DAU 12.3k，环比 -2%\n"
	if got := jobSummary(reply); got != "日报 09-03" {
		t.Errorf("jobSummary = %q", got)
	}
	if got := jobSummary("-# 小字\n| a | b |\n**DAU** 12.3k ↓2%"); got != "DAU 12.3k ↓2%" {
		t.Errorf("jobSummary(跳过小字与表格) = %q", got)
	}
	when := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	head := jobHeadline(schedule.Job{Name: "日报"}, when, "✅ 4m12s", "第一行\n第二行")
	if !strings.HasPrefix(head, "📅 **日报** · 09-03 10:00 · ✅ 4m12s\n-# 第一行 第二行") {
		t.Errorf("jobHeadline = %q", head)
	}
	if fmtElapsed(252*time.Second) != "4m12s" || fmtElapsed(38*time.Second) != "38s" {
		t.Error("fmtElapsed 格式不对")
	}
	if isSnowflake("web") || !isSnowflake("1544238324653363260") {
		t.Error("isSnowflake 判定不对")
	}
}

// 契约：开场注入带运行时事实（几点、上次结果）与无人值守约定，任务正文在最后。

func TestJobPreamble(t *testing.T) {
	last := time.Date(2026, 9, 2, 2, 0, 0, 0, time.UTC)
	job := schedule.Job{Name: "日报", TZ: "Asia/Shanghai", LastRunAt: &last, LastStatus: schedule.StatusOK, LastSummary: "DAU 12.3k"}
	when := time.Date(2026, 9, 3, 10, 0, 0, 0, job.Location())
	pre := jobPreamble(job, schedule.Run{Trigger: schedule.TriggerManual}, when)
	for _, want := range []string{"「日报」", "2026-09-03 10:00", "手动触发", "上次运行：2026-09-02 10:00，结果 成功，摘要：「DAU 12.3k」", "NO_REPORT", "---- 任务 ----"} {
		if !strings.Contains(pre, want) {
			t.Errorf("开场缺少 %q:\n%s", want, pre)
		}
	}
	first := jobPreamble(schedule.Job{Name: "n"}, schedule.Run{}, when)
	if !strings.Contains(first, "第一次运行") || strings.Contains(first, "手动触发") {
		t.Errorf("首次运行的开场不对:\n%s", first)
	}
}

// 契约：/cron 的 id 参数认 id 与唯一的名字前缀；只有一条时可省略。

func TestFindJob(t *testing.T) {
	jobs := []schedule.Job{{ID: "j_1", Name: "用户日报"}, {ID: "j_2", Name: "用户周报"}, {ID: "j_3", Name: "日志巡检"}}
	if j, err := findJob(jobs, "j_2"); err != nil || j.ID != "j_2" {
		t.Errorf("按 id 找: %v %+v", err, j)
	}
	if j, err := findJob(jobs, "日志"); err != nil || j.ID != "j_3" {
		t.Errorf("按前缀找: %v %+v", err, j)
	}
	if _, err := findJob(jobs, "用户"); err == nil {
		t.Error("多条匹配应报错")
	}
	if _, err := findJob(jobs, ""); err == nil {
		t.Error("多条任务不指定应报错")
	}
	if j, err := findJob(jobs[:1], ""); err != nil || j.ID != "j_1" {
		t.Errorf("只有一条时可省略: %v", err)
	}
	if _, err := findJob(jobs, "zzz"); err == nil {
		t.Error("找不到应报错")
	}
}

// 契约：任务卡三个按钮的 custom_id 都走 jobPrefix（分发表按它路由）；
// schedule 的哨兵映射成本包的（httpapi 只认本包那套）。

func TestJobCardAndErrMapping(t *testing.T) {
	next := time.Date(2026, 9, 4, 2, 0, 0, 0, time.UTC)
	card := jobCard(schedule.Job{ID: "j_9", Name: "日报", Cron: "0 10 * * *", TZ: "Asia/Shanghai", Enabled: true, NextRunAt: &next, Prompt: "写日报"})
	raw, _ := json.Marshal(card)
	for _, want := range []string{`"cj:run:j_9"`, `"cj:toggle:j_9"`, `"cj:rm:j_9"`, "每天 10:00", "下次 09-04 10:00", "写日报"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("任务卡缺少 %s: %s", want, raw)
		}
	}
	if !strings.Contains(string(raw), "⏸ 停用") {
		t.Error("启用中的任务卡应给「停用」按钮")
	}
	off := jobCard(schedule.Job{ID: "j_9", Name: "日报", Cron: "0 10 * * *", DisabledReason: "连续失败 5 次，已自动停用"})
	raw, _ = json.Marshal(off)
	if !strings.Contains(string(raw), "▶️ 启用") || !strings.Contains(string(raw), "连续失败") {
		t.Errorf("停用的任务卡应给「启用」并说明原因: %s", raw)
	}

	if !errors.Is(jobErr(schedule.ErrNotFound), ErrNotFound) || !errors.Is(jobErr(schedule.ErrInvalid), ErrInvalid) ||
		!errors.Is(jobErr(schedule.ErrRunning), ErrInvalid) || jobErr(nil) != nil {
		t.Error("哨兵映射不对")
	}
	if got := cronURL("http://127.0.0.1:48080/api/mcp/discord/peer_abc"); got != "http://127.0.0.1:48080/api/mcp/discord-cron/peer_abc" {
		t.Errorf("cronURL = %s", got)
	}
}

// 契约：配了调度器的子区会话同时挂 acpp-chat 与 acpp-cron 两个面（claude
// 走 _meta 且工具预批，codex 走 mcpServers）；没配则只有交付面。

func TestChatMountsIncludeCron(t *testing.T) {
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "discord.json"), Deps{MCPBase: "http://127.0.0.1:1/api/mcp/discord/"})
	if err != nil {
		t.Fatal(err)
	}
	b := Binding{ChannelID: "c1", Workdir: dir, Agent: "claude"}
	_, meta, err := s.chatMounts("t1", b)
	if err != nil {
		t.Fatal(err)
	}
	opts := meta["claudeCode"].(map[string]any)["options"].(map[string]any)
	if _, has := opts["mcpServers"].(map[string]any)[cronServerName]; has {
		t.Error("没配调度器不该挂 acpp-cron")
	}

	s.sched, err = schedule.New(filepath.Join(dir, "schedule.json"), nil, schedule.Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	_, meta, err = s.chatMounts("t1", b)
	if err != nil {
		t.Fatal(err)
	}
	opts = meta["claudeCode"].(map[string]any)["options"].(map[string]any)
	servers := opts["mcpServers"].(map[string]any)
	if _, has := servers[cronServerName]; !has {
		t.Fatalf("claude 侧应挂 acpp-cron: %v", servers)
	}
	cronCfg := servers[cronServerName].(map[string]any)
	if !strings.Contains(cronCfg["url"].(string), "/api/mcp/discord-cron/") {
		t.Errorf("acpp-cron 的回连地址不对: %v", cronCfg)
	}
	allowed := strings.Join(opts["allowedTools"].([]string), ",")
	if !strings.Contains(allowed, "mcp__acpp-cron__cron_add") || !strings.Contains(allowed, "mcp__acpp-chat__send_file") {
		t.Errorf("预批清单不全: %s", allowed)
	}

	b.Agent = "codex"
	list, _, err := s.chatMounts("t1", b)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Errorf("codex 侧应有两个 server，得到 %d", len(list))
	}
}

// 契约：工具面里的 cron_add 能凭子区推出频道，建出的任务归属那个频道，
// 子区不明时报错而不是建到空 scope 上。

func TestCronToolsScope(t *testing.T) {
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "discord.json"), Deps{})
	if err != nil {
		t.Fatal(err)
	}
	s.sched, err = schedule.New(filepath.Join(dir, "schedule.json"), nil, schedule.Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.update(func(c *Config) {
		c.upsertBinding(Binding{ChannelID: "c1", Workdir: dir, Agent: "claude"})
		c.upsertThread(Thread{ThreadID: "t1", ChannelID: "c1"})
	}); err != nil {
		t.Fatal(err)
	}
	tools := s.cronTools("t1")
	var add, list func(args string) (string, error)
	for _, tl := range tools {
		switch tl.Name {
		case cronToolAdd:
			add = func(args string) (string, error) { return tl.Call(t.Context(), json.RawMessage(args)) }
		case cronToolList:
			list = func(args string) (string, error) { return tl.Call(t.Context(), json.RawMessage(args)) }
		}
	}
	out, err := add(`{"name":"日报","cron":"0 10 * * *","tz":"Asia/Shanghai","prompt":"写日报"}`)
	if err != nil || !strings.Contains(out, "已创建定时任务") {
		t.Fatalf("cron_add: %v %q", err, out)
	}
	jobs := s.sched.Jobs("c1")
	if len(jobs) != 1 || jobs[0].Scope != "c1" || jobs[0].Cron != "0 10 * * *" {
		t.Errorf("任务归属不对: %+v", jobs)
	}
	if out, err := list("{}"); err != nil || !strings.Contains(out, "日报") {
		t.Errorf("cron_list: %v %q", err, out)
	}
	if _, err := add(`{"name":"坏","cron":"99 10 * * *","prompt":"x"}`); err == nil {
		t.Error("坏表达式应报工具级错误")
	}
	if _, err := s.cronTools("unknown")[0].Call(t.Context(), json.RawMessage(`{"name":"n","cron":"0 1 * * *","prompt":"p"}`)); err == nil {
		t.Error("子区不明应报错")
	}
}

// 契约：remove 的 id 参数按逗号切成多段，每段各自按 findJob 的规则匹配，
// 任一段对不上整条拒绝（不能删了一半才报错）；重复命中只算一次；尾逗号
// 不算一段；整个参数为空退回单条语义。

func TestFindJobs(t *testing.T) {
	jobs := []schedule.Job{{ID: "j_1", Name: "用户日报"}, {ID: "j_2", Name: "用户周报"}, {ID: "j_3", Name: "日志巡检"}}
	got, err := findJobs(jobs, "j_1, 日志,")
	if err != nil || len(got) != 2 || got[0].ID != "j_1" || got[1].ID != "j_3" {
		t.Errorf("逗号分隔 + 尾逗号: %v %+v", err, got)
	}
	if got, err := findJobs(jobs, "j_2,用户周"); err != nil || len(got) != 1 {
		t.Errorf("同一条命中两次只算一次: %v %+v", err, got)
	}
	if _, err := findJobs(jobs, "j_1,zzz"); err == nil {
		t.Error("有一段对不上应整条拒绝")
	}
	if _, err := findJobs(jobs, "j_1,用户"); err == nil {
		t.Error("有一段多义应整条拒绝")
	}
	if got, err := findJobs(jobs[:1], ""); err != nil || len(got) != 1 || got[0].ID != "j_1" {
		t.Errorf("为空且只有一条时退回单条语义: %v %+v", err, got)
	}
	if _, err := findJobs(jobs, ""); err == nil {
		t.Error("为空且多条时应报错")
	}
}

// 契约：批量删除逐条执行并汇总回执——删成功的都点名，删失败的单独列出；
// 已经不在的视为删成功（用户要的结果已成立）。清空确认按钮的 custom_id
// 走 jobPrefix 且携带频道 id（/cron 可能在子区里敲，现场推不出父频道）。

func TestRemoveJobsAndClear(t *testing.T) {
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "discord.json"), Deps{})
	if err != nil {
		t.Fatal(err)
	}
	s.sched, err = schedule.New(filepath.Join(dir, "schedule.json"), nil, schedule.Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	var jobs []schedule.Job
	for _, name := range []string{"日报", "周报"} {
		j, err := s.sched.Add(schedule.Input{Scope: "c1", Name: name, Cron: "0 10 * * *", Prompt: "p"})
		if err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, j)
	}
	// 其中一条先删掉，模拟「清单是旧的、任务已被别人删了」。
	if err := s.sched.Remove(jobs[1].ID); err != nil {
		t.Fatal(err)
	}
	text := s.removeJobs(jobs)
	if !strings.Contains(text, "已删除 2 条") || !strings.Contains(text, "日报") || !strings.Contains(text, "周报") || strings.Contains(text, "删不掉") {
		t.Errorf("回执 = %q", text)
	}
	if left := s.sched.Jobs("c1"); len(left) != 0 {
		t.Errorf("应全部删光，剩 %d 条", len(left))
	}
	if !strings.HasPrefix(jobPrefix+"clr:c1", jobPrefix) {
		t.Error("清空按钮必须走 jobPrefix 路由")
	}
}
