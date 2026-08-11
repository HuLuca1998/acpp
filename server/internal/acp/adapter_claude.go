package acp

import (
	"context"
	"encoding/json"
)

// claudeAdapter 对接 claude-agent-acp。权限档与 plan 都住在 session modes 里；
// 模型/思考深度/fast 是 configOptions。
type claudeAdapter struct{}

// claudeLevels 是统一权限档到 claude modeId 的映射。claude 的 auto（分类器
// 裁决）与 dontAsk 档不在词汇表内，按交集规范不暴露。
var claudeLevels = []levelPair{
	{AccessSafe, "default"},
	{AccessAutoEdit, "acceptEdits"},
	{AccessFull, "bypassPermissions"},
}

func (claudeAdapter) Flavor() Flavor { return FlavorClaude }

func (claudeAdapter) Settings(caps Caps) Settings {
	s := Settings{Flavor: FlavorClaude}
	s.Models, s.CurrentModel = modelsFrom(optionByID(caps, "model"))
	s.Efforts, s.CurrentEffort = effortsFrom(optionByID(caps, "effort"))
	s.Levels, s.CurrentLevel = levelsFrom(caps.Modes, claudeLevels)
	s.FastSupported, s.FastOn = boolFrom(optionByID(caps, "fast"))

	// plan 在 claude 是一个权限档；处于 plan 时统一视图的 CurrentLevel 为空。
	if caps.Modes != nil {
		for _, m := range caps.Modes.AvailableModes {
			if m.ID == "plan" {
				s.PlanSupported = true
				break
			}
		}
		s.PlanOn = caps.Modes.CurrentModeID == "plan"
	}
	return s
}

func (claudeAdapter) SetModel(ctx context.Context, s *Session, id string) error {
	return s.setConfigOption(ctx, "model", id)
}

func (claudeAdapter) SetEffort(ctx context.Context, s *Session, e Effort) error {
	return s.setConfigOption(ctx, "effort", string(e))
}

func (claudeAdapter) SetAccessLevel(ctx context.Context, s *Session, l AccessLevel) error {
	modeID, ok := modeIDForLevel(claudeLevels, l)
	if !ok {
		return ErrUnsupported
	}
	return s.setMode(ctx, modeID)
}

// SetPlan 进入 plan 档；退出时回 default（safe）——不记忆进入前的档位，
// 与自动放行策略下 ExitPlanMode 选 allow_once（=default）的行为一致。
func (claudeAdapter) SetPlan(ctx context.Context, s *Session, on bool) error {
	if on {
		return s.setMode(ctx, "plan")
	}
	return s.setMode(ctx, "default")
}

func (claudeAdapter) SetFast(ctx context.Context, s *Session, on bool) error {
	return s.setConfigOption(ctx, "fast", onOff(on))
}

// PlanReview 识别 ExitPlanMode：kind=switch_mode 的权限请求，rawInput.plan
// 是 markdown 计划全文（实测形状见 docs/adr-001）。选项 optionId 是 claude
// 的模式名，翻译进统一三档；词汇表外的档（auto）丢弃；reject 类选项
//（"No, keep planning"）翻译为 Level 为空的「继续规划」。
func (claudeAdapter) PlanReview(p RequestPermissionParams) *PlanReview {
	if p.ToolCall.Kind != "switch_mode" {
		return nil
	}
	var input struct {
		Plan string `json:"plan"`
	}
	if err := json.Unmarshal(p.ToolCall.RawInput, &input); err != nil || input.Plan == "" {
		return nil
	}

	review := &PlanReview{Plan: input.Plan, Choices: []PlanChoice{}}
	for _, opt := range p.Options {
		if opt.Kind == "reject_once" || opt.Kind == "reject_always" {
			review.Choices = append(review.Choices, PlanChoice{OptionID: opt.OptionID})
			continue
		}
		for _, pair := range claudeLevels {
			if opt.OptionID == pair.ModeID {
				review.Choices = append(review.Choices, PlanChoice{
					OptionID: opt.OptionID,
					Level:    pair.Level,
				})
				break
			}
		}
	}
	return review
}
