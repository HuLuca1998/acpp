package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
)

func usageDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "usage.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.SkillUsage{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return gdb
}

// 在源目录放一个真实技能，供 codex 路径识别校验「该技能确实存在」。
func withSkillDir(t *testing.T, name string) string {
	t.Helper()
	dataDir := t.TempDir()
	dir := filepath.Join(dataDir, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: x\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dataDir
}

func toolCall(id, kind string, rawInput, rawOutput, locations string) acp.Event {
	ev := acp.Event{Kind: acp.EventToolCall, ToolCallID: id, ToolKind: kind}
	if rawInput != "" {
		ev.RawInput = json.RawMessage(rawInput)
	}
	if rawOutput != "" {
		ev.RawOutput = json.RawMessage(rawOutput)
	}
	if locations != "" {
		ev.Locations = json.RawMessage(locations)
	}
	return ev
}

// 契约：claude 的 Skill 工具——技能名在 rawInput.skill，带 plugin 前缀 acpp:，
// 去前缀归一到目录名。
func TestSkillName_Claude_RawInputSkill(t *testing.T) {
	got := skillNameFromToolCall("/any/skills", toolCall("t1", "other",
		`{"skill":"acpp:pirate-greeting"}`, "", ""))
	if got != "pirate-greeting" {
		t.Fatalf("got %q, want pirate-greeting", got)
	}
}

// 契约：claude 完成时 rawOutput.response.commandName 也能识别（rawInput 为空时的兜底）。
func TestSkillName_Claude_RawOutputCommandName(t *testing.T) {
	got := skillNameFromToolCall("/any/skills", toolCall("t1", "other",
		"", `{"toolName":"Skill","response":{"success":true,"commandName":"acpp:code-review"}}`, ""))
	if got != "code-review" {
		t.Fatalf("got %q, want code-review", got)
	}
}

// 契约：codex 的 read 工具读 <srcDir>/<name>/SKILL.md，从路径取技能名，
// 且该技能必须在源目录真实存在（防误把别处的 SKILL.md 当技能调用）。
func TestSkillName_Codex_ReadSkillMd(t *testing.T) {
	dataDir := withSkillDir(t, "deploy-runbook")
	srcDir := filepath.Join(dataDir, "skills")
	path := filepath.Join(srcDir, "deploy-runbook", "SKILL.md")

	got := skillNameFromToolCall(srcDir, toolCall("t1", "read", "", "",
		`[{"path":"`+path+`"}]`))
	if got != "deploy-runbook" {
		t.Fatalf("got %q, want deploy-runbook", got)
	}
}

// 契约：读一个不存在于源目录的 SKILL.md 不算技能调用（不误计）。
func TestSkillName_Codex_UnknownSkillMdIgnored(t *testing.T) {
	dataDir := withSkillDir(t, "real-skill")
	srcDir := filepath.Join(dataDir, "skills")
	got := skillNameFromToolCall(srcDir, toolCall("t1", "read", "", "",
		`[{"path":"/somewhere/else/ghost/SKILL.md"}]`))
	if got != "" {
		t.Fatalf("got %q, want empty (unknown skill)", got)
	}
}

// 契约：普通工具调用（读别的文件、执行命令）不是技能调用。
func TestSkillName_NonSkillToolCallIsEmpty(t *testing.T) {
	srcDir := "/any/skills"
	cases := []acp.Event{
		toolCall("t1", "read", "", "", `[{"path":"/proj/main.go"}]`),
		toolCall("t2", "execute", `{"command":"ls"}`, "", ""),
		toolCall("t3", "edit", `{"path":"/proj/a.ts"}`, "", ""),
	}
	for i, ev := range cases {
		if got := skillNameFromToolCall(srcDir, ev); got != "" {
			t.Fatalf("case %d: got %q, want empty", i, got)
		}
	}
}

// 契约：一次技能触发 = 一个 toolCallId 的多条事件，只计一次。
func TestSkillUsage_Observe_DedupsByToolCallID(t *testing.T) {
	dataDir := withSkillDir(t, "pirate-greeting")
	svc := NewSkillUsageService(usageDB(t), dataDir)

	// 同一 toolCallId：初始空 rawInput 的 tool_call + 带名字的 update + 完成 update。
	svc.Observe(toolCall("call-1", "other", "{}", "", ""))
	svc.Observe(toolCall("call-1", "other", `{"skill":"acpp:pirate-greeting"}`, "", ""))
	svc.Observe(toolCall("call-1", "other", "", `{"response":{"commandName":"acpp:pirate-greeting"}}`, ""))

	counts, err := svc.CountsByName(context.Background())
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if counts["pirate-greeting"] != 1 {
		t.Fatalf("count = %d, want 1 (deduped by toolCallId)", counts["pirate-greeting"])
	}
}

// 契约：不同 toolCallId 的多次调用累加，Top 按次数降序。
func TestSkillUsage_Observe_AccumulatesAndRanks(t *testing.T) {
	dataDir := withSkillDir(t, "pirate-greeting")
	// 再加一个技能目录用于第二种。
	if err := os.MkdirAll(filepath.Join(dataDir, "skills", "commit-style"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "skills", "commit-style", "SKILL.md"),
		[]byte("---\nname: commit-style\ndescription: x\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := NewSkillUsageService(usageDB(t), dataDir)

	for _, id := range []string{"a", "b", "c"} {
		svc.Observe(toolCall(id, "other", `{"skill":"acpp:pirate-greeting"}`, "", ""))
	}
	svc.Observe(toolCall("d", "other", `{"skill":"commit-style"}`, "", ""))

	top, err := svc.Top(context.Background(), 10)
	if err != nil {
		t.Fatalf("top: %v", err)
	}
	if len(top) != 2 {
		t.Fatalf("top len = %d, want 2 (%+v)", len(top), top)
	}
	if top[0].Name != "pirate-greeting" || top[0].Count != 3 {
		t.Fatalf("rank[0] = %+v, want pirate-greeting=3", top[0])
	}
	if top[1].Name != "commit-style" || top[1].Count != 1 {
		t.Fatalf("rank[1] = %+v, want commit-style=1", top[1])
	}
}

// 契约：删除技能的计数后，概览统计不再返回它（否则会指向已删技能）。
func TestSkillUsage_Delete_RemovesCount(t *testing.T) {
	dataDir := withSkillDir(t, "pirate-greeting")
	svc := NewSkillUsageService(usageDB(t), dataDir)
	svc.Observe(toolCall("a", "other", `{"skill":"acpp:pirate-greeting"}`, "", ""))

	if err := svc.Delete("pirate-greeting"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	counts, err := svc.CountsByName(context.Background())
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if len(counts) != 0 {
		t.Fatalf("counts = %v, want empty after delete", counts)
	}
}

// 契约：非技能事件不落任何计数行。
func TestSkillUsage_Observe_IgnoresNonSkill(t *testing.T) {
	svc := NewSkillUsageService(usageDB(t), t.TempDir())
	svc.Observe(toolCall("x", "execute", `{"command":"ls"}`, "", ""))
	svc.Observe(acp.Event{Kind: acp.EventMessage, Text: "hi"})

	counts, err := svc.CountsByName(context.Background())
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if len(counts) != 0 {
		t.Fatalf("counts = %v, want empty", counts)
	}
}
