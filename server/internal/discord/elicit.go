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

// modal 一屏最多 5 个顶层组件（平台上限）；「选项 + 自由输入」的题占两个位置。
const modalPageSize = 5

func questionSlots(q elicitQuestion) int {
	if len(q.Options) > 0 && q.OtherField != "" {
		return 2
	}
	return 1
}

// elicitModal 拼第 page 页的 modal 载荷（callback type 9 的 data）。
// custom_id 形如 `em:<nonce>:<page>`——nonce 挡旧卡的残留按钮。
func elicitModal(nonce string, qs []elicitQuestion, page int) map[string]any {
	var comps []map[string]any

	// 按占位切页：从既往页累计跳过，装满一屏为止。
	skip := 0
	for range page {
		used := 0
		for skip < len(qs) && used+questionSlots(qs[skip]) <= modalPageSize {
			used += questionSlots(qs[skip])
			skip++
		}
	}

	used := 0
	end := skip
	for end < len(qs) && used+questionSlots(qs[end]) <= modalPageSize {
		q := qs[end]
		if len(q.Options) == 0 {
			comps = append(comps, elicitLabel(q, elicitTextInput(q.ID, q.Required)))
		} else {
			choices := make([]choice, 0, len(q.Options))
			for _, o := range q.Options {
				choices = append(choices, choice{Label: o, Value: o})
			}
			comps = append(comps, elicitLabel(q, selectComponent(q.ID, choices, q.Required)))
			if q.OtherField != "" {
				comps = append(comps, map[string]any{
					"type": 18, "label": "其他（" + trimRunes(q.Title, 30) + "）",
					"description": "上一题选项都不合适时填这里",
					"component":   elicitTextInput(q.OtherField, false),
				})
			}
		}
		used += questionSlots(q)
		end++
	}

	title := "agent 的问题"
	if end < len(qs) {
		title = fmt.Sprintf("agent 的问题（还有 %d 题）", len(qs)-end)
	}
	return map[string]any{
		"custom_id":  fmt.Sprintf("em:%s:%d", nonce, page),
		"title":      title,
		"components": comps,
	}
}

// elicitRemaining 报告第 page 页之后还有没有题（分页提示用）。
func elicitRemaining(qs []elicitQuestion, page int) bool {
	probe := elicitModal("0", qs, page+1)
	comps := probe["components"].([]map[string]any)
	return len(comps) > 0
}

func elicitLabel(q elicitQuestion, inner map[string]any) map[string]any {
	out := map[string]any{"type": 18, "label": trimRunes(q.Title, 45), "component": inner}
	if q.Description != "" {
		out["description"] = trimRunes(q.Description, 100)
	}
	return out
}

func elicitTextInput(id string, required bool) map[string]any {
	return map[string]any{
		"type": 4, "custom_id": id, "style": 2, "required": required,
		"placeholder": "在这里输入…",
	}
}
