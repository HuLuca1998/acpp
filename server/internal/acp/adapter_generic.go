package acp

import "context"

// genericAdapter 是认不出方言的 runtime 的降级实现。模型与思考深度按
// category 试探（"model" / "thought_level" 的 category 在两个已知 runtime
// 上一致，是 ACP 生态惯例，守约的新 runtime 自动工作）；权限档、plan、
// fast 的语义无从推断，一律报不支持——不猜语义。
type genericAdapter struct{}

func (genericAdapter) Flavor() Flavor { return FlavorGeneric }

func (genericAdapter) Settings(caps Caps) Settings {
	s := Settings{Flavor: FlavorGeneric}
	s.Models, s.CurrentModel = modelsFrom(optionByCategory(caps, "model"))
	s.Efforts, s.CurrentEffort = effortsFrom(optionByCategory(caps, "thought_level"))
	return s
}

func (genericAdapter) SetModel(ctx context.Context, s *Session, id string) error {
	opt := optionByCategory(s.Caps(), "model")
	if opt == nil {
		return ErrUnsupported
	}
	return s.setConfigOption(ctx, opt.ID, id)
}

func (genericAdapter) SetEffort(ctx context.Context, s *Session, e Effort) error {
	opt := optionByCategory(s.Caps(), "thought_level")
	if opt == nil {
		return ErrUnsupported
	}
	return s.setConfigOption(ctx, opt.ID, string(e))
}

func (genericAdapter) SetAccessLevel(context.Context, *Session, AccessLevel) error {
	return ErrUnsupported
}

func (genericAdapter) SetPlan(context.Context, *Session, bool) error {
	return ErrUnsupported
}

func (genericAdapter) SetFast(context.Context, *Session, bool) error {
	return ErrUnsupported
}
