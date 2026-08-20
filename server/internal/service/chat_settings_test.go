package service

import (
	"testing"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
)

// claudeFresh 是一条 claude 会话刚被重新拉起时的统一设置视图：
// 模型与档位都停在 agent 的默认值上，用户设过的东西一样不剩。
func claudeFresh() acp.Settings {
	return acp.Settings{
		Flavor: acp.FlavorClaude,
		Models: []acp.Model{
			{ID: "sonnet", Name: "Sonnet"},
			{ID: "opus", Name: "Opus"},
		},
		CurrentModel:  "sonnet",
		Efforts:       []acp.Effort{acp.EffortLow, acp.EffortMedium, acp.EffortHigh, acp.EffortXHigh, acp.EffortMax},
		CurrentEffort: acp.EffortMedium,
		Levels:        []acp.AccessLevel{acp.AccessSafe, acp.AccessAutoEdit, acp.AccessFull},
		CurrentLevel:  acp.AccessSafe,
		PlanSupported: true,
		FastSupported: true,
	}
}

// 空闲回收后重开，快照里与新进程默认值不同的项都要被拨回去——
// 这正是「用户没操作，模型和权限档却自己变了」的修复点。
func TestSettingsRestorePatchAppliesSnapshot(t *testing.T) {
	last := model.JSONMap{
		"model": "opus", "effort": "xhigh", "level": "full", "plan": true, "fast": true,
	}

	patch, ok := settingsRestorePatch(claudeFresh(), last)
	if !ok {
		t.Fatal("快照与默认值处处不同，应当有要下发的项")
	}
	if patch.Model == nil || *patch.Model != "opus" {
		t.Errorf("Model = %v，期望 opus", patch.Model)
	}
	if patch.Effort == nil || *patch.Effort != acp.EffortXHigh {
		t.Errorf("Effort = %v，期望 xhigh", patch.Effort)
	}
	if patch.Level == nil || *patch.Level != acp.AccessFull {
		t.Errorf("Level = %v，期望 full", patch.Level)
	}
	if patch.Plan == nil || !*patch.Plan {
		t.Errorf("Plan = %v，期望 true——plan 关掉意味着 agent 直接动手改代码，比其它项更不能丢", patch.Plan)
	}
	if patch.Fast == nil || !*patch.Fast {
		t.Errorf("Fast = %v，期望 true", patch.Fast)
	}
}

// 已经一致的项不下发：session/load 可能已恢复了一部分，重设一遍是白跑的 RPC。
func TestSettingsRestorePatchSkipsUnchanged(t *testing.T) {
	current := claudeFresh()
	last := model.JSONMap{
		"model":  current.CurrentModel,
		"effort": string(current.CurrentEffort),
		"level":  string(current.CurrentLevel),
		"plan":   current.PlanOn,
		"fast":   current.FastOn,
	}

	patch, ok := settingsRestorePatch(current, last)
	if ok {
		t.Errorf("快照与当前值完全一致，不该有任何下发项，实际 %+v", patch)
	}
}

// 快照里的值新进程不认（模型下线、档位被 runtime 禁用）时必须丢掉这一项。
// Apply 是逐项串行、遇错即停的：留着它会让排在后面的档位统统不生效。
func TestSettingsRestorePatchDropsUnsupported(t *testing.T) {
	current := claudeFresh()
	current.Levels = []acp.AccessLevel{acp.AccessSafe, acp.AccessAutoEdit} // full 被禁用
	last := model.JSONMap{
		"model":  "gpt-5.6-sol", // 换 agent 后遗留的 codex 模型 id
		"level":  "full",
		"effort": "xhigh",
	}

	patch, ok := settingsRestorePatch(current, last)
	if !ok {
		t.Fatal("effort 有效，应当仍有要下发的项")
	}
	if patch.Model != nil {
		t.Errorf("Model = %v，清单里没有的模型必须丢弃", *patch.Model)
	}
	if patch.Level != nil {
		t.Errorf("Level = %v，runtime 已禁用的档位必须丢弃", *patch.Level)
	}
	if patch.Effort == nil || *patch.Effort != acp.EffortXHigh {
		t.Errorf("Effort = %v，有效项不该被牵连", patch.Effort)
	}
}

// runtime 不支持的维度不下发：codex 没有 plan/fast 这类开关时，
// 快照里的残留值不能变成一次注定失败的调用。
func TestSettingsRestorePatchSkipsUnsupportedDimensions(t *testing.T) {
	current := claudeFresh()
	current.PlanSupported, current.FastSupported = false, false
	last := model.JSONMap{"plan": true, "fast": true}

	patch, ok := settingsRestorePatch(current, last)
	if ok {
		t.Errorf("维度都不支持，不该有下发项，实际 %+v", patch)
	}
}

// 布尔项要能区分「快照记着关」和「快照里根本没这一项」：
// 前者要下发（把 agent 默认打开的 plan 关掉），后者不能凭零值瞎设。
func TestSettingsRestorePatchDistinguishesFalseFromAbsent(t *testing.T) {
	current := claudeFresh()
	current.PlanOn, current.FastOn = true, true

	patch, ok := settingsRestorePatch(current, model.JSONMap{"plan": false})
	if !ok || patch.Plan == nil || *patch.Plan {
		t.Errorf("Plan = %v（ok=%v），快照明确记着关就要下发 false", patch.Plan, ok)
	}
	if patch.Fast != nil {
		t.Errorf("Fast = %v，快照里没有这一项就不该动它", *patch.Fast)
	}
}

// 没有快照的会话（新建后从没改过设置）不该产生任何调用。
func TestSettingsRestorePatchEmptySnapshot(t *testing.T) {
	if patch, ok := settingsRestorePatch(claudeFresh(), model.JSONMap{}); ok {
		t.Errorf("空快照不该有下发项，实际 %+v", patch)
	}
}
