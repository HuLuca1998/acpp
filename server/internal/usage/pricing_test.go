package usage

import (
	"context"
	"errors"
	"testing"
	"time"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
)

// 一轮真实的 codex 用量（取自实测转录）：它一分钱都不报，正是折算要管的那类。
func codexTurn() *acp.Usage {
	return &acp.Usage{
		InputTokens:      575,
		OutputTokens:     770,
		CachedReadTokens: 122752,
		ThoughtTokens:    196,
		TotalTokens:      124097,
	}
}

func TestPricingLeavesUnpricedWhenTableEmpty(t *testing.T) {
	ledger, sessionID := ledgerFixture(t)
	if err := ledger.Record(context.Background(), TurnRecord{
		SessionID: sessionID, StartedAt: time.Now(), EndedAt: time.Now(),
		Usage: codexTurn(), StopReason: string(acp.StopEndTurn),
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got := rowsOf(t, ledger, sessionID)[0]
	if got.CostSource != model.CostNone || got.CostMicro != 0 {
		t.Errorf("空表下 = %d micro (%s)，没配价就该是未计价", got.CostMicro, got.CostSource)
	}
}

// 折算与实报分开：这一栏带 ≈ 还是不带，全看 CostSource。
func TestPricingEstimatesFromFlavorFallback(t *testing.T) {
	ledger, sessionID := ledgerFixture(t)
	// claude 报的模型名多半只是档位，所以兜底价按方言配。这里的会话是
	// claude 方言（见 ledgerFixture），用它验证兜底这条路。
	saved, err := ledger.SavePrices(PriceTable{
		Flavors: map[string]ModelPrice{
			"claude": {Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75},
		},
	})
	if err != nil {
		t.Fatalf("SavePrices: %v", err)
	}
	if saved.Rev != 1 {
		t.Errorf("Rev = %d，首次保存应是 1", saved.Rev)
	}

	if err := ledger.Record(context.Background(), TurnRecord{
		SessionID: sessionID, StartedAt: time.Now(), EndedAt: time.Now(),
		Usage: codexTurn(), StopReason: string(acp.StopEndTurn),
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got := rowsOf(t, ledger, sessionID)[0]
	// (575×3 + 770×15 + 122752×0.3 + 196×15) ÷ 1e6 = $0.053041
	// 思考那项没单独配价，按输出计——两端的计费里它本来就属于产出。
	const want = 53041
	if got.CostMicro != want {
		t.Errorf("折算 = %d micro，期望 %d", got.CostMicro, want)
	}
	if got.CostSource != model.CostEstimated {
		t.Errorf("CostSource = %q，折算出来的应是 %q", got.CostSource, model.CostEstimated)
	}
	if got.PriceRev != 1 {
		t.Errorf("PriceRev = %d，应记下用的是第几版单价表", got.PriceRev)
	}
}

// agent 自己报了费用就不折算——实报永远优先，它不依赖任何表。
func TestPricingNeverOverridesReported(t *testing.T) {
	ledger, sessionID := ledgerFixture(t)
	if _, err := ledger.SavePrices(PriceTable{
		Flavors: map[string]ModelPrice{"claude": {Input: 3, Output: 15}},
	}); err != nil {
		t.Fatalf("SavePrices: %v", err)
	}

	if err := ledger.Record(context.Background(), TurnRecord{
		SessionID: sessionID, StartedAt: time.Now(), EndedAt: time.Now(),
		Usage:      claudeTurn(),
		CostCum:    &acp.UsageCost{Amount: 0.233061, Currency: "USD"},
		StopReason: string(acp.StopEndTurn),
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got := rowsOf(t, ledger, sessionID)[0]
	if got.CostSource != model.CostReported || got.CostMicro != 233061 {
		t.Errorf("= %d micro (%s)，实报在，就不该被折算盖掉", got.CostMicro, got.CostSource)
	}
}

// 精确的模型价压过方言兜底价。
func TestPricingModelBeatsFlavor(t *testing.T) {
	table := PriceTable{
		Models:  map[string]ModelPrice{"gpt-6-astra": {Input: 100}},
		Flavors: map[string]ModelPrice{"codex": {Input: 1}},
	}
	got, ok := table.lookup("gpt-6-astra", "codex")
	if !ok || got.Input != 100 {
		t.Errorf("lookup = %+v (%v)，模型价应压过方言兜底", got, ok)
	}
	got, ok = table.lookup("gpt-5.5", "codex")
	if !ok || got.Input != 1 {
		t.Errorf("lookup = %+v (%v)，认不出的模型该退到方言兜底", got, ok)
	}
	if _, ok := table.lookup("gpt-5.5", "claude"); ok {
		t.Error("两头都没配还返回了价")
	}
}

// 四项全 0 的价是「没配」，不是「免费」——否则一条空记录会把整组变成 $0.00。
func TestPricingZeroPriceIsNotFree(t *testing.T) {
	table := PriceTable{Models: map[string]ModelPrice{"x": {}}}
	if _, ok := table.lookup("x", "codex"); ok {
		t.Error("全零的价被当成了有效单价")
	}
}

// 单价表是人手填的，填错一个数量级报表会给出一个看起来很真的荒唐总额。
func TestPricingValidateRejectsNonsense(t *testing.T) {
	cases := map[string]PriceTable{
		"负数":     {Models: map[string]ModelPrice{"x": {Input: -1}}},
		"离谱量级":   {Models: map[string]ModelPrice{"x": {Output: 99999}}},
		"认不出的方言": {Flavors: map[string]ModelPrice{"gemini": {Input: 1}}},
	}
	for name, table := range cases {
		if err := table.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v，期望 ErrInvalid", name, err)
		}
	}

	ok := PriceTable{
		Models:  map[string]ModelPrice{"gpt-6-astra": {Input: 1.25, Output: 10}},
		Flavors: map[string]ModelPrice{"codex": {Input: 1, Output: 8}},
	}
	if err := ok.Validate(); err != nil {
		t.Errorf("正常的表被拒了: %v", err)
	}
}
