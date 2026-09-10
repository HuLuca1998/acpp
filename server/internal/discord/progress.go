package discord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"acpp/server/internal/acp"
)

// 回合过程卡：计划卡与工具活动卡（后者也是「⏹ 停止」按钮的落点，中止
// 逻辑在文件末尾）。同一模式——一回合一张、原地 PATCH 刷新、generation
// 计数防旧状态倒灌；回合结束各自定格/收口。

// planEntry 是 ACP plan entries 里的一项（只解渲染用得到的字段）。
type planEntry struct {
	Content string `json:"content"`
	Status  string `json:"status"` // pending / in_progress / completed
}

// updatePlanCard 消费一次 plan 更新。事件在独立 goroutine 里进来，
// generation 计数保证晚到的旧状态不会盖掉新状态：取号在事件到达时，
// 发送前再核对一次，自己已经不是最新就直接放弃。
func (s *Service) updatePlanCard(token, threadID string, tc *threadChat, raw json.RawMessage) {
	var entries []planEntry
	if err := json.Unmarshal(raw, &entries); err != nil || len(entries) == 0 {
		return
	}

	tc.mu.Lock()
	tc.planGen++
	gen := tc.planGen
	tc.mu.Unlock()

	body := map[string]any{
		"flags":      1 << 15,
		"components": v2Container(colorGrey, planLines(entries)),
	}

	tc.mu.Lock()
	if gen != tc.planGen {
		tc.mu.Unlock()
		return
	}
	msgID := tc.planMsgID
	tc.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if msgID == "" {
		id := s.postCard(token, threadID, body)
		if id == "" {
			return
		}
		tc.mu.Lock()
		// 并发首发只留先到的那张；自己发的那张多余就删掉。
		if tc.planMsgID == "" {
			tc.planMsgID = id
			tc.mu.Unlock()
			return
		}
		tc.mu.Unlock()
		_ = botREST(ctx, token, "DELETE", "/channels/"+threadID+"/messages/"+id, nil, nil)
		return
	}
	if err := botREST(ctx, token, "PATCH", "/channels/"+threadID+"/messages/"+msgID, body, nil); err != nil {
		slog.Warn("刷新计划卡失败", "err", err)
	}
}

// planLines 把计划渲染成逐项一行：✅ 划线 / 🔄 加粗 / ⬜ 素文。
func planLines(entries []planEntry) []map[string]any {
	done := 0
	for _, e := range entries {
		if e.Status == "completed" {
			done++
		}
	}
	inner := []map[string]any{
		v2Text("### 📋 计划"),
	}
	var lines string
	for _, e := range entries {
		c := trimRunes(e.Content, 150)
		switch e.Status {
		case "completed":
			lines += "✅ ~~" + c + "~~\n"
		case "in_progress":
			lines += "🔄 **" + c + "**\n"
		default:
			lines += "⬜ " + c + "\n"
		}
	}
	inner = append(inner, v2Text(lines))
	if done == len(entries) {
		inner = append(inner, v2Text("-# 全部完成"))
	}
	return inner
}

// 过程卡：网页会话里每个工具调用都有一张卡，discord 收敛成**一回合一张、
// 原地刷新**的清单（与计划卡同一模式）——既能看到 agent 正在干什么，又不会
// 刷满频道。同一调用的流式状态更新只在「渲染结果变了」时才 PATCH，rawInput
// 分片风暴打不到平台限速上。
//
// 卡从回合一开始就发（还没有工具调用时只有一行「正在处理」），因为它同时
// 是「⏹ 停止」按钮的落点：操作人员不知道有 /stop，中止必须看得见、点得到，
// 而且会话启动、长思考这些没有工具调用的阶段也要能停。收口规则见
// finalizeTurnCard：没干活的回合整张删掉，频道不留痕。

// toolEntry 是一次工具调用在卡上的一行。
type toolEntry struct {
	id     string
	title  string
	status string
}

// noteToolCall 消费一条工具事件：新调用追加，老调用更新状态/标题；
// edit 类调用的文件路径顺带收进 touched（回合小结报改动文件数）。
func (s *Service) noteToolCall(token, threadID string, tc *threadChat, ev acp.Event) {
	tc.mu.Lock()
	if (ev.ToolKind == "edit" || ev.ToolKind == "delete" || ev.ToolKind == "move") && len(ev.Locations) > 0 {
		var locs []struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(ev.Locations, &locs) == nil {
			if tc.touched == nil {
				tc.touched = map[string]struct{}{}
			}
			for _, l := range locs {
				if l.Path != "" {
					tc.touched[l.Path] = struct{}{}
				}
			}
		}
	}
	found := false
	for i := range tc.toolLog {
		if tc.toolLog[i].id == ev.ToolCallID {
			if ev.Title != "" {
				tc.toolLog[i].title = ev.Title
			}
			if ev.Status != "" {
				tc.toolLog[i].status = ev.Status
			}
			found = true
			break
		}
	}
	if !found {
		tc.toolLog = append(tc.toolLog, toolEntry{id: ev.ToolCallID, title: ev.Title, status: ev.Status})
	}
	rendered := renderToolCard(tc.toolLog)
	if rendered == tc.toolRendered {
		tc.mu.Unlock()
		return
	}
	tc.toolRendered = rendered
	tc.toolGen++
	gen := tc.toolGen
	msgID := tc.toolMsgID
	body := turnCardBody(rendered, tc.turnNonce, tc.stoppedBy)
	tc.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if msgID == "" {
		id := s.postCard(token, threadID, body)
		if id == "" {
			return
		}
		tc.mu.Lock()
		if tc.toolMsgID == "" {
			tc.toolMsgID = id
			tc.mu.Unlock()
			return
		}
		tc.mu.Unlock()
		_ = botREST(ctx, token, "DELETE", "/channels/"+threadID+"/messages/"+id, nil, nil)
		return
	}
	tc.mu.Lock()
	stale := gen != tc.toolGen
	tc.mu.Unlock()
	if stale {
		return
	}
	if err := botREST(ctx, token, "PATCH", "/channels/"+threadID+"/messages/"+msgID, body, nil); err != nil {
		slog.Warn("刷新过程卡失败", "err", err)
	}
}

// turnCardBody 渲染过程卡：正文（工具清单，或开场的「正在处理」一行）
// 右侧挂「⏹ 停止」按钮；已有人按下停止就换成「正在中止」行、不再给按钮
// （终态由 finalizeTurnCard 写）。
func turnCardBody(text, nonce, stoppedBy string) map[string]any {
	if text == "" {
		text = "-# ⏳ 正在处理…"
	}
	var inner []map[string]any
	if stoppedBy != "" {
		inner = []map[string]any{v2Text(text), v2Text("-# ⏹ 正在中止…")}
	} else {
		inner = []map[string]any{v2Section(text, v2Button("⏹ 停止", stopPrefix+nonce, 2))}
	}
	return map[string]any{"flags": 1 << 15, "components": v2Container(colorGrey, inner)}
}

// postTurnCard 在回合开场发过程卡（此刻只有「正在处理」+ 停止按钮）。
// 发失败不拦回合：首次工具调用时 noteToolCall 会再补发一张。
func (s *Service) postTurnCard(token, threadID string, tc *threadChat) {
	tc.mu.Lock()
	body := turnCardBody("", tc.turnNonce, tc.stoppedBy)
	tc.mu.Unlock()
	id := s.postCard(token, threadID, body)
	if id == "" {
		return
	}
	tc.mu.Lock()
	tc.toolMsgID = id
	tc.mu.Unlock()
}

// renderToolCard 渲染工具清单：只显示末 8 条，更早的折叠成计数。
func renderToolCard(log []toolEntry) string {
	const show = 8
	var b strings.Builder
	b.WriteString("### 🔧 工具活动\n")
	if len(log) > show {
		fmt.Fprintf(&b, "-# …前面还有 %d 条\n", len(log)-show)
	}
	start := max(0, len(log)-show)
	for _, e := range log[start:] {
		mark := "🔄"
		switch e.status {
		case "completed":
			mark = "✅"
		case "failed":
			mark = "❌"
		}
		b.WriteString(mark + " " + trimRunes(orDefault(e.title, "（未命名调用）"), 90) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// finalizeTurnCard 在回合结束时给过程卡收口（按钮一律摘掉）：
//   - 被人中止：一行「⏹ 已由谁中止」（干过活的带工具计数）；
//   - 没有工具调用：整张删掉——纯问答的对话区里不该多一张空卡；
//   - 有失败：保留明细（排查要看是哪一步挂的）；
//   - 全部成功：压成一行——8 行过程清单在事后只是噪音。
func (s *Service) finalizeTurnCard(token, threadID string, tc *threadChat) {
	tc.mu.Lock()
	msgID := tc.toolMsgID
	total := len(tc.toolLog)
	failed := 0
	for _, e := range tc.toolLog {
		if e.status == "failed" {
			failed++
		}
	}
	stoppedBy := tc.stoppedBy
	detail := renderToolCard(tc.toolLog)
	tc.mu.Unlock()
	if msgID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	text := finalTurnLine(total, failed, stoppedBy, detail)
	if text == "" {
		if err := botREST(ctx, token, "DELETE", "/channels/"+threadID+"/messages/"+msgID, nil, nil); err != nil {
			slog.Warn("删过程卡失败", "err", err)
		}
		return
	}
	body := map[string]any{
		"flags":      1 << 15,
		"components": v2Container(colorGrey, []map[string]any{v2Text(text)}),
	}
	if err := botREST(ctx, token, "PATCH", "/channels/"+threadID+"/messages/"+msgID, body, nil); err != nil {
		slog.Warn("过程卡收口失败", "err", err)
	}
}

// finalTurnLine 是过程卡的终态文案；空串表示这张卡该删掉。
func finalTurnLine(total, failed int, stoppedBy, detail string) string {
	switch {
	case stoppedBy != "":
		line := "-# ⏹ 已由 " + stoppedBy + " 中止"
		if total > 0 {
			line += fmt.Sprintf(" · 🔧 %d 次工具调用", total)
		}
		return line
	case total == 0:
		return ""
	case failed > 0:
		return detail
	default:
		return fmt.Sprintf("-# 🔧 %d 次工具调用 · 全部完成", total)
	}
}

// 回合中止的唯一入口：/stop 命令与过程卡上的「⏹ 停止」按钮都走 stopTurn。
// 操作人员实测不知道有 /stop（手册与 /help 也没提），所以中止必须是子区
// 里看得见、点得到的按钮——过程卡从回合一开始就在（见 progress.go）。

// stopPrefix 是停止按钮 custom_id 的前缀，后面跟回合代号（threadChat.
// turnNonce）：旧卡残留的按钮对不上号就不作数。新增前缀要登记进
// handleInteraction（本包规范）。
const stopPrefix = "st:"

// beginTurn 给一轮做开场登记：发回合代号、建可掐的回合 ctx、清上一轮的
// 过程卡状态。回合 ctx 覆盖会话启动那一段——prompt 还没发出去时 acp 层的
// Cancel 够不着，只能掐 ctx 让握手提前退出。
func (tc *threadChat) beginTurn(ctx context.Context) (context.Context, context.CancelFunc) {
	nonce, err := randomID()
	if err != nil {
		nonce = "-"
	}
	tctx, cancel := context.WithCancel(ctx)
	tc.mu.Lock()
	tc.turnNonce = nonce
	tc.turnCancel = cancel
	tc.stoppedBy = ""
	tc.toolLog, tc.toolMsgID, tc.toolGen, tc.toolRendered = nil, "", 0, ""
	tc.touched = nil
	tc.mu.Unlock()
	return tctx, cancel
}

// requestStop 把中止意图记到运行态上：清空排队输入、作废挂起的问答、
// 署上是谁停的（第一个停的人算数）。返回被清掉的排队消息、回合 ctx 的
// cancel，以及是否真有活可停。纯内存操作，网络动作留给调用方。
func (tc *threadChat) requestStop(by string) (queued []queuedMsg, cancel context.CancelFunc, active bool) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	queued = tc.queue
	tc.queue = nil
	tc.asks = nil
	if !tc.running && len(queued) == 0 {
		return nil, nil, false
	}
	if tc.running && tc.stoppedBy == "" {
		tc.stoppedBy = orDefault(by, "用户")
	}
	return queued, tc.turnCancel, true
}

// stopLive 报告一颗停止按钮是否还对着正在跑的那一轮。
func (tc *threadChat) stopLive(nonce string) bool {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.running && tc.turnNonce == nonce
}

// stopTurn 中止子区当前回合：先让 acp 发 session/cancel（agent 体面收手，
// prompt 按 cancelled 收尾），再掐回合 ctx（兜住会话还在启动的窗口）；被清
// 掉的排队消息摘掉 ⏳——留着会让人以为它还会被带上。返回是否真有活可停。
func (s *Service) stopTurn(ctx context.Context, token, threadID, by string) bool {
	tc := s.chatState(threadID)
	queued, cancel, active := tc.requestStop(by)
	if !active {
		return false
	}
	if err := s.acpMgr.Cancel("dc:" + threadID); err != nil && !errors.Is(err, acp.ErrNoSession) {
		slog.Warn("中止子区回合失败", "thread", threadID, "err", err)
	}
	if cancel != nil {
		cancel()
	}
	// interaction 回调限时 3 秒，reaction 又限速狠，摘标记放后台。
	go func() {
		for _, q := range queued {
			if !q.queued {
				continue
			}
			s.unreact(ctx, token, orDefault(q.mainChannel, threadID), q.msgID, "⏳")
		}
	}()
	return true
}

// stopThread 处理 /stop。
func (s *Service) stopThread(ctx context.Context, token string, ev interactionEvent) {
	if s.acpMgr == nil {
		s.ephemeral(token, ev, "对话功能没有启用。")
		return
	}
	if !s.stopTurn(ctx, token, ev.ChannelID, ev.user()) {
		s.ephemeral(token, ev, "这里没有正在跑的回合。")
		return
	}
	s.ephemeral(token, ev, "⏹ 已中止，排队的输入也清掉了。")
}

// stopClicked 处理过程卡上的「⏹ 停止」：先把卡原地改成「正在中止」
// （type 7 = UPDATE_MESSAGE，V2 消息只能给 components），终态由回合收口
// 时写（finalizeTurnCard）；按钮对不上当前回合就只给本人回一句。
func (s *Service) stopClicked(ctx context.Context, token string, ev interactionEvent) {
	nonce := strings.TrimPrefix(ev.Data.CustomID, stopPrefix)
	tc := s.chatState(ev.ChannelID)
	if s.acpMgr == nil || !tc.stopLive(nonce) {
		s.ephemeral(token, ev, "这一轮已经结束了。")
		return
	}
	if err := interactionCallback(token, ev.ID, ev.Token, 7, map[string]any{
		"flags":      1 << 15,
		"components": v2Container(colorGrey, []map[string]any{v2Text("-# ⏹ 正在中止…")}),
	}); err != nil {
		slog.Warn("停止按钮回执失败", "err", err)
	}
	s.stopTurn(ctx, token, ev.ChannelID, ev.user())
}
