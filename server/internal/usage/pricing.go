package usage

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"

	"acpp/server/internal/config"
	"acpp/server/internal/model"
)

// ModelPrice 是一个模型的四项单价，单位统一是**美元 / 百万 token**。
//
// 四项分开而不是一个「平均单价」：实测缓存读占全部 token 的 96%，而它
// 只要输入的 1/10——用平均价折算出来的数字会离谱到没有参考价值。
type ModelPrice struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	// Thought 是思考 token 的单价（codex 独有）。留空时按 Output 计——
	// 两端的计费里它本来就属于产出。
	Thought float64 `json:"thought,omitempty"`
}

// zero 报告这条价是不是压根没填。四项全 0 的价不是「免费」，是「没配」。
func (p ModelPrice) zero() bool {
	return p.Input == 0 && p.Output == 0 && p.CacheRead == 0 && p.CacheWrite == 0
}

// PriceTable 是单价表：按模型 id 精确匹配，匹配不上时退到 runtime 方言的
// 兜底价。
//
// **不内置任何默认价**。模型 id 一个月里就能从 claude-fable-5 变成
// claude-fable-5-1，而各家的价目也在动——猜一个数字填进去，报表会拿它
// 一路算下去，没人知道它是编的。没配就是「未计价」，界面如实显示。
type PriceTable struct {
	// Rev 每保存一次加一，落进账目的 PriceRev，用来判断哪些行该重算。
	Rev int `json:"rev"`
	// Models 按模型 id 精确匹配（区分大小写，与协议报上来的一致）。
	Models map[string]ModelPrice `json:"models,omitempty"`
	// Flavors 是按 runtime 方言的兜底价（键是 claude / codex / generic）：
	// claude 报的模型名多半只是档位（default），一个个配没有意义。
	Flavors map[string]ModelPrice `json:"flavors,omitempty"`
}

// lookup 找一条模型该用的价：先精确，再方言兜底；都没有返回 false。
func (t PriceTable) lookup(modelID, flavor string) (ModelPrice, bool) {
	if p, ok := t.Models[modelID]; ok && !p.zero() {
		return p, true
	}
	if p, ok := t.Flavors[strings.ToLower(flavor)]; ok && !p.zero() {
		return p, true
	}
	return ModelPrice{}, false
}

// estimate 按单价算出一行账目的成本（微元）。
func (p ModelPrice) estimate(row *model.TokenUsage) int64 {
	thought := p.Thought
	if thought == 0 {
		thought = p.Output
	}
	// 每项都是「token 数 ÷ 一百万 × 单价」，先乘再除以免整数截断。
	usd := float64(row.InputTokens)*p.Input +
		float64(row.OutputTokens)*p.Output +
		float64(row.CacheReadTokens)*p.CacheRead +
		float64(row.CacheWriteTokens)*p.CacheWrite +
		float64(row.ThoughtTokens)*thought
	return usdToMicro(usd / 1_000_000)
}

// Validate 挡住明显不合理的价：负数与离谱的量级。
//
// 单价表是人手填的，填错一个数量级报表就会给出一个荒唐的总额，而那个
// 数字看起来和真的一样。$10000/M token 已经比任何真实模型贵三个数量级。
func (t PriceTable) Validate() error {
	const maxPerMillion = 10000
	check := func(where string, p ModelPrice) error {
		for name, v := range map[string]float64{
			"input": p.Input, "output": p.Output,
			"cacheRead": p.CacheRead, "cacheWrite": p.CacheWrite,
			"thought": p.Thought,
		} {
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
				return fmt.Errorf("%w: %s 的 %s 单价不是有效数字", ErrInvalid, where, name)
			}
			if v > maxPerMillion {
				return fmt.Errorf("%w: %s 的 %s 单价 %.2f 超出合理范围（每百万 token）",
					ErrInvalid, where, name, v)
			}
		}
		return nil
	}
	for id, p := range t.Models {
		if err := check(id, p); err != nil {
			return err
		}
	}
	for flavor, p := range t.Flavors {
		switch strings.ToLower(flavor) {
		case "claude", "codex", "generic":
		default:
			return fmt.Errorf("%w: 认不出的 runtime 方言 %q", ErrInvalid, flavor)
		}
		if err := check(flavor, p); err != nil {
			return err
		}
	}
	return nil
}

// prices 是账本手上的单价表副本：落账时要用，而保存与读取都可能并发。
type prices struct {
	mu    sync.RWMutex
	table PriceTable
}

func (p *prices) get() PriceTable {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.table
}

func (p *prices) set(t PriceTable) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.table = t
}

// Prices 返回当前单价表。
func (l *Ledger) Prices() PriceTable { return l.prices.get() }

// applyEstimatedCost 给没有实报费用的行折算一个价。
//
// 折算与实报**分开存**（CostSource 与 EstimatedMicro/ReportedMicro）：
// 一个是 agent 自己算的，一个是我们按表乘出来的，合在一起报表就再也
// 说不清「这个数字有多可信」。
func applyEstimatedCost(row *model.TokenUsage, table PriceTable) {
	if row.CostSource == model.CostReported {
		return
	}
	price, ok := table.lookup(row.Model, row.Flavor)
	if !ok {
		row.CostSource = model.CostNone
		return
	}
	row.CostMicro = price.estimate(row)
	row.CostSource = model.CostEstimated
	row.PriceRev = table.Rev
}

// LoadPrices 从本机配置读回单价表（装配期调用一次）。
// 没配过、或配坏了都退回空表——那时所有没有实报费用的轮子如实落
// 「未计价」，而不是拿一张半截的表去算。
func (l *Ledger) LoadPrices() {
	raw := config.SavedUsagePrices()
	if len(raw) == 0 {
		return
	}
	var table PriceTable
	if err := json.Unmarshal(raw, &table); err != nil {
		slog.Warn("usage: 单价表读不出来，按未计价处理", "err", err)
		return
	}
	if err := table.Validate(); err != nil {
		slog.Warn("usage: 单价表不合法，按未计价处理", "err", err)
		return
	}
	l.prices.set(table)
}

// SavePrices 校验并保存单价表，同时热更新账本手上的那一份。
// Rev 由这里递增——调用方不用关心版本号怎么走。
func (l *Ledger) SavePrices(table PriceTable) (PriceTable, error) {
	if err := table.Validate(); err != nil {
		return PriceTable{}, err
	}
	table.Rev = l.prices.get().Rev + 1

	raw, err := json.Marshal(table)
	if err != nil {
		return PriceTable{}, fmt.Errorf("usage: marshal prices: %w", err)
	}
	if err := config.SaveUsagePrices(raw); err != nil {
		return PriceTable{}, fmt.Errorf("usage: save prices: %w", err)
	}
	l.prices.set(table)
	return table, nil
}
