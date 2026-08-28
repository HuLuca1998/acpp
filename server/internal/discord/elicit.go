package discord

import (
	"encoding/json"
	"fmt"
	"strings"
)

// 本文件是提问表单的形状层：requestedSchema → 题目列表 → 分页 modal。
// 两条 ACP 的自由输入标记不同：codex 用 `_meta.codex.isOtherAnswer`
// （字段名 __other），claude 用 `<题目>_custom` 命名约定——差异吃在
// 解析里，与 web 端 lib/elicitation.ts 同一口径。

// elicitQuestion 是解析后的一道题。
type elicitQuestion struct {
	ID          string
	Title       string
	Description string
	Required    bool
	Options     []string
	// OtherField 非空表示这道题带自由输入栏，答案要落到这个字段上。
	OtherField string
}

// parseElicitSchema 把 requestedSchema 解析成题目列表。
func parseElicitSchema(raw json.RawMessage) ([]elicitQuestion, error) {
	var schema struct {
		Properties map[string]struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			OneOf       []struct {
				Const       string `json:"const"`
				Description string `json:"description"`
			} `json:"oneOf"`
			Meta struct {
				Codex struct {
					IsOtherAnswer bool   `json:"isOtherAnswer"`
					QuestionID    string `json:"questionId"`
				} `json:"codex"`
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
	for key, prop := range schema.Properties {
		if prop.Meta.Codex.IsOtherAnswer && prop.Meta.Codex.QuestionID != "" {
			others[prop.Meta.Codex.QuestionID] = key
			continue
		}
		if target, ok := strings.CutSuffix(key, "_custom"); ok {
			if _, exists := schema.Properties[target]; exists {
				others[target] = key
			}
		}
	}

	var qs []elicitQuestion
	for key, prop := range schema.Properties {
		if prop.Meta.Codex.IsOtherAnswer {
			continue
		}
		if target, ok := strings.CutSuffix(key, "_custom"); ok {
			if _, exists := schema.Properties[target]; exists {
				continue
			}
		}
		q := elicitQuestion{
			ID: key, Title: prop.Title, Description: prop.Description,
			Required: required[key], OtherField: others[key],
		}
		if q.Title == "" {
			q.Title = key
		}
		for _, o := range prop.OneOf {
			if o.Const != "" {
				q.Options = append(q.Options, o.Const)
			}
		}
		qs = append(qs, q)
	}
	return qs, nil
}

// questionCardEmbed 是逐题问答卡的当前形态：已答的题累积显示在正文里，
// 下方是当前题——一张卡从头答到尾，不弹多页表单（用户嫌 modal 翻页繁琐，
// 平台又不允许 modal 内翻页）。
func questionCardEmbed(ask *pendingAsk) map[string]any {
	cur := ask.questions[ask.cursor]
	var lines []string
	if ask.title != "" {
		lines = append(lines, trimRunes(ask.title, 500), "")
	}
	for i := 0; i < ask.cursor; i++ {
		q := ask.questions[i]
		lines = append(lines, "✅ "+trimRunes(q.Title, 100)+"：**"+trimRunes(answerOf(ask, q), 200)+"**")
	}
	if ask.cursor > 0 {
		lines = append(lines, "")
	}
	lines = append(lines, "❓ **"+trimRunes(cur.Title, 240)+"**")
	if cur.Description != "" {
		lines = append(lines, trimRunes(cur.Description, 300))
	}
	hint := "点按钮作答，或直接回复文字。"
	if len(cur.Options) > 0 {
		hint = "点按钮作答；回复编号/文字也行。"
	}
	lines = append(lines, "-# "+hint)
	return map[string]any{
		"title":       fmt.Sprintf("❓ agent 有问题问你（%d/%d）", ask.cursor+1, len(ask.questions)),
		"description": strings.Join(lines, "\n"),
		"color":       colorBlurbe,
	}
}

// questionCardComponents 是当前题的作答组件：≤5 个选项直接按钮，更多换
// 下拉；带自由输入（或纯输入题）附「✍️ 输入」按钮弹单题小表单。
func questionCardComponents(ask *pendingAsk) []map[string]any {
	cur := ask.questions[ask.cursor]
	var rows []map[string]any
	switch {
	case len(cur.Options) == 0:
	case len(cur.Options) <= 5:
		var buttons []map[string]any
		for i, o := range cur.Options {
			buttons = append(buttons, map[string]any{
				"type": 2, "style": 2,
				"label":     trimRunes(fmt.Sprintf("%d · %s", i+1, o), 76),
				"custom_id": fmt.Sprintf("ea:%s:%d", ask.nonce, i),
			})
		}
		rows = append(rows, map[string]any{"type": 1, "components": buttons})
	default:
		opts := make([]choice, 0, len(cur.Options))
		for _, o := range cur.Options {
			opts = append(opts, choice{Label: o, Value: o})
		}
		sel := selectComponent("es:"+ask.nonce, opts, false)
		sel["type"] = 3
		sel["placeholder"] = "选一个…"
		delete(sel, "required")
		rows = append(rows, map[string]any{"type": 1, "components": []map[string]any{sel}})
	}
	if len(cur.Options) == 0 || cur.OtherField != "" {
		rows = append(rows, map[string]any{"type": 1, "components": []map[string]any{{
			"type": 2, "style": 1, "label": "✍️ 输入",
			"custom_id": "ei:" + ask.nonce,
		}}})
	}
	return rows
}

// answerOf 取一道题的已存答案（自由输入落在 OtherField 上的也认）。
func answerOf(ask *pendingAsk, q elicitQuestion) string {
	if a := ask.partial[q.ID]; a != "" {
		return a
	}
	if q.OtherField != "" {
		return ask.partial[q.OtherField]
	}
	return ""
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
