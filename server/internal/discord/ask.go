package discord

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"acpp/server/internal/acp"
)

// 本文件是子区里的问答桥：agent 的权限请求变成按钮裁决卡（点一下即
// 裁决，卡片原地收口）；提问是**入口卡 + 一次性表单**——点「📝 填表」
// 弹 modal，单选组/多选组/输入框混排、本地勾选零往返、一次提交
//（参考网页会话问答卡的「一次提交」交互；>5 题才分页兜底）。
// 单题提问仍可直接打字/回编号作答。

// pendingAsk 是子区里挂起的一次问答。
type pendingAsk struct {
	kind string // "permission" | "elicitation"
	id   string // permissionID / elicitationID
	key  string // acp 会话 key
	// nonce 进按钮/modal 的 custom_id，挡旧卡的残留组件误触当前问答。
	nonce string
	// msgID 是问答卡消息，收口时原地改终态。
	msgID string
	// title 是这次问答的题面（权限的操作标题 / 提问的引言），终态卡要
	// 留住它——收口后也得看得出当初问的是什么。
	title string
	// 权限专用：选项清单（按钮顺序）。
	permOpts []acp.PermissionOption
	// 提问专用：题目、当前题号（逐题卡）、已收答案（选项集合与自由输入
	// 各占一个字段，多选题的选项字段是多值）。
	questions []elicitQuestion
	cursor    int
	partial   map[string][]string
}

// askContent 把答案拼成 ResolveElicitation 的 content：多选题给数组，
// 其余给单值字符串。
func askContent(ask *pendingAsk) map[string]any {
	multiple := map[string]bool{}
	for _, q := range ask.questions {
		multiple[q.ID] = q.Multiple
	}
	content := map[string]any{}
	for k, vs := range ask.partial {
		if len(vs) == 0 {
			continue
		}
		if multiple[k] {
			content[k] = vs
		} else {
			content[k] = vs[0]
		}
	}
	return content
}

// permClosedV2 是权限决策的终态卡。刻意压成两行小卡（用户点名嫌大）：
// 决策对象截成一行，命令全文没必要留——它马上就要在对话里被执行了。
func permClosedV2(ask *pendingAsk, opt acp.PermissionOption, by string) []map[string]any {
	mark, color := "✅", colorGreen
	if strings.HasPrefix(opt.Kind, "reject") {
		mark, color = "❌", colorRed
	}
	title := strings.TrimSpace(strings.SplitN(ask.title, "\n", 2)[0])
	title = strings.TrimPrefix(title, "### ")
	return v2Container(color, []map[string]any{
		v2Text(fmt.Sprintf("%s **%s**　%s\n-# %s · <t:%d:R>",
			mark, orDefault(opt.Name, opt.OptionID), trimRunes(title, 90), by, time.Now().Unix())),
	})
}

// askPermission 把权限请求发成按钮裁决卡。claude 的计划审批（PlanReview）
// 也走这里——计划全文进卡片正文。
func (s *Service) askPermission(token, threadID string, tc *threadChat, ev acp.Event) {
	nonce, err := randomID()
	if err != nil {
		return
	}
	ask := &pendingAsk{
		kind: "permission", id: ev.PermissionID, key: "dc:" + threadID,
		nonce: nonce, permOpts: ev.Options,
	}

	title := "🔐 " + orDefault(trimRunes(ev.Title, 220), "agent 请求授权")
	desc := "点按钮裁决，或回复选项编号。"
	if ev.PlanReview != nil {
		title = "📋 计划完成，请求开始执行"
		desc = trimRunes(ev.PlanReview.Plan, 3800) + "\n\n点按钮裁决。"
	}
	ask.title = title
	var buttons []map[string]any
	for i, o := range ev.Options {
		if i == 5 {
			break // 一行最多 5 个按钮；权限选项超过 5 个从未见过
		}
		style := 2
		switch {
		case strings.HasPrefix(o.Kind, "allow"):
			style = 3
		case strings.HasPrefix(o.Kind, "reject"):
			style = 4
		}
		buttons = append(buttons, map[string]any{
			"type": 2, "style": style,
			"label":     trimRunes(orDefault(o.Name, o.OptionID), 76),
			"custom_id": fmt.Sprintf("pm:%s:%d", nonce, i),
		})
	}

	msgID := s.postCard(token, threadID, map[string]any{
		"flags": 1 << 15,
		"components": v2Container(colorBlurbe, []map[string]any{
			v2Text("### " + title),
			v2Text("-# " + desc),
			{"type": 1, "components": buttons},
		}),
		"allowed_mentions": noMentions(),
	})
	ask.msgID = msgID
	tc.mu.Lock()
	tc.ask = ask
	tc.mu.Unlock()
}

// askElicitation 把 agent 的提问发成逐题卡：从第一题开始，点选即翻题。
func (s *Service) askElicitation(token, threadID string, tc *threadChat, ev acp.Event) {
	qs, err := parseElicitSchema(ev.RawInput)
	if err != nil || len(qs) == 0 {
		slog.Warn("提问形状解不开，自动取消", "err", err)
		if rerr := s.acpMgr.ResolveElicitation("dc:"+threadID, ev.ElicitationID,
			acp.ElicitationResult{Action: "cancel"}); rerr != nil {
			slog.Warn("取消提问失败", "err", rerr)
		}
		s.say(context.Background(), token, threadID, "（agent 的提问形状解不开，已自动取消）")
		return
	}
	nonce, err := randomID()
	if err != nil {
		return
	}
	ask := &pendingAsk{
		kind: "elicitation", id: ev.ElicitationID, key: "dc:" + threadID,
		nonce: nonce, questions: qs, partial: map[string][]string{},
		title: strings.TrimSpace(ev.Text),
	}

	msgID := s.postCard(token, threadID, map[string]any{
		"flags":            1 << 15,
		"components":       askIntroCard(ask),
		"allowed_mentions": noMentions(),
	})
	ask.msgID = msgID
	tc.mu.Lock()
	tc.ask = ask
	tc.mu.Unlock()
}

// currentAsk 取子区当前挂起的问答；nonce 不匹配（旧卡残留组件）给 nil。
func (s *Service) currentAsk(threadID, nonce string) *pendingAsk {
	tc := s.chatState(threadID)
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if tc.ask == nil || (nonce != "" && tc.ask.nonce != nonce) {
		return nil
	}
	return tc.ask
}

// handleAskComponent 分发问答卡上的组件点击：
//   - pm:<nonce>:<idx>  权限按钮 → 裁决 + 原地收口
//   - eb:<nonce>:<page> 「📝 填表」→ 弹第 page 页表单
//   - ec:<nonce>:<page> 「继续填写」→ 弹下一页（仅 >5 题的分页兜底）
func (s *Service) handleAskComponent(token string, ev interactionEvent) {
	parts := strings.Split(ev.Data.CustomID, ":")
	if len(parts) != 3 {
		return
	}
	prefix, nonce := parts[0], parts[1]
	ask := s.currentAsk(ev.ChannelID, nonce)
	if ask == nil {
		s.ephemeral(token, ev, "这张卡已经失效了（处理过或超时）。")
		return
	}
	switch prefix {
	case "pm":
		idx, err := strconv.Atoi(parts[2])
		if err != nil || idx < 0 || idx >= len(ask.permOpts) {
			return
		}
		s.resolvePermissionAsk(token, ev, ask, idx)
	case "eb", "ec":
		page, _ := strconv.Atoi(parts[2])
		if err := interactionCallback(token, ev.ID, ev.Token, 9, formModal(ask, page)); err != nil {
			slog.Error("弹表单失败", "err", err)
		}
	}
}

// handleAskModal 收表单的一页（em:<nonce>:<page>）：常态一页就是全部；
// >5 题时还有下页就暂存并给「继续填写」按钮，收齐整体回传并收口。
func (s *Service) handleAskModal(token string, ev interactionEvent) {
	parts := strings.Split(ev.Data.CustomID, ":")
	if len(parts) != 3 {
		return
	}
	page, _ := strconv.Atoi(parts[2])
	ask := s.currentAsk(ev.ChannelID, parts[1])
	if ask == nil || ask.kind != "elicitation" {
		s.ephemeral(token, ev, "这张卡已经失效了（处理过或超时）。")
		return
	}
	for k, vs := range parseModalSubmit(ev.Data.Components) {
		ask.partial[k] = vs
	}
	if page+1 < formPages(ask.questions) {
		err := interactionCallback(token, ev.ID, ev.Token, 4, map[string]any{
			"content": "这一页收到了，还有下一页。",
			"flags":   1 << 6,
			"components": []map[string]any{{"type": 1, "components": []map[string]any{{
				"type": 2, "style": 1, "label": "▶ 继续填写",
				"custom_id": fmt.Sprintf("ec:%s:%d", ask.nonce, page+1),
			}}}},
			"allowed_mentions": noMentions(),
		})
		if err != nil {
			slog.Error("分页续填提示失败", "err", err)
		}
		return
	}

	err := s.acpMgr.ResolveElicitation(ask.key, ask.id,
		acp.ElicitationResult{Action: "accept", Content: askContent(ask)})
	s.clearAsk(ev.ChannelID, ask)
	if err != nil {
		s.ephemeral(token, ev, "这个提问已经失效了（超时或已在别处回答）。")
		return
	}
	// modal 的 callback 直接把入口卡原地改成记录卡——一步收口，
	// 不再补一条多余的「已提交」确认（用户点名嫌吵）。
	closed := elicitClosedV2(ask, ask.partial, "由 "+ev.user()+" 提交")
	cerr := interactionCallback(token, ev.ID, ev.Token, 7, map[string]any{
		"components":       closed,
		"allowed_mentions": noMentions(),
	})
	if cerr != nil {
		slog.Warn("表单收口改卡失败，走 REST 兜底", "err", cerr)
		s.finalizeAskCard(token, ev.ChannelID, ask, closed)
	}
}

// resolvePermissionAsk 用按钮点击裁决权限：回传 + 原地改终态。
func (s *Service) resolvePermissionAsk(token string, ev interactionEvent, ask *pendingAsk, idx int) {
	opt := ask.permOpts[idx]
	err := s.acpMgr.ResolvePermission(ask.key, ask.id, opt.OptionID)
	s.clearAsk(ev.ChannelID, ask)
	if err != nil {
		s.ephemeral(token, ev, "这条已经处理过了（或已失效）。")
		return
	}
	// type 7 = 原地改卡：按钮摘掉，决策对象与裁决结果都留在对话里。
	cerr := interactionCallback(token, ev.ID, ev.Token, 7, map[string]any{
		"components":       permClosedV2(ask, opt, "由 "+ev.user()),
		"allowed_mentions": noMentions(),
	})
	if cerr != nil {
		slog.Error("权限卡收口失败", "err", cerr)
	}
}

// answerAsk 用子区的一条普通消息了结挂起的问答（快捷路径）：权限回编号；
// 单题提问回编号（多选可「1 3」）或文字。多题提问请走表单。
func (s *Service) answerAsk(ctx context.Context, token, threadID string, ask *pendingAsk, msgID, text string) {
	pick := -1
	if n, err := strconv.Atoi(strings.TrimSpace(text)); err == nil && n >= 1 {
		pick = n - 1
	}

	var err error
	var closed []map[string]any
	switch ask.kind {
	case "permission":
		if pick < 0 || pick >= len(ask.permOpts) {
			s.say(ctx, token, threadID, fmt.Sprintf("点卡片按钮，或回复 1–%d 的编号。", len(ask.permOpts)))
			return
		}
		opt := ask.permOpts[pick]
		err = s.acpMgr.ResolvePermission(ask.key, ask.id, opt.OptionID)
		closed = permClosedV2(ask, opt, "以消息作答")
	case "elicitation":
		if len(ask.questions) != 1 {
			s.say(ctx, token, threadID, "有好几题，点卡片上的「📝 填表回答」一次填完。")
			return
		}
		q := ask.questions[0]
		values := parseAnswerText(text, q)
		field := q.ID
		if q.OtherField != "" && len(values) == 1 && !slices.Contains(optionValues(q), values[0]) {
			field = q.OtherField
		}
		ask.partial[field] = values
		err = s.acpMgr.ResolveElicitation(ask.key, ask.id,
			acp.ElicitationResult{Action: "accept", Content: askContent(ask)})
		closed = elicitClosedV2(ask, ask.partial, "以消息作答")
	}

	s.clearAsk(threadID, ask)
	if err != nil {
		s.react(ctx, token, threadID, msgID, "⚠️")
		s.say(ctx, token, threadID, "这条问答已经失效了（可能超时或已在别处处理）。")
		return
	}
	s.react(ctx, token, threadID, msgID, "✅")
	s.finalizeAskCard(token, threadID, ask, closed)
}

// parseAnswerText 把文本作答变成值集合：整条都是编号（空格/逗号分隔）就
// 映射成选项，否则原文当一个自由输入值。
func parseAnswerText(text string, q elicitQuestion) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == ',' || r == '，' || r == '、'
	})
	var picked []string
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 1 || n > len(q.Options) {
			return []string{text}
		}
		picked = append(picked, q.Options[n-1].Const)
	}
	if len(picked) == 0 {
		return []string{text}
	}
	if !q.Multiple {
		return picked[:1]
	}
	return picked
}

// clearAsk 清掉子区的挂起问答（只清自己那份，防并发覆盖）。
func (s *Service) clearAsk(threadID string, ask *pendingAsk) {
	tc := s.chatState(threadID)
	tc.mu.Lock()
	if tc.ask == ask {
		tc.ask = nil
	}
	tc.mu.Unlock()
}

// finalizeAskCard 用 REST 把问答卡整卡替换（V2 组件树）——服务
// interaction callback 之外的路径（消息作答的翻题与收口、别处处理）。
func (s *Service) finalizeAskCard(token, threadID string, ask *pendingAsk, components []map[string]any) {
	if ask.msgID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := botREST(ctx, token, "PATCH",
		fmt.Sprintf("/channels/%s/messages/%s", threadID, ask.msgID), map[string]any{
			"components":       components,
			"allowed_mentions": noMentions(),
		}, nil)
	if err != nil {
		slog.Warn("问答卡更新失败", "err", err)
	}
}

// askDone 处理 PermissionDone/ElicitationDone：问答在别处（网页/超时）
// 收口时，把卡片改成灰终态。自己收口的场景 ask 已清，这里自然跳过。
func (s *Service) askDone(token, threadID string, tc *threadChat, doneID string) {
	tc.mu.Lock()
	ask := tc.ask
	if ask == nil || ask.id != doneID {
		tc.mu.Unlock()
		return
	}
	tc.ask = nil
	tc.mu.Unlock()
	// 题面留住，只把状态标灰——别处处理的也得看得出当初问的是什么。
	title := ask.title
	if title == "" {
		title = "agent 的提问"
	}
	s.finalizeAskCard(token, threadID, ask, v2Container(colorGrey, []map[string]any{
		v2Text("### " + trimRunes(title, 200)),
		v2Text("⚪ 已在别处处理，或已超时。"),
	}))
}

// postCard 发一条带组件的卡，返回消息 id（失败给空串，问答仍可用文本路径）。
func (s *Service) postCard(token, threadID string, body map[string]any) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var msg struct {
		ID string `json:"id"`
	}
	if err := botREST(ctx, token, "POST", "/channels/"+threadID+"/messages", body, &msg); err != nil {
		slog.Error("发问答卡失败", "err", err)
		return ""
	}
	return msg.ID
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
