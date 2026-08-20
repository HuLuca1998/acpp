package acp

import "context"

// codexAdapter 对接 codex-acp。权限档住在 session modes；plan 是独立配置项
// collaboration_mode；模型/思考深度/fast 是 configOptions。
// codex 顶层 models（模型×推理档笛卡尔积）与 session/set_model 都不用——
// 后者与 set_config_option 写同一状态、互相覆盖。
type codexAdapter struct{}

// codexLevels 是统一权限档到 codex modeId 的映射。codex 的 read-only
// （编辑与命令都要批准）语义上就是「写操作逐项确认」，映射到 safe。
var codexLevels = []levelPair{
	{AccessSafe, "read-only"},
	{AccessAutoEdit, "agent"},
	{AccessFull, "agent-full-access"},
}

func (codexAdapter) Flavor() Flavor { return FlavorCodex }

func (codexAdapter) Settings(caps Caps) Settings {
	s := Settings{Flavor: FlavorCodex}
	s.Models, s.CurrentModel = modelsFrom(optionByID(caps, "model"))
	// ultra 档（自动任务委派）不在词汇表内，effortsFrom 按交集自动丢弃。
	s.Efforts, s.CurrentEffort = effortsFrom(optionByID(caps, "reasoning_effort"))
	s.Levels, s.CurrentLevel = levelsFrom(caps.Modes, codexLevels)
	s.FastSupported, s.FastOn = boolFrom(optionByID(caps, "fast-mode"))

	if opt := optionByID(caps, "collaboration_mode"); opt != nil {
		s.PlanSupported = true
		s.PlanOn = opt.CurrentValue == "plan"
	}
	// 同 claude：图片与内嵌上下文是实测能力，按方言兜底；audio 按声明走。
	s.Prompt = caps.Prompt
	s.Prompt.Image = true
	s.Prompt.EmbeddedContext = true
	return s
}

func (codexAdapter) SetModel(ctx context.Context, s *Session, id string) error {
	return s.setConfigOption(ctx, "model", id)
}

func (codexAdapter) SetEffort(ctx context.Context, s *Session, e Effort) error {
	return s.setConfigOption(ctx, "reasoning_effort", string(e))
}

func (codexAdapter) SetAccessLevel(ctx context.Context, s *Session, l AccessLevel) error {
	modeID, ok := modeIDForLevel(codexLevels, l)
	if !ok {
		return ErrUnsupported
	}
	return s.setMode(ctx, modeID)
}

func (codexAdapter) SetPlan(ctx context.Context, s *Session, on bool) error {
	value := "default"
	if on {
		value = "plan"
	}
	return s.setConfigOption(ctx, "collaboration_mode", value)
}

func (codexAdapter) SetFast(ctx context.Context, s *Session, on bool) error {
	return s.setConfigOption(ctx, "fast-mode", onOff(on))
}

// PlanReview：codex 的 plan 是配置项，模型完成计划后正常结束 turn，
// 没有「请求退出计划模式」的交互。
func (codexAdapter) PlanReview(RequestPermissionParams) *PlanReview { return nil }

// Interject 走 _session/steering：注入当前轮，改道后的内容并入当前轮、
// 由当前轮的 prompt 正常收尾。不用第二个 session/prompt——实测 codex
// 会吸收其内容但那个请求的响应永远悬着。
func (codexAdapter) Interject(ctx context.Context, s *Session, blocks []ContentBlock) (PromptResult, bool, error) {
	if err := s.steeringCall(ctx, blocks); err != nil {
		return PromptResult{}, false, err
	}
	return PromptResult{}, false, nil
}
