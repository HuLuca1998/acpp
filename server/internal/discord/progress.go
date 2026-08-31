package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"acpp/server/internal/acp"
)

// 回合过程卡：计划卡与工具活动卡。同一模式——一回合一张、原地 PATCH
// 刷新、generation 计数防旧状态倒灌；回合结束各自定格/收口。

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

// 工具活动卡：网页会话里每个工具调用都有一张卡，discord 收敛成
// **一回合一张、原地刷新**的清单（与计划卡同一模式）——既能看到 agent
// 正在干什么，又不会刷满频道。同一调用的流式状态更新只在「渲染结果
// 变了」时才 PATCH，rawInput 分片风暴打不到平台限速上。

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
	tc.mu.Unlock()

	body := map[string]any{
		"flags":      1 << 15,
		"components": v2Container(colorGrey, []map[string]any{v2Text(rendered)}),
	}
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
		slog.Warn("刷新工具卡失败", "err", err)
	}
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

// finalizeToolCard 在回合结束时给工具活动卡收口：全部成功就压成一行
// ——对话滚动区里一张 8 行的过程清单在事后只是噪音；有失败的保留明细
// （排查要看是哪一步挂的）。
func (s *Service) finalizeToolCard(token, threadID string, tc *threadChat) {
	tc.mu.Lock()
	msgID := tc.toolMsgID
	total := len(tc.toolLog)
	failed := 0
	for _, e := range tc.toolLog {
		if e.status == "failed" {
			failed++
		}
	}
	tc.mu.Unlock()
	if msgID == "" || total == 0 || failed > 0 {
		return
	}
	body := map[string]any{
		"flags": 1 << 15,
		"components": v2Container(colorGrey, []map[string]any{
			v2Text(fmt.Sprintf("-# 🔧 %d 次工具调用 · 全部完成", total)),
		}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := botREST(ctx, token, "PATCH", "/channels/"+threadID+"/messages/"+msgID, body, nil); err != nil {
		slog.Warn("工具卡收口失败", "err", err)
	}
}
