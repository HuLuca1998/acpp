package acp

import (
	"context"
	"errors"
	"strings"
)

// ErrUnsupported 表示该 runtime 不支持这个统一设置维度。
var ErrUnsupported = errors.New("acp: setting not supported by this runtime")

// Flavor 标识 runtime 方言，决定用哪个 adapter 实现。
type Flavor string

const (
	FlavorCodex   Flavor = "codex"
	FlavorClaude  Flavor = "claude"
	FlavorGeneric Flavor = "generic"
)

// flavorOf 用 initialize 返回的 agent 名与启动命令双信号识别方言。
// 认不出的一律走 generic：按 category 试探、其余透传，不猜语义。
func flavorOf(agentName, command string) Flavor {
	s := strings.ToLower(agentName + " " + command)
	switch {
	case strings.Contains(s, "claude"):
		return FlavorClaude
	case strings.Contains(s, "codex"):
		return FlavorCodex
	default:
		return FlavorGeneric
	}
}

func adapterFor(f Flavor) Adapter {
	switch f {
	case FlavorClaude:
		return claudeAdapter{}
	case FlavorCodex:
		return codexAdapter{}
	default:
		return genericAdapter{}
	}
}

// Effort 是统一思考深度，五档——恰好是两端 runtime 选项的交集。
// 词汇表外的值（codex 的 ultra、claude 的 default）不进统一视图。
type Effort string

const (
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
	EffortXHigh  Effort = "xhigh"
	EffortMax    Effort = "max"
)

// effortVocabulary 兼作词汇表与展示顺序。
var effortVocabulary = []Effort{EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}

// AccessLevel 是统一权限档，三档，两端 runtime 全覆盖（交集规范）。
// 上层永远不出现 "acceptEdits" / "agent-full-access" 这类 runtime 字面量。
type AccessLevel string

const (
	// AccessSafe 写操作逐项确认（claude default / codex read-only）。
	AccessSafe AccessLevel = "safe"
	// AccessAutoEdit 自动接受编辑，危险命令仍拦（claude acceptEdits / codex agent）。
	AccessAutoEdit AccessLevel = "auto-edit"
	// AccessFull 跳过一切检查（claude bypassPermissions / codex agent-full-access）。
	AccessFull AccessLevel = "full"
)

// Model 是统一模型描述。ID 是 runtime 自己的标识，透传不映射。
type Model struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Settings 是会话设置的统一视图。空切片表示该 runtime 不支持这个维度，
// 前端据此隐藏控件；Current* 为空表示当前值不在统一词汇表内。
// 词汇表外的配置项（如 claude 的 agent persona）按交集规范不透传。
type Settings struct {
	Flavor        Flavor        `json:"flavor"`
	Models        []Model       `json:"models"`
	CurrentModel  string        `json:"currentModel,omitempty"`
	Efforts       []Effort      `json:"efforts"`
	CurrentEffort Effort        `json:"currentEffort,omitempty"`
	Levels        []AccessLevel `json:"levels"`
	CurrentLevel  AccessLevel   `json:"currentLevel,omitempty"`
	PlanSupported bool          `json:"planSupported"`
	PlanOn        bool          `json:"planOn"`
	FastSupported bool          `json:"fastSupported"`
	FastOn        bool          `json:"fastOn"`
}

// SettingsPatch 是逐项可选的设置变更，nil 字段不动。
type SettingsPatch struct {
	Model  *string      `json:"model,omitempty"`
	Effort *Effort      `json:"effort,omitempty"`
	Level  *AccessLevel `json:"level,omitempty"`
	Plan   *bool        `json:"plan,omitempty"`
	Fast   *bool        `json:"fast,omitempty"`
}

// PlanReview 是「计划完成，请求开始执行」的统一视图（claude 的
// ExitPlanMode 权限请求）。Plan 是 markdown 计划全文；Choices 是批准后
// 的执行档（词汇表外的档位已被 adapter 丢弃）。
type PlanReview struct {
	Plan    string       `json:"plan"`
	Choices []PlanChoice `json:"choices"`
}

// PlanChoice 是计划审批的一个去向。Level 为空表示「继续规划」（拒绝）。
type PlanChoice struct {
	// OptionID 原样回传给 agent，上层不解释。
	OptionID string      `json:"optionId"`
	Level    AccessLevel `json:"level,omitempty"`
}

// Adapter 把一个 runtime 的语义差异封装在实现内部：读方法从能力快照提取
// 统一视图，写方法把统一概念翻译成该 runtime 的协议调用。
// `if flavor == ...` 只允许出现在 adapter 实现文件里。
type Adapter interface {
	Flavor() Flavor
	Settings(caps Caps) Settings

	SetModel(ctx context.Context, s *Session, id string) error
	SetEffort(ctx context.Context, s *Session, e Effort) error
	SetAccessLevel(ctx context.Context, s *Session, l AccessLevel) error
	SetPlan(ctx context.Context, s *Session, on bool) error
	SetFast(ctx context.Context, s *Session, on bool) error

	// PlanReview 识别「计划完成」权限请求并翻译成统一视图；
	// 不是这类请求（或该 runtime 没有此交互）时返回 nil。
	PlanReview(p RequestPermissionParams) *PlanReview
}

// ---- adapter 共用的取数工具 ----

func optionByID(caps Caps, id string) *ConfigOption {
	for i := range caps.ConfigOptions {
		if caps.ConfigOptions[i].ID == id {
			return &caps.ConfigOptions[i]
		}
	}
	return nil
}

func optionByCategory(caps Caps, category string) *ConfigOption {
	for i := range caps.ConfigOptions {
		if caps.ConfigOptions[i].Category == category {
			return &caps.ConfigOptions[i]
		}
	}
	return nil
}

// modelsFrom 把一个 select 型配置项翻译成统一模型清单与当前值。
// 各提取函数都返回非 nil 切片：JSON 里必须是 []，null 会让前端炸掉。
func modelsFrom(opt *ConfigOption) ([]Model, string) {
	if opt == nil {
		return []Model{}, ""
	}
	models := make([]Model, 0, len(opt.Options))
	for _, v := range opt.Options {
		models = append(models, Model{ID: v.Value, Name: v.Name, Description: v.Description})
	}
	return models, opt.CurrentValue
}

// effortsFrom 取配置项选项值与统一词汇表的交集，保持词汇表顺序。
// 当前值不在词汇表内（如 claude 的 default）时 current 为空。
func effortsFrom(opt *ConfigOption) ([]Effort, Effort) {
	if opt == nil {
		return []Effort{}, ""
	}
	available := make(map[string]bool, len(opt.Options))
	for _, v := range opt.Options {
		available[v.Value] = true
	}
	efforts := make([]Effort, 0, len(effortVocabulary))
	for _, e := range effortVocabulary {
		if available[string(e)] {
			efforts = append(efforts, e)
		}
	}
	var current Effort
	for _, e := range efforts {
		if string(e) == opt.CurrentValue {
			current = e
			break
		}
	}
	return efforts, current
}

// levelPair 是统一权限档与 runtime modeId 的一条映射。
type levelPair struct {
	Level  AccessLevel
	ModeID string
}

// levelsFrom 按映射表提取该会话实际可用的统一档与当前档。
// modes 里没有的档不列出（比如 bypass 被 runtime 禁用时）。
func levelsFrom(modes *Modes, table []levelPair) ([]AccessLevel, AccessLevel) {
	if modes == nil {
		return []AccessLevel{}, ""
	}
	available := make(map[string]bool, len(modes.AvailableModes))
	for _, m := range modes.AvailableModes {
		available[m.ID] = true
	}
	levels := make([]AccessLevel, 0, len(table))
	var current AccessLevel
	for _, p := range table {
		if !available[p.ModeID] {
			continue
		}
		levels = append(levels, p.Level)
		if p.ModeID == modes.CurrentModeID {
			current = p.Level
		}
	}
	return levels, current
}

func modeIDForLevel(table []levelPair, l AccessLevel) (string, bool) {
	for _, p := range table {
		if p.Level == l {
			return p.ModeID, true
		}
	}
	return "", false
}

// boolFrom 读 on/off 型配置项。
func boolFrom(opt *ConfigOption) (supported, on bool) {
	if opt == nil {
		return false, false
	}
	return true, opt.CurrentValue == "on"
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

