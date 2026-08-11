package acp

import (
	"reflect"
	"testing"
)

// fixture 数据来自 2026-08-11 对 claude-agent-acp 0.63.0 / codex-acp 1.1.7
// 的实测（docs/adr-001）。字段值不是编的——改这里前先重跑 scripts/acp-probe.py。

func claudeCaps() Caps {
	return Caps{
		Modes: &Modes{
			CurrentModeID: "default",
			AvailableModes: []Mode{
				{ID: "auto", Name: "Auto"},
				{ID: "default", Name: "Manual"},
				{ID: "acceptEdits", Name: "Accept Edits"},
				{ID: "plan", Name: "Plan Mode"},
				{ID: "dontAsk", Name: "Don't Ask"},
				{ID: "bypassPermissions", Name: "Bypass Permissions"},
			},
		},
		ConfigOptions: []ConfigOption{
			{ID: "mode", Category: "mode", Type: "select", CurrentValue: "default"},
			{ID: "model", Category: "model", Type: "select", CurrentValue: "default", Options: []ConfigOptionValue{
				{Value: "default", Name: "Default (recommended)"},
				{Value: "opus[1m]", Name: "Opus (1M context)"},
				{Value: "claude-fable-5[1m]", Name: "Fable"},
				{Value: "sonnet", Name: "Sonnet"},
				{Value: "haiku", Name: "Haiku"},
			}},
			{ID: "effort", Category: "thought_level", Type: "select", CurrentValue: "xhigh", Options: []ConfigOptionValue{
				{Value: "default", Name: "Default"},
				{Value: "low", Name: "Low"},
				{Value: "medium", Name: "Medium"},
				{Value: "high", Name: "High"},
				{Value: "xhigh", Name: "Xhigh"},
				{Value: "max", Name: "Max"},
			}},
			{ID: "fast", Category: "model_config", Type: "select", CurrentValue: "off", Options: []ConfigOptionValue{
				{Value: "on", Name: "On"},
				{Value: "off", Name: "Off"},
			}},
			{ID: "agent", Type: "select", CurrentValue: "default"},
		},
	}
}

func codexCaps() Caps {
	return Caps{
		Modes: &Modes{
			CurrentModeID: "agent",
			AvailableModes: []Mode{
				{ID: "read-only", Name: "Read-only"},
				{ID: "agent", Name: "Agent"},
				{ID: "agent-full-access", Name: "Agent (full access)"},
			},
		},
		ConfigOptions: []ConfigOption{
			{ID: "mode", Category: "mode", Type: "select", CurrentValue: "agent"},
			{ID: "collaboration_mode", Category: "collaboration_mode", Type: "select", CurrentValue: "default", Options: []ConfigOptionValue{
				{Value: "default", Name: "Default"},
				{Value: "plan", Name: "Plan"},
			}},
			{ID: "model", Category: "model", Type: "select", CurrentValue: "gpt-5.6-sol", Options: []ConfigOptionValue{
				{Value: "gpt-5.6-sol", Name: "GPT-5.6-Sol"},
				{Value: "gpt-5.6-terra", Name: "GPT-5.6-Terra"},
				{Value: "gpt-5.6-luna", Name: "GPT-5.6-Luna"},
				{Value: "gpt-5.5", Name: "GPT-5.5"},
				{Value: "gpt-5.2", Name: "GPT-5.2"},
			}},
			{ID: "reasoning_effort", Category: "thought_level", Type: "select", CurrentValue: "high", Options: []ConfigOptionValue{
				{Value: "low", Name: "Low"},
				{Value: "medium", Name: "Medium"},
				{Value: "high", Name: "High"},
				{Value: "xhigh", Name: "Xhigh"},
				{Value: "max", Name: "Max"},
				{Value: "ultra", Name: "Ultra"},
			}},
			{ID: "fast-mode", Category: "model_config", Type: "select", CurrentValue: "off", Options: []ConfigOptionValue{
				{Value: "off", Name: "Off"},
				{Value: "on", Name: "On"},
			}},
		},
	}
}

func TestClaudeSettings(t *testing.T) {
	s := claudeAdapter{}.Settings(claudeCaps())

	if s.Flavor != FlavorClaude {
		t.Fatalf("flavor = %q", s.Flavor)
	}
	if len(s.Models) != 5 || s.Models[2].ID != "claude-fable-5[1m]" {
		t.Fatalf("models = %+v", s.Models)
	}
	if s.CurrentModel != "default" {
		t.Fatalf("currentModel = %q", s.CurrentModel)
	}
	// effort 的 default 值不是档位，不进词汇表。
	want := []Effort{EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}
	if !reflect.DeepEqual(s.Efforts, want) {
		t.Fatalf("efforts = %v", s.Efforts)
	}
	if s.CurrentEffort != EffortXHigh {
		t.Fatalf("currentEffort = %q", s.CurrentEffort)
	}
	// auto 与 dontAsk 不在词汇表内，不暴露。
	if !reflect.DeepEqual(s.Levels, []AccessLevel{AccessSafe, AccessAutoEdit, AccessFull}) {
		t.Fatalf("levels = %v", s.Levels)
	}
	if s.CurrentLevel != AccessSafe {
		t.Fatalf("currentLevel = %q", s.CurrentLevel)
	}
	if !s.PlanSupported || s.PlanOn {
		t.Fatalf("plan = %v/%v", s.PlanSupported, s.PlanOn)
	}
	if !s.FastSupported || s.FastOn {
		t.Fatalf("fast = %v/%v", s.FastSupported, s.FastOn)
	}
}

func TestClaudeSettingsInPlanMode(t *testing.T) {
	caps := claudeCaps()
	caps.Modes.CurrentModeID = "plan"

	s := claudeAdapter{}.Settings(caps)
	if !s.PlanOn {
		t.Fatal("planOn should be true")
	}
	// plan 是 claude 的一个档位；处于 plan 时统一档位无对应值。
	if s.CurrentLevel != "" {
		t.Fatalf("currentLevel = %q, want empty", s.CurrentLevel)
	}
}

func TestCodexSettings(t *testing.T) {
	s := codexAdapter{}.Settings(codexCaps())

	if s.Flavor != FlavorCodex {
		t.Fatalf("flavor = %q", s.Flavor)
	}
	if len(s.Models) != 5 || s.CurrentModel != "gpt-5.6-sol" {
		t.Fatalf("models = %+v current = %q", s.Models, s.CurrentModel)
	}
	// ultra 超出词汇表，按交集丢弃。
	want := []Effort{EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}
	if !reflect.DeepEqual(s.Efforts, want) {
		t.Fatalf("efforts = %v", s.Efforts)
	}
	if s.CurrentEffort != EffortHigh {
		t.Fatalf("currentEffort = %q", s.CurrentEffort)
	}
	if !reflect.DeepEqual(s.Levels, []AccessLevel{AccessSafe, AccessAutoEdit, AccessFull}) {
		t.Fatalf("levels = %v", s.Levels)
	}
	// codex 默认档 agent → auto-edit。
	if s.CurrentLevel != AccessAutoEdit {
		t.Fatalf("currentLevel = %q", s.CurrentLevel)
	}
	if !s.PlanSupported || s.PlanOn {
		t.Fatalf("plan = %v/%v", s.PlanSupported, s.PlanOn)
	}
	if !s.FastSupported || s.FastOn {
		t.Fatalf("fast = %v/%v", s.FastSupported, s.FastOn)
	}
}

func TestCodexSettingsPlanOn(t *testing.T) {
	caps := codexCaps()
	for i := range caps.ConfigOptions {
		if caps.ConfigOptions[i].ID == "collaboration_mode" {
			caps.ConfigOptions[i].CurrentValue = "plan"
		}
	}
	s := codexAdapter{}.Settings(caps)
	if !s.PlanOn {
		t.Fatal("planOn should be true")
	}
	// codex 的 plan 是独立配置项，不占权限档——档位保持可读。
	if s.CurrentLevel != AccessAutoEdit {
		t.Fatalf("currentLevel = %q", s.CurrentLevel)
	}
}

func TestGenericSettingsByCategory(t *testing.T) {
	// generic 只按 category 惯例试探，id 是什么无所谓。
	caps := Caps{ConfigOptions: []ConfigOption{
		{ID: "llm", Category: "model", Type: "select", CurrentValue: "m1", Options: []ConfigOptionValue{
			{Value: "m1", Name: "Model One"},
		}},
		{ID: "depth", Category: "thought_level", Type: "select", CurrentValue: "weird", Options: []ConfigOptionValue{
			{Value: "low", Name: "Low"},
			{Value: "weird", Name: "Weird"},
		}},
	}}

	s := genericAdapter{}.Settings(caps)
	if len(s.Models) != 1 || s.CurrentModel != "m1" {
		t.Fatalf("models = %+v current = %q", s.Models, s.CurrentModel)
	}
	// 词汇表外的值只丢值不丢维度；当前值不在词汇表内时置空。
	if !reflect.DeepEqual(s.Efforts, []Effort{EffortLow}) {
		t.Fatalf("efforts = %v", s.Efforts)
	}
	if s.CurrentEffort != "" {
		t.Fatalf("currentEffort = %q, want empty", s.CurrentEffort)
	}
	if len(s.Levels) != 0 || s.PlanSupported || s.FastSupported {
		t.Fatalf("generic 不该猜 levels/plan/fast: %+v", s)
	}
}

func TestFlavorOf(t *testing.T) {
	cases := []struct {
		agentName, command string
		want               Flavor
	}{
		{"@agentclientprotocol/claude-agent-acp", "claude-agent-acp", FlavorClaude},
		{"", "claude-agent-acp", FlavorClaude},
		{"Codex", "codex-acp", FlavorCodex},
		{"", "/usr/local/bin/codex-acp", FlavorCodex},
		{"gemini-cli", "gemini", FlavorGeneric},
	}
	for _, c := range cases {
		if got := flavorOf(c.agentName, c.command); got != c.want {
			t.Errorf("flavorOf(%q, %q) = %q, want %q", c.agentName, c.command, got, c.want)
		}
	}
}
