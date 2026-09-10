package discord

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// 过程卡的两个契约：没人停时卡上一定有对着当前回合的停止按钮；有人停了
// 按钮必须消失（残留按钮会让第二个人以为还没停）。
func TestTurnCardBody(t *testing.T) {
	raw, _ := json.Marshal(turnCardBody("", "n1", ""))
	body := string(raw)
	if !strings.Contains(body, `"custom_id":"st:n1"`) {
		t.Errorf("开场卡缺停止按钮：%s", body)
	}
	if !strings.Contains(body, "正在处理") {
		t.Errorf("开场卡应有「正在处理」占位：%s", body)
	}
	raw, _ = json.Marshal(turnCardBody("### 🔧 工具活动\n🔄 读文件", "n1", "luca"))
	body = string(raw)
	if strings.Contains(body, "custom_id") {
		t.Errorf("按下停止后卡上不该再有按钮：%s", body)
	}
	if !strings.Contains(body, "正在中止") || !strings.Contains(body, "读文件") {
		t.Errorf("停止中的卡应保留工具清单并标「正在中止」：%s", body)
	}
}

func TestFinalTurnLine(t *testing.T) {
	cases := []struct {
		total, failed int
		by, detail    string
		want          string
	}{
		{0, 0, "", "", ""},
		{3, 0, "", "detail", "-# 🔧 3 次工具调用 · 全部完成"},
		{3, 1, "", "detail", "detail"},
		{0, 0, "luca", "", "-# ⏹ 已由 luca 中止"},
		{2, 1, "luca", "detail", "-# ⏹ 已由 luca 中止 · 🔧 2 次工具调用"},
	}
	for _, c := range cases {
		if got := finalTurnLine(c.total, c.failed, c.by, c.detail); got != c.want {
			t.Errorf("finalTurnLine(%d,%d,%q) = %q, want %q", c.total, c.failed, c.by, got, c.want)
		}
	}
}

// 停止的运行态契约：空闲时无活可停；跑着时清队列、署第一个停的人、
// 交出回合 ctx 的 cancel；按钮只认当前回合的代号。
func TestRequestStopAndTurnNonce(t *testing.T) {
	tc := &threadChat{}
	if _, _, active := tc.requestStop("a"); active {
		t.Fatal("空闲子区不该有活可停")
	}
	tctx, cancel := tc.beginTurn(context.Background())
	defer cancel()
	tc.mu.Lock()
	tc.running = true
	tc.queue = []queuedMsg{{msgID: "m1", queued: true}}
	tc.toolLog = []toolEntry{{id: "x"}}
	nonce := tc.turnNonce
	tc.mu.Unlock()
	if nonce == "" || nonce == "-" {
		t.Fatalf("回合代号应为随机串，got %q", nonce)
	}
	if !tc.stopLive(nonce) || tc.stopLive("stale") {
		t.Error("停止按钮应只认当前回合的代号")
	}

	queued, c, active := tc.requestStop("alice")
	if !active || len(queued) != 1 || c == nil {
		t.Fatalf("requestStop = (%d, %v, %v)，应清出 1 条排队并交出 cancel", len(queued), c != nil, active)
	}
	if _, _, again := tc.requestStop("bob"); !again {
		t.Error("回合还在跑时再停一次仍算有活可停")
	}
	tc.mu.Lock()
	by, left := tc.stoppedBy, len(tc.queue)
	tc.mu.Unlock()
	if by != "alice" || left != 0 {
		t.Errorf("stoppedBy=%q queue=%d，应署第一个停的人且队列清空", by, left)
	}
	c()
	if tctx.Err() == nil {
		t.Error("cancel 后回合 ctx 应已结束")
	}

	// 下一轮开场必须把上一轮的工具卡状态与署名清干净（跨回合累加是老 bug）。
	_, cancel2 := tc.beginTurn(context.Background())
	defer cancel2()
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if tc.stoppedBy != "" || len(tc.toolLog) != 0 || tc.toolMsgID != "" || tc.turnNonce == nonce {
		t.Errorf("beginTurn 没有重置回合状态：stoppedBy=%q tools=%d msg=%q nonce=%q", tc.stoppedBy, len(tc.toolLog), tc.toolMsgID, tc.turnNonce)
	}
}
