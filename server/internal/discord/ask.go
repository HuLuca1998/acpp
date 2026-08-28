package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"acpp/server/internal/acp"
)

// 本文件是子区里的问答桥：agent 的权限请求与提问转成一条编号列表消息，
// 用户的下一条子区消息优先当作答（编号选选项，其他文本按自由输入）。
// safe 权限档与 agent 主动提问靠它才能在 Discord 里走通。

// pendingAsk 是子区里挂起的一次问答。
type pendingAsk struct {
	kind string // "permission" | "elicitation"
	id   string // permissionID / elicitationID
	key  string // acp 会话 key
	// options 按展示顺序存回传值（权限是 optionId，提问是选项值）。
	options []string
	// field 是提问答案的字段名（权限用不到）。
	field string
	// allowFree 表示提问接受自由文本（没有选项清单时）。
	allowFree bool
}

// askPermission 把权限请求发进子区等人批。
func (s *Service) askPermission(token, threadID string, tc *threadChat, ev acp.Event) {
	var lines []string
	title := ev.Title
	if title == "" {
		title = "agent 请求授权"
	}
	lines = append(lines, "🔐 **"+trimRunes(title, 200)+"**")
	var opts []string
	for i, o := range ev.Options {
		label := o.Name
		if label == "" {
			label = o.OptionID
		}
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, trimRunes(label, 100)))
		opts = append(opts, o.OptionID)
	}
	lines = append(lines, "-# 回复编号裁决；其他消息会先被当作答案。")

	tc.mu.Lock()
	tc.ask = &pendingAsk{kind: "permission", id: ev.PermissionID, key: "dc:" + threadID, options: opts}
	tc.mu.Unlock()
	s.say(context.Background(), token, threadID, strings.Join(lines, "\n"))
}

// askElicitation 把 agent 的提问发进子区。只支持单题（多题形状在
// Discord 文本流里问不清楚，诚实取消别让回合挂死）。
func (s *Service) askElicitation(token, threadID string, tc *threadChat, ev acp.Event) {
	q, err := parseSingleQuestion(ev.RawInput)
	if err != nil {
		slog.Warn("提问形状不支持，自动取消", "err", err)
		if rerr := s.acpMgr.ResolveElicitation("dc:"+threadID, ev.ElicitationID,
			acp.ElicitationResult{Action: "cancel"}); rerr != nil {
			slog.Warn("取消提问失败", "err", rerr)
		}
		s.say(context.Background(), token, threadID, "（agent 问了一个多题表单，Discord 里暂不支持，已自动取消——去网页版处理这类任务）")
		return
	}

	var lines []string
	msg := strings.TrimSpace(ev.Text)
	if msg != "" {
		lines = append(lines, "❓ "+trimRunes(msg, 500))
	}
	if q.title != "" && q.title != msg {
		lines = append(lines, "**"+trimRunes(q.title, 200)+"**")
	}
	for i, o := range q.options {
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, trimRunes(o, 100)))
	}
	if len(q.options) > 0 {
		lines = append(lines, "-# 回复编号选择，或直接回复文字作为答案。")
	} else {
		lines = append(lines, "-# 直接回复文字作为答案。")
	}

	tc.mu.Lock()
	tc.ask = &pendingAsk{
		kind: "elicitation", id: ev.ElicitationID, key: "dc:" + threadID,
		options: q.options, field: q.field, allowFree: true,
	}
	tc.mu.Unlock()
	s.say(context.Background(), token, threadID, strings.Join(lines, "\n"))
}

// answerAsk 用子区的一条消息了结挂起的问答。回执用 reaction 标在作答
// 消息上（✅ 已回传 / ⚠️ 已失效），不再往子区堆确认文字。
func (s *Service) answerAsk(ctx context.Context, token, threadID string, tc *threadChat, ask *pendingAsk, msgID, text string) {
	pick := -1
	if n, err := strconv.Atoi(strings.TrimSpace(text)); err == nil && n >= 1 && n <= len(ask.options) {
		pick = n - 1
	}

	var err error
	switch ask.kind {
	case "permission":
		if pick < 0 {
			s.say(ctx, token, threadID, fmt.Sprintf("回复 1–%d 的编号来裁决。", len(ask.options)))
			return
		}
		err = s.acpMgr.ResolvePermission(ask.key, ask.id, ask.options[pick])
	case "elicitation":
		answer := text
		if pick >= 0 {
			answer = ask.options[pick]
		} else if !ask.allowFree {
			s.say(ctx, token, threadID, fmt.Sprintf("回复 1–%d 的编号来选择。", len(ask.options)))
			return
		}
		err = s.acpMgr.ResolveElicitation(ask.key, ask.id,
			acp.ElicitationResult{Action: "accept", Content: map[string]any{ask.field: answer}})
	}

	tc.mu.Lock()
	if tc.ask == ask {
		tc.ask = nil
	}
	tc.mu.Unlock()
	if err != nil {
		s.react(ctx, token, threadID, msgID, "⚠️")
		s.say(ctx, token, threadID, "这条问答已经失效了（可能超时或已在别处处理）。")
		return
	}
	s.react(ctx, token, threadID, msgID, "✅")
}

// singleQuestion 是解析后的单题提问。
type singleQuestion struct {
	field   string
	title   string
	options []string
}

// parseSingleQuestion 解 requestedSchema 的单题形状。两条 ACP 的自由输入
// 标记不同：codex 用 `_meta.codex.isOtherAnswer`（该字段不算独立题目），
// claude 用 `<题目>_custom` 命名约定——与 web 端解析口径一致。
func parseSingleQuestion(raw json.RawMessage) (singleQuestion, error) {
	var schema struct {
		Properties map[string]struct {
			Title string `json:"title"`
			OneOf []struct {
				Const string `json:"const"`
			} `json:"oneOf"`
			Meta struct {
				Codex struct {
					IsOtherAnswer bool `json:"isOtherAnswer"`
				} `json:"codex"`
			} `json:"_meta"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		return singleQuestion{}, fmt.Errorf("解 requestedSchema: %w", err)
	}
	var q singleQuestion
	count := 0
	for key, prop := range schema.Properties {
		if prop.Meta.Codex.IsOtherAnswer {
			continue
		}
		if target, ok := strings.CutSuffix(key, "_custom"); ok {
			if _, exists := schema.Properties[target]; exists {
				continue
			}
		}
		count++
		q.field = key
		q.title = prop.Title
		for _, o := range prop.OneOf {
			if o.Const != "" {
				q.options = append(q.options, o.Const)
			}
		}
	}
	if count != 1 {
		return singleQuestion{}, fmt.Errorf("题目数 %d，只支持单题", count)
	}
	return q, nil
}
