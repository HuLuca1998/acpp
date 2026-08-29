package discord

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"acpp/server/internal/acp"
)

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

// noteToolCall 消费一条工具事件：新调用追加，老调用更新状态/标题。
func (s *Service) noteToolCall(token, threadID string, tc *threadChat, ev acp.Event) {
	tc.mu.Lock()
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
