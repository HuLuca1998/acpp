package discord

import (
	"encoding/json"
	"fmt"
	"strings"
)

// 本文件是提问的形状层：requestedSchema → 题目列表 → Components V2 逐题卡
//（用户定稿的布局：标题+已答摘要、题干、每选项一行 Section 右侧 ✓/○ 按钮、
// 自由输入行、⬅️➡️提交导航条）。解析口径与 web 端 lib/elicitation.ts 一致。

// elicitQuestion 是解析后的一道题。
type elicitQuestion struct {
	ID          string
	Title       string
	Description string
	Required    bool
	// Multiple 表示多选题（schema 是 array + items 选项集）：答案是集合，
	// 交互上点选是勾选/取消，不自动前进。
	Multiple bool
	Options  []elicitOption
	// OtherField 非空表示这道题带自由输入栏，答案要落到这个字段上。
	OtherField string
}

// elicitOption 是 schema 里的一个选项形状（Const 是值，Description 是说明）。
type elicitOption struct {
	Const       string `json:"const"`
	Description string `json:"description"`
}

// optionValues 取一道题的全部选项值。
func optionValues(q elicitQuestion) []string {
	out := make([]string, 0, len(q.Options))
	for _, o := range q.Options {
		out = append(out, o.Const)
	}
	return out
}

// parseElicitSchema 把 requestedSchema 解析成题目列表。形状是 2026-08 从
// 真实转录抓的（claude 的 AskUserQuestion）：单选是顶层 oneOf；多选是
// `type:"array"` + `items.anyOf`（oneOf/enum 一并兜底）；自由输入字段
// 有三种标记——claude 新版 `_meta._askUserQuestionCustomAnswer`、claude
// 旧版 `<题目>_custom` 命名、codex `_meta.codex.isOtherAnswer`。
func parseElicitSchema(raw json.RawMessage) ([]elicitQuestion, error) {
	var schema struct {
		Properties map[string]struct {
			Type        string         `json:"type"`
			Title       string         `json:"title"`
			Description string         `json:"description"`
			OneOf       []elicitOption `json:"oneOf"`
			Items       *struct {
				AnyOf []elicitOption `json:"anyOf"`
				OneOf []elicitOption `json:"oneOf"`
				Enum  []string       `json:"enum"`
			} `json:"items"`
			Meta struct {
				Codex struct {
					IsOtherAnswer bool   `json:"isOtherAnswer"`
					QuestionID    string `json:"questionId"`
				} `json:"codex"`
				AskCustom struct {
					IsCustomAnswer bool   `json:"isCustomAnswer"`
					QuestionID     string `json:"questionId"`
				} `json:"_askUserQuestionCustomAnswer"`
			} `json:"_meta"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("解 requestedSchema: %w", err)
	}

	required := map[string]bool{}
	for _, k := range schema.Required {
		required[k] = true
	}
	others := map[string]string{} // 题目 id → 自由输入字段名
	isOther := func(key string) (string, bool) {
		prop := schema.Properties[key]
		if prop.Meta.Codex.IsOtherAnswer {
			return prop.Meta.Codex.QuestionID, true
		}
		if prop.Meta.AskCustom.IsCustomAnswer {
			return prop.Meta.AskCustom.QuestionID, true
		}
		if target, ok := strings.CutSuffix(key, "_custom"); ok {
			if _, exists := schema.Properties[target]; exists {
				return target, true
			}
		}
		return "", false
	}
	for key := range schema.Properties {
		if target, ok := isOther(key); ok && target != "" {
			others[target] = key
		}
	}

	var qs []elicitQuestion
	for key, prop := range schema.Properties {
		if _, ok := isOther(key); ok {
			continue
		}
		q := elicitQuestion{
			ID: key, Title: prop.Title, Description: prop.Description,
			Required: required[key], OtherField: others[key],
		}
		if q.Title == "" {
			q.Title = key
		}
		if prop.Type == "array" && prop.Items != nil {
			q.Multiple = true
			for _, list := range [][]elicitOption{prop.Items.AnyOf, prop.Items.OneOf} {
				for _, o := range list {
					if o.Const != "" {
						q.Options = append(q.Options, o)
					}
				}
			}
			for _, e := range prop.Items.Enum {
				if e != "" {
					q.Options = append(q.Options, elicitOption{Const: e})
				}
			}
		} else {
			for _, o := range prop.OneOf {
				if o.Const != "" {
					q.Options = append(q.Options, o)
				}
			}
		}
		qs = append(qs, q)
	}
	return qs, nil
}

// ---- Components V2 渲染（布局由用户以 JSON 定稿）----

// v2Text 是一个 Text Display 组件。
func v2Text(content string) map[string]any {
	return map[string]any{"type": 10, "content": content}
}

// v2Sep 是分隔线。
func v2Sep() map[string]any {
	return map[string]any{"type": 14, "divider": true, "spacing": 1}
}

// v2Section 是「左文字 + 右按钮」的一行。
func v2Section(content string, button map[string]any) map[string]any {
	return map[string]any{
		"type": 9, "components": []map[string]any{v2Text(content)},
		"accessory": button,
	}
}

// v2Container 把内容包进带色条的容器（消息顶层组件）。
func v2Container(color int, inner []map[string]any) []map[string]any {
	return []map[string]any{{"type": 17, "accent_color": color, "components": inner}}
}

// answeredLine 是一道已答题的摘要行：「✅ ~~题~~　**答案**」。
func answeredLine(q elicitQuestion, answers []string) string {
	return "✅ ~~" + trimRunes(q.Title, 60) + "~~　**" + trimRunes(strings.Join(answers, "、"), 160) + "**"
}

// elicitClosedV2 是提问的终态卡：逐题「✅ ~~题~~　答案」，记录留在对话里。
func elicitClosedV2(ask *pendingAsk, answers map[string][]string, by string) []map[string]any {
	var inner []map[string]any
	inner = append(inner, v2Text("### ✅ 已回答"))
	if ask.title != "" {
		inner = append(inner, v2Text("-# "+trimRunes(ask.title, 300)))
	}
	for _, q := range ask.questions {
		vals := append([]string(nil), answers[q.ID]...)
		if q.OtherField != "" {
			vals = append(vals, answers[q.OtherField]...)
		}
		if len(vals) == 0 {
			vals = []string{"—"}
		}
		inner = append(inner, v2Text(answeredLine(q, vals)))
	}
	inner = append(inner, v2Text("-# "+by))
	return v2Container(colorGreen, inner)
}

// askReady 报告可否提交：必答题全部有答案，且至少答过一题。
func askReady(ask *pendingAsk) bool {
	answered := 0
	for _, q := range ask.questions {
		if len(answersOf(ask, q)) > 0 {
			answered++
		} else if q.Required {
			return false
		}
	}
	return answered > 0
}

// answersOf 取一道题的已存答案集合（选项集 + 自由输入两个字段合并）。
func answersOf(ask *pendingAsk, q elicitQuestion) []string {
	out := append([]string(nil), ask.partial[q.ID]...)
	if q.OtherField != "" {
		out = append(out, ask.partial[q.OtherField]...)
	}
	return out
}

// ---- 一次性表单（用户定稿：不逐题翻页，一个 modal 填完全部）----

// askIntroCard 是提问的入口卡：modal 只能由点击触发，这张卡就是那个入口。
func askIntroCard(ask *pendingAsk) []map[string]any {
	var inner []map[string]any
	inner = append(inner, v2Text(fmt.Sprintf("### ❓ agent 有问题问你（%d 题）", len(ask.questions))))
	if ask.title != "" {
		inner = append(inner, v2Text(trimRunes(ask.title, 500)))
	}
	var lines []string
	for i, q := range ask.questions {
		kind := ""
		if q.Multiple {
			kind = "（多选）"
		}
		lines = append(lines, fmt.Sprintf("%d. %s%s", i+1, trimRunes(q.Title, 80), kind))
	}
	inner = append(inner, v2Text(strings.Join(lines, "\n")))
	label := "📝 填表回答"
	if formPages(ask.questions) > 1 {
		label = fmt.Sprintf("📝 填表回答（分 %d 页）", formPages(ask.questions))
	}
	inner = append(inner, map[string]any{"type": 1, "components": []map[string]any{{
		"type": 2, "style": 1, "label": label,
		"custom_id": "eb:" + ask.nonce + ":0",
	}}})
	return v2Container(colorBlurbe, inner)
}

// modal 一屏最多 5 个顶层组件（平台上限）。现在每题只占一个组件
// （单选 RadioGroup / 多选 CheckboxGroup / 纯输入 TextInput），claude
// 单次最多 4 题——常态一页全装下，只有 >5 题才分页兜底。
const formPageSize = 5

func formPages(qs []elicitQuestion) int {
	return (len(qs) + formPageSize - 1) / formPageSize
}

// formModal 拼第 page 页的表单。一页装不完时页内塞满 5 题，提交后经
// 「继续填写」按钮翻下一页（modal 提交后不能直接再弹 modal，平台规则）。
// 空余组件位分给带自由输入的题（每题一个「其他」输入框，先到先得）。
func formModal(ask *pendingAsk, page int) map[string]any {
	start := page * formPageSize
	end := min(start+formPageSize, len(ask.questions))
	pageQs := ask.questions[start:end]

	var comps []map[string]any
	for _, q := range pageQs {
		comps = append(comps, formQuestion(q))
	}
	// 空位分给「其他」输入框：选项外的自定义答案有地方写。
	for _, q := range pageQs {
		if len(comps) >= formPageSize {
			break
		}
		if q.OtherField != "" && len(q.Options) > 0 {
			comps = append(comps, map[string]any{
				"type": 18, "label": trimRunes("其他（"+q.Title+"）", 45),
				"description": "上面选项都不合适时填这里",
				"component": map[string]any{
					"type": 4, "custom_id": q.OtherField, "style": 2, "required": false,
					"placeholder": "自定义回答（可留空）…",
				},
			})
		}
	}

	title := "agent 的问题"
	if formPages(ask.questions) > 1 {
		title = fmt.Sprintf("agent 的问题（%d/%d 页）", page+1, formPages(ask.questions))
	}
	return map[string]any{
		"custom_id":  fmt.Sprintf("em:%s:%d", ask.nonce, page),
		"title":      title,
		"components": comps,
	}
}

// formQuestion 把一道题变成一个表单组件：多选 CheckboxGroup(22)、
// 单选 ≤10 项 RadioGroup(21)、更多 String Select(3)、纯输入 TextInput(4)。
func formQuestion(q elicitQuestion) map[string]any {
	label := map[string]any{"type": 18, "label": trimRunes(q.Title, 45)}
	if q.Description != "" {
		label["description"] = trimRunes(q.Description, 100)
	}
	if len(q.Options) == 0 {
		label["component"] = map[string]any{
			"type": 4, "custom_id": q.ID, "style": 2, "required": q.Required,
			"placeholder": "在这里输入…",
		}
		return label
	}
	opts := make([]map[string]any, 0, len(q.Options))
	for _, o := range q.Options {
		opt := map[string]any{"label": trimRunes(o.Const, 90), "value": trimRunes(o.Const, 90)}
		if o.Description != "" {
			opt["description"] = trimRunes(o.Description, 90)
		}
		opts = append(opts, opt)
	}
	kind := 21 // RadioGroup
	if q.Multiple {
		kind = 22 // CheckboxGroup
	} else if len(q.Options) > 10 {
		kind = 3 // String Select
	}
	comp := map[string]any{"type": kind, "custom_id": q.ID, "options": opts, "required": q.Required}
	if kind == 3 {
		comp["placeholder"] = "选一个…"
	}
	label["component"] = comp
	return label
}
