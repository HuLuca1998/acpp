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
// 裁决，卡片原地收口），提问变成**逐题卡**——同一张卡上一题一题点过去
//（已答的累积显示），答完自动整体回传。选项是按钮/下拉，自由输入给
// 「✍️ 输入」小表单；直接打字/回编号也永远有效（答的是当前题）。

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

// applyAnswer 把一次作答落到当前题上：
//   - 单选：选项覆盖（清掉自由输入），选项外的文本进自由字段（清掉选项）；
//   - 多选：选项 toggle（勾/取消），文本进自由字段与选项集合并存。
//
// 返回值报告要不要自动前进（单选选完就走，多选停留让人多勾几个）。
func (s *Service) applyAnswer(ask *pendingAsk, values []string) (advance bool) {
	cur := ask.questions[ask.cursor]
	var opts, free []string
	for _, v := range values {
		if slices.Contains(cur.Options, v) {
			opts = append(opts, v)
		} else if v != "" {
			free = append(free, v)
		}
	}
	freeField := cur.OtherField
	if freeField == "" {
		freeField = cur.ID // 纯输入题的答案直接落题目字段
	}

	if cur.Multiple {
		set := ask.partial[cur.ID]
		for _, o := range opts {
			if i := slices.Index(set, o); i >= 0 {
				set = slices.Delete(set, i, i+1)
			} else {
				set = append(set, o)
			}
		}
		ask.partial[cur.ID] = set
		if len(free) > 0 {
			ask.partial[freeField] = []string{strings.Join(free, "、")}
		}
		return false
	}

	delete(ask.partial, cur.ID)
	if cur.OtherField != "" {
		delete(ask.partial, cur.OtherField)
	}
	switch {
	case len(free) > 0:
		ask.partial[freeField] = []string{free[0]}
	case len(opts) > 0:
		ask.partial[cur.ID] = []string{opts[0]}
	}
	return ask.cursor < len(ask.questions)-1
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

// permClosedEmbed 是权限决策的终态卡：留住决策对象，标出选了什么、谁选的。
func permClosedEmbed(ask *pendingAsk, opt acp.PermissionOption, by string) map[string]any {
	mark, color := "✅", colorGreen
	if strings.HasPrefix(opt.Kind, "reject") {
		mark, color = "❌", colorRed
	}
	return map[string]any{
		"title": ask.title,
		"description": fmt.Sprintf("%s **%s** · %s · <t:%d:R>",
			mark, orDefault(opt.Name, opt.OptionID), by, time.Now().Unix()),
		"color": color,
	}
}

// elicitClosedEmbed 是提问的终态卡：逐题列「问题—答案」，问答记录留在对话里。
func elicitClosedEmbed(ask *pendingAsk, answers map[string][]string, by string) map[string]any {
	fields := make([]map[string]any, 0, len(ask.questions))
	for _, q := range ask.questions {
		vals := append([]string(nil), answers[q.ID]...)
		if q.OtherField != "" {
			vals = append(vals, answers[q.OtherField]...)
		}
		a := strings.Join(vals, "、")
		if a == "" {
			a = "—"
		}
		fields = append(fields, map[string]any{
			"name": "❓ " + trimRunes(q.Title, 240), "value": trimRunes(a, 1000), "inline": false,
		})
	}
	embed := map[string]any{
		"title":  "✅ 已回答",
		"fields": fields,
		"footer": map[string]any{"text": by},
		"color":  colorGreen,
	}
	if ask.title != "" {
		embed["description"] = trimRunes(ask.title, 500)
	}
	return embed
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
		"embeds": []map[string]any{{
			"title": title, "description": desc, "color": colorBlurbe,
		}},
		"components":       []map[string]any{{"type": 1, "components": buttons}},
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
		"embeds":           []map[string]any{questionCardEmbed(ask)},
		"components":       questionCardComponents(ask),
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
//   - pm:<nonce>:<idx> 权限按钮 → 裁决 + 原地收口
//   - ea:<nonce>:<idx> 当前题的选项按钮 → 记答案、卡片翻下一题
//   - es:<nonce>       当前题的选项下拉 → 同上
//   - ei:<nonce>       「✍️ 输入」→ 弹单题小表单
func (s *Service) handleAskComponent(token string, ev interactionEvent) {
	parts := strings.Split(ev.Data.CustomID, ":")
	if len(parts) < 2 {
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
		if len(parts) != 3 {
			return
		}
		idx, err := strconv.Atoi(parts[2])
		if err != nil || idx < 0 || idx >= len(ask.permOpts) {
			return
		}
		s.resolvePermissionAsk(token, ev, ask, idx)
	case "ea":
		if len(parts) != 3 {
			return
		}
		cur := ask.questions[ask.cursor]
		idx, err := strconv.Atoi(parts[2])
		if err != nil || idx < 0 || idx >= len(cur.Options) {
			return
		}
		s.stepAsk(token, ev, ask, []string{cur.Options[idx]})
	case "es":
		// 下拉一次交互给出全部选中值（多选下拉天然多值）。
		s.stepAsk(token, ev, ask, ev.Data.Values)
	case "en":
		if len(parts) != 3 {
			return
		}
		if parts[2] == "p" && ask.cursor > 0 {
			ask.cursor--
		}
		if parts[2] == "n" && ask.cursor < len(ask.questions)-1 {
			ask.cursor++
		}
		s.refreshAskCard(token, ev, ask)
	case "ez":
		s.submitAsk(token, ev, ask)
	case "ei":
		if err := interactionCallback(token, ev.ID, ev.Token, 9, inputModal(ask)); err != nil {
			slog.Error("弹输入表单失败", "err", err)
		}
	}
}

// handleAskModal 收「✍️ 输入」的单题答案（em:<nonce>），落到当前题上。
func (s *Service) handleAskModal(token string, ev interactionEvent) {
	parts := strings.Split(ev.Data.CustomID, ":")
	if len(parts) != 2 {
		return
	}
	ask := s.currentAsk(ev.ChannelID, parts[1])
	if ask == nil || ask.kind != "elicitation" {
		s.ephemeral(token, ev, "这张卡已经失效了（处理过或超时）。")
		return
	}
	answer := strings.TrimSpace(parseModalSubmit(ev.Data.Components)["answer"])
	if answer == "" {
		s.ephemeral(token, ev, "没收到内容，再点一次「✍️ 输入」。")
		return
	}
	s.stepAsk(token, ev, ask, []string{answer})
}

// stepAsk 落一次作答并刷新卡片（单选自动前进，多选停留继续勾）。
// 提交不在这里发生——「提交」按钮是唯一的收口入口，选错随时回去改。
func (s *Service) stepAsk(token string, ev interactionEvent, ask *pendingAsk, values []string) {
	if s.applyAnswer(ask, values) {
		ask.cursor++
	}
	s.refreshAskCard(token, ev, ask)
}

// refreshAskCard 用 interaction callback 原地刷新逐题卡。
func (s *Service) refreshAskCard(token string, ev interactionEvent, ask *pendingAsk) {
	err := interactionCallback(token, ev.ID, ev.Token, 7, map[string]any{
		"embeds":           []map[string]any{questionCardEmbed(ask)},
		"components":       questionCardComponents(ask),
		"allowed_mentions": noMentions(),
	})
	if err != nil {
		slog.Error("问答卡刷新失败", "err", err)
	}
}

// submitAsk 整体提交：必答缺答就提示（按钮 disabled 兜底，这里防御性
// 双保险），齐了回传并把卡收口成「问题—答案」记录。
func (s *Service) submitAsk(token string, ev interactionEvent, ask *pendingAsk) {
	if !askReady(ask) {
		for i, q := range ask.questions {
			if q.Required && len(answersOf(ask, q)) == 0 {
				s.ephemeral(token, ev, fmt.Sprintf("第 %d 题「%s」还没答。", i+1, trimRunes(q.Title, 60)))
				return
			}
		}
		s.ephemeral(token, ev, "至少答一题再提交。")
		return
	}
	err := s.acpMgr.ResolveElicitation(ask.key, ask.id,
		acp.ElicitationResult{Action: "accept", Content: askContent(ask)})
	s.clearAsk(ev.ChannelID, ask)
	if err != nil {
		s.ephemeral(token, ev, "这个提问已经失效了（超时或已在别处回答）。")
		return
	}
	cerr := interactionCallback(token, ev.ID, ev.Token, 7, map[string]any{
		"embeds":           []map[string]any{elicitClosedEmbed(ask, ask.partial, "由 "+ev.user()+" 提交")},
		"components":       []map[string]any{},
		"allowed_mentions": noMentions(),
	})
	if cerr != nil {
		slog.Error("问答卡收口失败", "err", cerr)
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
		"embeds":           []map[string]any{permClosedEmbed(ask, opt, "由 "+ev.user())},
		"components":       []map[string]any{},
		"allowed_mentions": noMentions(),
	})
	if cerr != nil {
		slog.Error("权限卡收口失败", "err", cerr)
	}
}

// answerAsk 用子区的一条普通消息了结挂起的问答（快捷路径）：权限回编号，
// 单题提问回编号或文字。多题提问必须走卡片上的表单。
func (s *Service) answerAsk(ctx context.Context, token, threadID string, ask *pendingAsk, msgID, text string) {
	pick := -1
	if n, err := strconv.Atoi(strings.TrimSpace(text)); err == nil && n >= 1 {
		pick = n - 1
	}

	var err error
	var closed map[string]any
	switch ask.kind {
	case "permission":
		if pick < 0 || pick >= len(ask.permOpts) {
			s.say(ctx, token, threadID, fmt.Sprintf("点卡片按钮，或回复 1–%d 的编号。", len(ask.permOpts)))
			return
		}
		opt := ask.permOpts[pick]
		err = s.acpMgr.ResolvePermission(ask.key, ask.id, opt.OptionID)
		closed = permClosedEmbed(ask, opt, "以消息作答")
	case "elicitation":
		// 逐题卡的文本路径：编号选当前题的选项（多选可「1 3」一次勾几个），
		// 其他文字进自由输入；「提交」两个字整体提交。
		if text == "提交" || strings.EqualFold(text, "submit") {
			if !askReady(ask) {
				s.say(ctx, token, threadID, "还有必答题没答，⬅️➡️ 翻回去看看。")
				return
			}
			err = s.acpMgr.ResolveElicitation(ask.key, ask.id,
				acp.ElicitationResult{Action: "accept", Content: askContent(ask)})
			closed = elicitClosedEmbed(ask, ask.partial, "以消息作答")
			break
		}
		cur := ask.questions[ask.cursor]
		if s.applyAnswer(ask, parseAnswerText(text, cur)) {
			ask.cursor++
		}
		s.react(ctx, token, threadID, msgID, "✅")
		s.finalizeAskCardKeep(token, threadID, ask,
			questionCardEmbed(ask), questionCardComponents(ask))
		return
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
		picked = append(picked, q.Options[n-1])
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

// finalizeAskCard 把问答卡改成终态（摘按钮）。自己点按钮的场景走
// interaction callback 原地改，这里服务别的收口路径（消息作答、别处处理）。
func (s *Service) finalizeAskCard(token, threadID string, ask *pendingAsk, embed map[string]any) {
	s.finalizeAskCardKeep(token, threadID, ask, embed, []map[string]any{})
}

// finalizeAskCardKeep 用 REST 改问答卡（components 由调用方给——翻题保留
// 组件，收口传空摘掉）。
func (s *Service) finalizeAskCardKeep(token, threadID string, ask *pendingAsk, embed map[string]any, components []map[string]any) {
	if ask.msgID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := botREST(ctx, token, "PATCH",
		fmt.Sprintf("/channels/%s/messages/%s", threadID, ask.msgID), map[string]any{
			"embeds":           []map[string]any{embed},
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
	s.finalizeAskCard(token, threadID, ask, map[string]any{
		"title":       ask.title,
		"description": "⚪ 已在别处处理，或已超时。",
		"color":       colorGrey,
	})
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
