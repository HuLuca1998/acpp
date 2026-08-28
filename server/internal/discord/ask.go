package discord

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"strconv"
	"strings"
	"time"

	"acpp/server/internal/acp"
)

// 本文件是子区里的问答桥：agent 的权限请求变成按钮裁决卡（点一下即
// 裁决，卡片原地收口），提问变成「回答」按钮 + 分页 modal 表单（一页
// 5 项，提交后「继续回答」——modal 提交后不能直接再弹 modal，平台规则）。
// 单题提问仍接受直接打字作答（快捷路径）。

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
	// 提问专用：题目、分页中途答案。
	questions []elicitQuestion
	partial   map[string]string
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
func elicitClosedEmbed(ask *pendingAsk, answers map[string]string, by string) map[string]any {
	fields := make([]map[string]any, 0, len(ask.questions))
	for _, q := range ask.questions {
		a := answers[q.ID]
		if a == "" && q.OtherField != "" {
			a = answers[q.OtherField]
		}
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

// askElicitation 把 agent 的提问发成「回答」按钮卡；点开是分页 modal。
// 单题时也接受直接打字作答。
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
		nonce: nonce, questions: qs, partial: map[string]string{},
		title: strings.TrimSpace(ev.Text),
	}

	var lines []string
	if msg := strings.TrimSpace(ev.Text); msg != "" {
		lines = append(lines, trimRunes(msg, 800))
	}
	hint := "点「回答」填表。"
	if len(qs) == 1 {
		if len(qs[0].Options) > 0 {
			var opts []string
			for i, o := range qs[0].Options {
				opts = append(opts, fmt.Sprintf("%d. %s", i+1, trimRunes(o, 90)))
			}
			lines = append(lines, strings.Join(opts, "\n"))
			hint = "点「回答」填表，或直接回复编号/文字。"
		} else {
			hint = "点「回答」填表，或直接回复文字。"
		}
	} else {
		hint = fmt.Sprintf("共 %d 题，点「回答」逐页填写。", len(qs))
	}
	lines = append(lines, "-# "+hint)

	msgID := s.postCard(token, threadID, map[string]any{
		"embeds": []map[string]any{{
			"title": "❓ agent 有问题问你", "description": strings.Join(lines, "\n"), "color": colorBlurbe,
		}},
		"components": []map[string]any{{"type": 1, "components": []map[string]any{{
			"type": 2, "style": 1, "label": "📝 回答",
			"custom_id": "eb:" + nonce + ":0",
		}}}},
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
//   - eb:<nonce>:<page> 「回答」按钮 → 弹第 page 页 modal
//   - ec:<nonce>:<page> 「继续回答」按钮 → 弹下一页 modal
func (s *Service) handleAskComponent(token string, ev interactionEvent) {
	parts := strings.Split(ev.Data.CustomID, ":")
	if len(parts) != 3 {
		return
	}
	prefix, nonce, arg := parts[0], parts[1], parts[2]
	ask := s.currentAsk(ev.ChannelID, nonce)
	if ask == nil {
		s.ephemeral(token, ev, "这张卡已经失效了（处理过或超时）。")
		return
	}
	switch prefix {
	case "pm":
		idx, err := strconv.Atoi(arg)
		if err != nil || idx < 0 || idx >= len(ask.permOpts) {
			return
		}
		s.resolvePermissionAsk(token, ev, ask, idx)
	case "eb", "ec":
		page, _ := strconv.Atoi(arg)
		data := elicitModal(ask.nonce, ask.questions, page)
		if err := interactionCallback(token, ev.ID, ev.Token, 9, data); err != nil {
			slog.Error("弹提问表单失败", "err", err)
		}
	}
}

// handleAskModal 收提问表单的一页（em:<nonce>:<page>）：还有下一页就暂存
// 并给「继续回答」按钮，没有了就整体回传并收口。
func (s *Service) handleAskModal(token string, ev interactionEvent) {
	parts := strings.Split(ev.Data.CustomID, ":")
	if len(parts) != 3 {
		return
	}
	nonce := parts[1]
	page, _ := strconv.Atoi(parts[2])
	ask := s.currentAsk(ev.ChannelID, nonce)
	if ask == nil || ask.kind != "elicitation" {
		s.ephemeral(token, ev, "这张卡已经失效了（处理过或超时）。")
		return
	}

	maps.Copy(ask.partial, parseModalSubmit(ev.Data.Components))
	if elicitRemaining(ask.questions, page) {
		err := interactionCallback(token, ev.ID, ev.Token, 4, map[string]any{
			"content": "这一页收到了，还有几题。",
			"flags":   1 << 6,
			"components": []map[string]any{{"type": 1, "components": []map[string]any{{
				"type": 2, "style": 1, "label": "▶ 继续回答",
				"custom_id": fmt.Sprintf("ec:%s:%d", ask.nonce, page+1),
			}}}},
			"allowed_mentions": noMentions(),
		})
		if err != nil {
			slog.Error("分页续答提示失败", "err", err)
		}
		return
	}

	content := map[string]any{}
	for k, v := range ask.partial {
		content[k] = v
	}
	err := s.acpMgr.ResolveElicitation(ask.key, ask.id,
		acp.ElicitationResult{Action: "accept", Content: content})
	s.clearAsk(ev.ChannelID, ask)
	if err != nil {
		s.ephemeral(token, ev, "这个提问已经失效了（超时或已在别处回答）。")
		return
	}
	s.finalizeAskCard(token, ev.ChannelID, ask, elicitClosedEmbed(ask, ask.partial, "由 "+ev.user()+" 提交"))
	s.ephemeral(token, ev, "✅ 已提交给 agent。")
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
		if len(ask.questions) != 1 {
			s.say(ctx, token, threadID, "这是个多题表单，点卡片上的「回答」逐页填写。")
			return
		}
		q := ask.questions[0]
		answer := text
		if pick >= 0 && pick < len(q.Options) {
			answer = q.Options[pick]
		}
		err = s.acpMgr.ResolveElicitation(ask.key, ask.id,
			acp.ElicitationResult{Action: "accept", Content: map[string]any{q.ID: answer}})
		closed = elicitClosedEmbed(ask, map[string]string{q.ID: answer}, "以消息作答")
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
	if ask.msgID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := botREST(ctx, token, "PATCH",
		fmt.Sprintf("/channels/%s/messages/%s", threadID, ask.msgID), map[string]any{
			"embeds":           []map[string]any{embed},
			"components":       []map[string]any{},
			"allowed_mentions": noMentions(),
		}, nil)
	if err != nil {
		slog.Warn("问答卡收口失败", "err", err)
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
