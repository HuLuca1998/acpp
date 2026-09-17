package usage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"acpp/server/internal/model"
	"acpp/server/internal/transcript"
)

// 下面这些行是真实转录的形状（取自实测的 claude 会话，只把正文删掉）：
// 一次握手带出当前模型，随后两轮，各自带工具调用、轮末的费用累计值与
// prompt 响应里的计量。
const (
	lineNew = `{"ts":"2026-08-21T17:50:00+08:00","dir":"recv","msg":{"jsonrpc":"2.0","id":1,` +
		`"result":{"sessionId":"e68163db","configOptions":[{"id":"model","currentValue":"default"},` +
		`{"id":"effort","currentValue":"high"}]}}}`

	linePrompt1 = `{"ts":"2026-08-21T17:51:40+08:00","dir":"send","msg":{"jsonrpc":"2.0","id":2,` +
		`"method":"session/prompt","params":{"sessionId":"e68163db"}}}`
	lineTool1 = `{"ts":"2026-08-21T17:51:44+08:00","dir":"recv","msg":{"jsonrpc":"2.0",` +
		`"method":"session/update","params":{"update":{"sessionUpdate":"tool_call_update",` +
		`"toolCallId":"t1","status":"completed"}}}}`
	lineToolFail = `{"ts":"2026-08-21T17:51:45+08:00","dir":"recv","msg":{"jsonrpc":"2.0",` +
		`"method":"session/update","params":{"update":{"sessionUpdate":"tool_call_update",` +
		`"toolCallId":"t2","status":"failed"}}}}`
	// 同一次调用推两条 update，收敛后只能算一次。
	lineToolDup = `{"ts":"2026-08-21T17:51:46+08:00","dir":"recv","msg":{"jsonrpc":"2.0",` +
		`"method":"session/update","params":{"update":{"sessionUpdate":"tool_call_update",` +
		`"toolCallId":"t1","status":"completed"}}}}`
	lineCost1 = `{"ts":"2026-08-21T17:53:52.555+08:00","dir":"recv","msg":{"jsonrpc":"2.0",` +
		`"method":"session/update","params":{"update":{"sessionUpdate":"usage_update",` +
		`"used":23098,"size":1000000,"cost":{"amount":0.233061,"currency":"USD"}}}}}`
	lineDone1 = `{"ts":"2026-08-21T17:53:52.556+08:00","dir":"recv","msg":{"jsonrpc":"2.0","id":2,` +
		`"result":{"stopReason":"end_turn","usage":{"inputTokens":2,"outputTokens":96,` +
		`"cachedReadTokens":0,"cachedWriteTokens":23000,"totalTokens":23098}}}}`

	linePrompt2 = `{"ts":"2026-08-21T17:54:08+08:00","dir":"send","msg":{"jsonrpc":"2.0","id":3,` +
		`"method":"session/prompt","params":{"sessionId":"e68163db"}}}`
	lineSetModel = `{"ts":"2026-08-21T17:54:09+08:00","dir":"send","msg":{"jsonrpc":"2.0","id":4,` +
		`"method":"session/set_config_option","params":{"sessionId":"e68163db",` +
		`"configId":"model","value":"opus[1m]"}}}`
	lineCost2 = `{"ts":"2026-08-21T17:54:15.481+08:00","dir":"recv","msg":{"jsonrpc":"2.0",` +
		`"method":"session/update","params":{"update":{"sessionUpdate":"usage_update",` +
		`"used":23671,"size":1000000,"cost":{"amount":0.2718955,"currency":"USD"}}}}}`
	lineDone2 = `{"ts":"2026-08-21T17:54:15.482+08:00","dir":"recv","msg":{"jsonrpc":"2.0","id":3,` +
		`"result":{"stopReason":"end_turn","usage":{"inputTokens":4,"outputTokens":376,` +
		`"cachedReadTokens":46249,"cachedWriteTokens":629,"totalTokens":47258}}}}`
)

// backfillFixture 建一条会话 + 一份转录，返回账本与会话 id。
func backfillFixture(t *testing.T, lines ...string) (*Ledger, uint) {
	t.Helper()
	dir := t.TempDir()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(dir, "usage.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.Agent{}, &model.Session{}, &model.TokenUsage{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	agent := model.Agent{Name: "claude", Command: "claude-agent-acp", Flavor: "claude"}
	if err := gdb.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}
	sess := model.Session{AgentID: agent.ID, TenantID: 2, Cwd: dir, State: model.SessionIdle}
	if err := gdb.Create(&sess).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	store, err := transcript.NewStore(filepath.Join(dir, "transcripts"))
	if err != nil {
		t.Fatalf("transcript store: %v", err)
	}
	body := strings.Join(lines, "\n") + "\n"
	path := store.Path(fmt.Sprint(sess.ID))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	return NewLedger(gdb, store), sess.ID
}

func backfilledRows(t *testing.T, ledger *Ledger, sessionID uint) []model.TokenUsage {
	t.Helper()
	var rows []model.TokenUsage
	if err := ledger.db.Where("session_id = ?", sessionID).Order("turn_seq").Find(&rows).Error; err != nil {
		t.Fatalf("load rows: %v", err)
	}
	return rows
}

func TestBackfillRebuildsTurnsFromTranscript(t *testing.T) {
	ledger, sessionID := backfillFixture(t,
		lineNew, linePrompt1, lineTool1, lineToolFail, lineToolDup, lineCost1, lineDone1,
		linePrompt2, lineSetModel, lineCost2, lineDone2)

	n, err := ledger.BackfillSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("BackfillSession: %v", err)
	}
	if n != 2 {
		t.Fatalf("重建行数 = %d，转录里是 2 轮", n)
	}

	rows := backfilledRows(t, ledger, sessionID)
	first, second := rows[0], rows[1]

	if first.TurnSeq != 1 || second.TurnSeq != 2 {
		t.Errorf("轮序号 = %d, %d，应按转录顺序从 1 起", first.TurnSeq, second.TurnSeq)
	}
	if first.CacheWriteTokens != 23000 || second.CacheReadTokens != 46249 {
		t.Errorf("计量没读对: 首轮缓存写 %d，次轮缓存读 %d", first.CacheWriteTokens, second.CacheReadTokens)
	}
	// 成本是会话累计值，逐轮差分后两行之和必须等于末次累计。
	if first.CostMicro != 233061 || second.CostMicro != 38835 {
		t.Errorf("成本差分 = %d, %d micro，期望 233061, 38835", first.CostMicro, second.CostMicro)
	}
	if first.CostSource != model.CostReported {
		t.Errorf("CostSource = %q，claude 报了费用就该是实报", first.CostSource)
	}
	// 工具计数按 toolCallId 收敛：t1 推了两条 update，只能算一次。
	if first.ToolCalls != 2 || first.ToolFailed != 1 {
		t.Errorf("首轮工具计数 = %d/%d，期望 2 次里挂 1 次", first.ToolCalls, first.ToolFailed)
	}
	if second.ToolCalls != 0 {
		t.Errorf("次轮工具计数 = %d，工具事件不该跨轮累积", second.ToolCalls)
	}
	// 模型取的是**这一轮当时**生效的值，中途切了档就跟着变。
	if first.Model != "default" || second.Model != "opus[1m]" {
		t.Errorf("模型 = %q, %q，期望 default → opus[1m]", first.Model, second.Model)
	}
	// 归属从会话定格。
	if first.TenantID != 2 || first.Flavor != "claude" {
		t.Errorf("归属没带上: tenant=%d flavor=%q", first.TenantID, first.Flavor)
	}
	if first.DurationMs != 132556 {
		t.Errorf("DurationMs = %d，应是 prompt 发出到响应的间隔", first.DurationMs)
	}
}

// 重算要能反复跑：这是「事实源是转录」的兑现方式，跑一次和跑三次
// 必须是同一批行。
func TestBackfillIsIdempotent(t *testing.T) {
	ledger, sessionID := backfillFixture(t,
		lineNew, linePrompt1, lineCost1, lineDone1, linePrompt2, lineCost2, lineDone2)

	ctx := context.Background()
	for i := range 3 {
		if _, err := ledger.BackfillSession(ctx, sessionID); err != nil {
			t.Fatalf("第 %d 次 BackfillSession: %v", i+1, err)
		}
	}

	rows := backfilledRows(t, ledger, sessionID)
	if len(rows) != 2 {
		t.Fatalf("跑三次后有 %d 行，期望仍是 2 行", len(rows))
	}
	if rows[0].CostMicro != 233061 || rows[1].CostMicro != 38835 {
		t.Errorf("重复重算后成本变了: %d, %d", rows[0].CostMicro, rows[1].CostMicro)
	}
}

// 实时落账漏过一轮（进程被杀）会让后面的轮序号整体错位一格。重算必须
// 把整条会话拉回与转录一致，而不是在错位的行上叠加。
func TestBackfillReplacesMisalignedRows(t *testing.T) {
	ledger, sessionID := backfillFixture(t,
		lineNew, linePrompt1, lineCost1, lineDone1, linePrompt2, lineCost2, lineDone2)

	// 伪造一条实时留下的脏行：只记到第 1 轮，且计量是错的。
	stale := model.TokenUsage{SessionID: sessionID, TurnSeq: 1, TotalTokens: 999, CostMicro: 999}
	if err := ledger.db.Create(&stale).Error; err != nil {
		t.Fatalf("create stale row: %v", err)
	}
	// 再来一条转录里根本不存在的尾行。
	ghost := model.TokenUsage{SessionID: sessionID, TurnSeq: 7, TotalTokens: 42}
	if err := ledger.db.Create(&ghost).Error; err != nil {
		t.Fatalf("create ghost row: %v", err)
	}

	if _, err := ledger.BackfillSession(context.Background(), sessionID); err != nil {
		t.Fatalf("BackfillSession: %v", err)
	}

	rows := backfilledRows(t, ledger, sessionID)
	if len(rows) != 2 {
		t.Fatalf("重算后有 %d 行，转录里只有 2 轮——幽灵行没被清掉", len(rows))
	}
	if rows[0].TotalTokens != 23098 {
		t.Errorf("首轮计量 = %d，脏行没被转录覆盖", rows[0].TotalTokens)
	}
}

// 有转录、会话记录却没了的，跳过而不是硬记：账目必须有主人，归属只有
// 会话记录知道。
func TestBackfillSkipsOrphanTranscript(t *testing.T) {
	ledger, sessionID := backfillFixture(t, lineNew, linePrompt1, lineCost1, lineDone1)
	if err := ledger.db.Delete(&model.Session{}, sessionID).Error; err != nil {
		t.Fatalf("delete session: %v", err)
	}

	_, err := ledger.BackfillSession(context.Background(), sessionID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v，会话没了应是 ErrNotFound", err)
	}

	res, err := ledger.Backfill(context.Background())
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if res.Orphans != 1 || res.Sessions != 0 || res.Turns != 0 {
		t.Errorf("结果 = %+v，期望只数一份孤儿转录", res)
	}
}

// agent 报错的那一轮同样要落账——token 多半已经烧掉了，而「哪一轮报了
// 什么错」正是异常率要回答的问题。
func TestBackfillKeepsErrorTurn(t *testing.T) {
	lineErr := `{"ts":"2026-09-04T14:05:33+08:00","dir":"recv","msg":{"jsonrpc":"2.0","id":2,` +
		`"error":{"code":-32603,"message":"Internal error: You've hit your session limit"}}}`
	ledger, sessionID := backfillFixture(t, lineNew, linePrompt1, lineErr)

	if _, err := ledger.BackfillSession(context.Background(), sessionID); err != nil {
		t.Fatalf("BackfillSession: %v", err)
	}

	rows := backfilledRows(t, ledger, sessionID)
	if len(rows) != 1 {
		t.Fatalf("报错轮没落账: %d 行", len(rows))
	}
	if rows[0].ErrorCode != -32603 {
		t.Errorf("ErrorCode = %d，期望留下 JSON-RPC 码", rows[0].ErrorCode)
	}
	if !strings.Contains(rows[0].ErrorMsg, "session limit") {
		t.Errorf("ErrorMsg = %q，期望留下原文", rows[0].ErrorMsg)
	}
}
