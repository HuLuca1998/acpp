package usage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
)

// ledgerFixture 建一条真实会话现场：一个 claude 工具 + 一条属于租户 2 的
// 会话，模型快照是 claude 实际会报的档位名 "default"。
func ledgerFixture(t *testing.T) (*Ledger, uint) {
	t.Helper()
	// 把家目录钉在临时目录里：单价表存 ~/.acpp/config.json，不隔离的话
	// 跑一次测试就把开发机上真实的配置改了。
	t.Setenv("HOME", t.TempDir())
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "usage.db")), &gorm.Config{})
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
	sess := model.Session{
		AgentID:      agent.ID,
		TenantID:     2,
		Cwd:          t.TempDir(),
		State:        model.SessionActive,
		LastSettings: model.JSONMap{"model": "default", "effort": "high"},
	}
	if err := gdb.Create(&sess).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	return NewLedger(gdb, nil), sess.ID
}

// rowsOf 按轮序读出这条会话的全部账目。
func rowsOf(t *testing.T, ledger *Ledger, sessionID uint) []model.TokenUsage {
	t.Helper()
	var rows []model.TokenUsage
	if err := ledger.db.Where("session_id = ?", sessionID).Order("turn_seq").Find(&rows).Error; err != nil {
		t.Fatalf("load rows: %v", err)
	}
	return rows
}

// 一轮真实的 claude 用量（取自实测转录：缓存读占绝大头，输出才是新产出的）。
func claudeTurn() *acp.Usage {
	return &acp.Usage{
		InputTokens:       4,
		OutputTokens:      376,
		CachedReadTokens:  46249,
		CachedWriteTokens: 629,
		TotalTokens:       47258,
	}
}

func TestUsageRecordCapturesOwnershipAndTokens(t *testing.T) {
	ledger, sessionID := ledgerFixture(t)
	start := time.Now().Add(-90 * time.Second)

	err := ledger.Record(context.Background(), TurnRecord{
		SessionID:  sessionID,
		StartedAt:  start,
		EndedAt:    start.Add(64 * time.Second),
		Usage:      claudeTurn(),
		CostCum:    &acp.UsageCost{Amount: 0.2718955, Currency: "USD"},
		StopReason: string(acp.StopEndTurn),
		ToolCalls:  12,
		ToolFailed: 1,
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	rows := rowsOf(t, ledger, sessionID)
	if len(rows) != 1 {
		t.Fatalf("落账行数 = %d，期望 1", len(rows))
	}
	got := rows[0]
	if got.TurnSeq != 1 {
		t.Errorf("TurnSeq = %d，首轮应从 1 起", got.TurnSeq)
	}
	if got.TenantID != 2 || got.Flavor != "claude" || got.Model != "default" {
		t.Errorf("归属没从会话取到: tenant=%d flavor=%q model=%q", got.TenantID, got.Flavor, got.Model)
	}
	if got.Origin != model.OriginUI {
		t.Errorf("Origin = %q，空来源应归一到 %q", got.Origin, model.OriginUI)
	}
	// 四项分开存是这张表的立身之本：合成一个总数就说不清钱花在哪。
	if got.CacheReadTokens != 46249 || got.CacheWriteTokens != 629 {
		t.Errorf("缓存读写没分开存: read=%d write=%d", got.CacheReadTokens, got.CacheWriteTokens)
	}
	if got.OutputTokens != 376 || got.TotalTokens != 47258 {
		t.Errorf("计量不符: out=%d total=%d", got.OutputTokens, got.TotalTokens)
	}
	if got.DurationMs != 64000 {
		t.Errorf("DurationMs = %d，期望 64000", got.DurationMs)
	}
	if got.ToolCalls != 12 || got.ToolFailed != 1 {
		t.Errorf("工具计数不符: calls=%d failed=%d", got.ToolCalls, got.ToolFailed)
	}
	if got.CostSource != model.CostReported || got.CostMicro != 271896 {
		t.Errorf("首轮成本 = %d micro (%s)，期望 271896 reported", got.CostMicro, got.CostSource)
	}
}

// agent 报的 cost 是会话累计值，本轮花了多少必须减去上一轮——直接记累计值
// 会让一条 10 轮的会话把费用重复计十遍。
func TestUsageRecordCostIsPerTurnDelta(t *testing.T) {
	ledger, sessionID := ledgerFixture(t)
	ctx := context.Background()
	// 取自实测转录 101.jsonl 的三轮累计值。
	for _, cum := range []float64{0.233061, 0.2718955, 0.303652} {
		if err := ledger.Record(ctx, TurnRecord{
			SessionID:  sessionID,
			StartedAt:  time.Now(),
			EndedAt:    time.Now(),
			Usage:      claudeTurn(),
			CostCum:    &acp.UsageCost{Amount: cum, Currency: "USD"},
			StopReason: string(acp.StopEndTurn),
		}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	rows := rowsOf(t, ledger, sessionID)
	if len(rows) != 3 {
		t.Fatalf("落账行数 = %d，期望 3", len(rows))
	}
	// 差分在**整数域**做（先各自转 micro 再相减），所以三轮之和精确等于
	// 末次累计值。换成浮点差分再取整，这里会多出 1 micro 对不上账。
	want := []int64{233061, 38835, 31756}
	var sum int64
	for i, row := range rows {
		if row.TurnSeq != i+1 {
			t.Errorf("第 %d 行 TurnSeq = %d，轮序应连续", i, row.TurnSeq)
		}
		if row.CostMicro != want[i] {
			t.Errorf("第 %d 轮成本 = %d micro，期望 %d", i+1, row.CostMicro, want[i])
		}
		sum += row.CostMicro
	}
	// 逐轮差分加起来必须等于最后一次累计值，否则账就是错的。
	if sum != 303652 {
		t.Errorf("三轮合计 = %d micro，期望等于末次累计 303652", sum)
	}
}

// 跨 session/load 恢复后 agent 可能把累计值清零，差分会变成负数。
// 那时按「新起点」记，绝不能让一个负数把合计拉低。
func TestUsageRecordHandlesCostReset(t *testing.T) {
	ledger, sessionID := ledgerFixture(t)
	ctx := context.Background()
	rec := TurnRecord{
		SessionID:  sessionID,
		StartedAt:  time.Now(),
		EndedAt:    time.Now(),
		Usage:      claudeTurn(),
		StopReason: string(acp.StopEndTurn),
	}
	rec.CostCum = &acp.UsageCost{Amount: 5.0, Currency: "USD"}
	if err := ledger.Record(ctx, rec); err != nil {
		t.Fatalf("Record 第一轮: %v", err)
	}
	// 进程重开，agent 从头累计。
	rec.CostCum = &acp.UsageCost{Amount: 0.4, Currency: "USD"}
	if err := ledger.Record(ctx, rec); err != nil {
		t.Fatalf("Record 第二轮: %v", err)
	}

	rows := rowsOf(t, ledger, sessionID)
	if len(rows) != 2 {
		t.Fatalf("落账行数 = %d，期望 2", len(rows))
	}
	if rows[1].CostMicro != 400000 {
		t.Errorf("重置后成本 = %d micro，期望按新起点记 400000", rows[1].CostMicro)
	}
	if rows[1].CostCumMicro != 400000 {
		t.Errorf("累计基准 = %d micro，期望跟着新起点走", rows[1].CostCumMicro)
	}
}

// codex 一分钱都不报。那一栏必须是「不知道」，不是零——界面要分得开。
func TestUsageRecordUnreportedCostIsNotZero(t *testing.T) {
	ledger, sessionID := ledgerFixture(t)
	err := ledger.Record(context.Background(), TurnRecord{
		SessionID: sessionID,
		StartedAt: time.Now(),
		EndedAt:   time.Now(),
		Usage: &acp.Usage{
			InputTokens: 575, OutputTokens: 770,
			CachedReadTokens: 122752, ThoughtTokens: 196, TotalTokens: 124097,
		},
		StopReason: string(acp.StopEndTurn),
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	got := rowsOf(t, ledger, sessionID)[0]
	if got.CostSource != model.CostNone {
		t.Errorf("CostSource = %q，不报费用时应是 %q", got.CostSource, model.CostNone)
	}
	if got.ThoughtTokens != 196 {
		t.Errorf("ThoughtTokens = %d，codex 独有的计量不该被丢掉", got.ThoughtTokens)
	}
}

// 报错的那一轮 token 多半已经烧掉了，而「哪些轮报了什么错」正是异常率
// 要回答的问题——所以它必须落账，哪怕 usage 是空的。
func TestUsageRecordKeepsFailedTurn(t *testing.T) {
	ledger, sessionID := ledgerFixture(t)
	err := ledger.Record(context.Background(), TurnRecord{
		SessionID: sessionID,
		StartedAt: time.Now(),
		EndedAt:   time.Now(),
		Err:       errors.New("session/prompt: agent error -32603: Internal error: API Error: 529 Overloaded"),
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	rows := rowsOf(t, ledger, sessionID)
	if len(rows) != 1 {
		t.Fatalf("失败轮没落账: %d 行", len(rows))
	}
	if rows[0].ErrorMsg == "" {
		t.Error("ErrorMsg 为空，失败原因应当留存")
	}
	if rows[0].TotalTokens != 0 || rows[0].CostMicro != 0 {
		t.Errorf("无计量的失败轮不该凭空造出数字: total=%d cost=%d", rows[0].TotalTokens, rows[0].CostMicro)
	}
}

func TestUsageRecordRejectsUnknownSession(t *testing.T) {
	ledger, _ := ledgerFixture(t)
	err := ledger.Record(context.Background(), TurnRecord{SessionID: 9999, StartedAt: time.Now()})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v，会话不存在时应是 ErrNotFound", err)
	}
}

// 外部子系统（Discord）不写会话的设置快照，模型只能由那一侧直接给。
// 给了就用它，不给才退回快照——两条路都要成立。
func TestRecordTakesModelFromCaller(t *testing.T) {
	ledger, sessionID := ledgerFixture(t)
	ctx := context.Background()

	if err := ledger.Record(ctx, TurnRecord{
		SessionID: sessionID, StartedAt: time.Now(), EndedAt: time.Now(),
		Usage: claudeTurn(), StopReason: string(acp.StopEndTurn),
		Model: "opus[1m]",
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := ledger.Record(ctx, TurnRecord{
		SessionID: sessionID, StartedAt: time.Now(), EndedAt: time.Now(),
		Usage: claudeTurn(), StopReason: string(acp.StopEndTurn),
	}); err != nil {
		t.Fatalf("Record 第二轮: %v", err)
	}

	rows := rowsOf(t, ledger, sessionID)
	if rows[0].Model != "opus[1m]" {
		t.Errorf("第一轮 Model = %q，调用方给了就该用它", rows[0].Model)
	}
	// ledgerFixture 的会话快照里是 "default"。
	if rows[1].Model != "default" {
		t.Errorf("第二轮 Model = %q，没给就该退回会话快照", rows[1].Model)
	}
}
