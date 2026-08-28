package discord

import (
	"encoding/json"
	"fmt"
	"slices"
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
	// Multiple 表示多选题（schema 是 array + items 选项集）：答案是集合，
	// 交互上点选是勾选/取消，不自动前进。
	Multiple bool
	Options  []string
	// OtherField 非空表示这道题带自由输入栏，答案要落到这个字段上。
	OtherField string
}

// elicitOption 是 schema 里的一个选项形状。
type elicitOption struct {
	Const       string `json:"const"`
	Description string `json:"description"`
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
						q.Options = append(q.Options, o.Const)
					}
				}
			}
			for _, e := range prop.Items.Enum {
				if e != "" {
					q.Options = append(q.Options, e)
				}
			}
		} else {
			for _, o := range prop.OneOf {
				if o.Const != "" {
					q.Options = append(q.Options, o.Const)
				}
			}
		}
		qs = append(qs, q)
	}
	return qs, nil
}

// questionCardEmbed 是逐题问答卡：正文列当前题与全部选项（单选标 ●，
// 多选标 ☑），下方一行编号按钮 + ⬅️➡️✍️提交导航条。可来回翻题改答案，
// 答完手动提交（用户点名的形态：不自动交，选错随时回去改）。
func questionCardEmbed(ask *pendingAsk) map[string]any {
	cur := ask.questions[ask.cursor]
	sel := answersOf(ask, cur)
	var lines []string
	if ask.title != "" {
		lines = append(lines, trimRunes(ask.title, 400), "")
	}
	kind := ""
	if cur.Multiple {
		kind = "（多选）"
	}
	lines = append(lines, "❓ **"+trimRunes(cur.Title, 240)+"**"+kind)
	if cur.Description != "" {
		lines = append(lines, trimRunes(cur.Description, 300))
	}
	marks := [2]string{"○", "●"}
	if cur.Multiple {
		marks = [2]string{"☐", "☑"}
	}
	var free []string
	for _, a := range sel {
		if !slices.Contains(cur.Options, a) {
			free = append(free, a)
		}
	}
	for i, o := range cur.Options {
		mark := marks[0]
		if slices.Contains(sel, o) {
			mark = marks[1]
		}
		lines = append(lines, fmt.Sprintf("%s **%d** · %s", mark, i+1, trimRunes(o, 90)))
	}
	if len(free) > 0 {
		lines = append(lines, "✍️ 已输入：**"+trimRunes(strings.Join(free, "、"), 200)+"**")
	}
	hint := "点编号选择（自动到下一题）"
	if cur.Multiple {
		hint = "点编号勾选/取消（可多个），选完 ➡️ 下一题"
	}
	lines = append(lines, "-# "+hint+"；⬅️➡️ 翻题可改答案，全部答完按「提交」。打字/回编号也行。")

	answered := 0
	for _, q := range ask.questions {
		if len(answersOf(ask, q)) > 0 {
			answered++
		}
	}
	return map[string]any{
		"title":       fmt.Sprintf("❓ agent 有问题问你（%d/%d）", ask.cursor+1, len(ask.questions)),
		"description": strings.Join(lines, "\n"),
		"color":       colorBlurbe,
		"footer":      map[string]any{"text": fmt.Sprintf("已答 %d/%d 题", answered, len(ask.questions))},
	}
}

// questionCardComponents：编号按钮一行（选中的高亮，>5 个选项换下拉；
// 多选下拉带 max_values），「✍️ 输入」跟在选项后面当追加项（行满或
// 纯输入题落导航条）+ 导航条 [⬅️][➡️][提交]。边界与未答齐用 disabled 表达。
func questionCardComponents(ask *pendingAsk) []map[string]any {
	cur := ask.questions[ask.cursor]
	sel := answersOf(ask, cur)
	inputBtn := map[string]any{
		"type": 2, "style": 2, "label": "✍️ 输入", "custom_id": "ei:" + ask.nonce,
	}
	inputPlaced := false
	var rows []map[string]any
	switch {
	case len(cur.Options) == 0:
	case len(cur.Options) <= 5:
		var buttons []map[string]any
		for i, o := range cur.Options {
			style := 2
			if slices.Contains(sel, o) {
				style = 1
			}
			buttons = append(buttons, map[string]any{
				"type": 2, "style": style,
				"label":     fmt.Sprintf("%d", i+1),
				"custom_id": fmt.Sprintf("ea:%s:%d", ask.nonce, i),
			})
		}
		// ✍️ 是选项之外的「第 N+1 个选择」，跟在编号后面（一行最多 5 个）。
		if len(buttons) < 5 {
			buttons = append(buttons, inputBtn)
			inputPlaced = true
		}
		rows = append(rows, map[string]any{"type": 1, "components": buttons})
	default:
		opts := make([]map[string]any, 0, len(cur.Options))
		for _, o := range cur.Options {
			opt := map[string]any{"label": trimRunes(o, 90), "value": trimRunes(o, 90)}
			if slices.Contains(sel, o) {
				opt["default"] = true
			}
			opts = append(opts, opt)
		}
		selComp := map[string]any{
			"type": 3, "custom_id": "es:" + ask.nonce, "options": opts,
			"placeholder": "选一个…",
		}
		if cur.Multiple {
			selComp["placeholder"] = "可以选多个…"
			selComp["min_values"] = 0
			selComp["max_values"] = len(opts)
		}
		rows = append(rows, map[string]any{"type": 1, "components": []map[string]any{selComp}})
	}
	nav := []map[string]any{
		{"type": 2, "style": 2, "label": "⬅️", "custom_id": "en:" + ask.nonce + ":p",
			"disabled": ask.cursor == 0},
		{"type": 2, "style": 2, "label": "➡️", "custom_id": "en:" + ask.nonce + ":n",
			"disabled": ask.cursor == len(ask.questions)-1},
	}
	if !inputPlaced {
		nav = append(nav, inputBtn)
	}
	nav = append(nav, map[string]any{
		"type": 2, "style": 3, "label": "提交", "custom_id": "ez:" + ask.nonce,
		"disabled": !askReady(ask)})
	rows = append(rows, map[string]any{"type": 1, "components": nav})
	return rows
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
