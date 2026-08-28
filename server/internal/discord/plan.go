package discord

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// 任务计划卡：agent 的 plan 更新（网页端的 PlanCard）在子区里表现为
// **一回合一张、原地刷新**的 V2 卡——计划每变一次就 PATCH 同一条消息，
// 回合结束时卡上就是终态，历史回合的计划卡留在对话流里当记录。

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
		"components": v2Container(colorBlurbe, planLines(entries)),
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
