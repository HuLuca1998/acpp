package discord

import (
	"encoding/json"
	"fmt"
	"slices"
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

// questionCardV2 是逐题卡：标题与已答摘要在头部，当前题的选项每行一个
// Section（右侧 ✓/○ 按钮，多选 toggle），自由输入一行，底部 ⬅️➡️提交。
func questionCardV2(ask *pendingAsk) []map[string]any {
	cur := ask.questions[ask.cursor]
	sel := answersOf(ask, cur)
	values := optionValues(cur)

	var inner []map[string]any
	inner = append(inner, v2Text(fmt.Sprintf("### ❓ agent 有问题问你　·　%d/%d", ask.cursor+1, len(ask.questions))))
	if ask.title != "" {
		inner = append(inner, v2Text("-# "+trimRunes(ask.title, 300)))
	}
	for i, q := range ask.questions {
		if i == ask.cursor {
			continue
		}
		if a := answersOf(ask, q); len(a) > 0 {
			inner = append(inner, v2Text(answeredLine(q, a)))
		}
	}
	inner = append(inner, v2Sep())

	title := "**" + trimRunes(cur.Title, 200)
	if cur.Description != "" {
		title += " — " + trimRunes(cur.Description, 200)
	}
	title += "**"
	if cur.Multiple {
		title += "（可多选）"
	}
	inner = append(inner, v2Text(title))

	for i, o := range cur.Options {
		picked := slices.Contains(sel, o.Const)
		label := fmt.Sprintf("%d · %s", i+1, trimRunes(o.Const, 120))
		if picked {
			label = "**" + label + "**"
		}
		if o.Description != "" {
			label += "\n-# " + trimRunes(o.Description, 140)
		}
		btn := map[string]any{
			"type": 2, "style": 2, "label": "○",
			"custom_id": fmt.Sprintf("ea:%s:%d", ask.nonce, i),
		}
		if picked {
			btn["style"] = 3
			btn["label"] = "✓"
		}
		inner = append(inner, v2Section(label, btn))
	}

	// 自由输入行：已输入的显示出来（多选时与选项并存）。
	var free []string
	for _, a := range sel {
		if !slices.Contains(values, a) {
			free = append(free, a)
		}
	}
	freeLabel := "自由输入"
	if len(free) > 0 {
		freeLabel = "✍️ **" + trimRunes(strings.Join(free, "、"), 160) + "**"
	}
	inner = append(inner, v2Section(freeLabel, map[string]any{
		"type": 2, "style": 2, "label": "✍️", "custom_id": "ei:" + ask.nonce,
	}))

	inner = append(inner, v2Sep())

	answered := 0
	for _, q := range ask.questions {
		if len(answersOf(ask, q)) > 0 {
			answered++
		}
	}
	inner = append(inner, map[string]any{"type": 1, "components": []map[string]any{
		{"type": 2, "style": 2, "label": "⬅️", "custom_id": "en:" + ask.nonce + ":p",
			"disabled": ask.cursor == 0},
		{"type": 2, "style": 2, "label": "➡️", "custom_id": "en:" + ask.nonce + ":n",
			"disabled": ask.cursor == len(ask.questions)-1},
		{"type": 2, "style": 3, "label": fmt.Sprintf("提交（%d/%d）", answered, len(ask.questions)),
			"custom_id": "ez:" + ask.nonce, "disabled": !askReady(ask)},
	}})
	return v2Container(colorBlurbe, inner)
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

// inputModal 是自由输入的单题小表单（em:<nonce> 提交，答的是当前题）。
func inputModal(ask *pendingAsk) map[string]any {
	cur := ask.questions[ask.cursor]
	return map[string]any{
		"custom_id": "em:" + ask.nonce,
		"title":     trimRunes(cur.Title, 45),
		"components": []map[string]any{{
			"type": 18, "label": trimRunes(cur.Title, 45),
			"component": map[string]any{
				"type": 4, "custom_id": "answer", "style": 2, "required": true,
				"placeholder": "在这里输入…",
			},
		}},
	}
}
