// Package usage 是轮次用量账本：把每一轮对话的 token 与费用落成一行，
// 供报表按身份、项目、工具、模型等维度聚合。
//
// **事实源是转录 JSONL，不是这张表**——每轮的计量本来就写在 session/prompt
// 的响应里，账本只是把它读出来建的索引，任何时候都能照转录重算。因此写入
// 是旁路：失败只记日志，绝不打断对话本身（与转录同一条原则）。
//
// 本包只 import 叶子包（model / acp / gitrepo），不回头依赖 service。
package usage

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"acpp/server/internal/acp"
	"acpp/server/internal/gitrepo"
	"acpp/server/internal/model"
)

// 可预期失败。httpapi 的 writeError 里映射状态码（与 service 的同义）。
var (
	ErrInvalid  = errors.New("usage: invalid")
	ErrNotFound = errors.New("usage: not found")
)

// Ledger 是账本本体：写入、回填与聚合都挂在它上面。
type Ledger struct {
	db *gorm.DB
}

func NewLedger(db *gorm.DB) *Ledger {
	return &Ledger{db: db}
}

// TurnRecord 是一轮结束时交给账本的原始事实。
// 归属（租户、项目、模型）由账本自己从会话查，调用方不用组装。
type TurnRecord struct {
	SessionID uint
	StartedAt time.Time
	EndedAt   time.Time
	// Usage 是 agent 报的本轮计量；一轮报错时可能为 nil，那样也要记一行
	// ——「这一轮失败了」本身就是账本要回答的问题。
	Usage *acp.Usage
	// CostCum 是 agent 报的会话**累计**费用，nil 表示这端不报（codex）。
	CostCum *acp.UsageCost
	// StopReason 是轮次收尾原因；Err 是 agent 报回来的错误（可为 nil）。
	StopReason string
	Err        error
	ToolCalls  int
	ToolFailed int
}

// errMsgLimit 是错误文案的留存长度。够看清是哪一类错误即可，原文完整
// 留在转录里。
const errMsgLimit = 256

// Record 把一轮的用量落进账本。
//
// 轮序号与成本差分都在这里算：成本那一项 agent 报的是**会话累计值**，
// 本轮花了多少要减去上一轮的累计——所以每行都留一份累计原值，下一轮拿它
// 作基准，不依赖任何内存状态（进程重启、会话恢复都不会算错）。
func (l *Ledger) Record(ctx context.Context, rec TurnRecord) error {
	if rec.SessionID == 0 {
		return fmt.Errorf("%w: usage record without session", ErrInvalid)
	}

	var sess model.Session
	if err := l.db.WithContext(ctx).First(&sess, rec.SessionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: session %d", ErrNotFound, rec.SessionID)
		}
		return fmt.Errorf("usage: load session: %w", err)
	}

	var flavor string
	if sess.AgentID != 0 {
		var agent model.Agent
		if err := l.db.WithContext(ctx).Select("flavor").First(&agent, sess.AgentID).Error; err == nil {
			flavor = agent.Flavor
		}
	}

	// 上一行给两样东西：轮序号的起点，与成本差分的基准。
	var prev model.TokenUsage
	hasPrev := l.db.WithContext(ctx).
		Where("session_id = ?", rec.SessionID).
		Order("turn_seq desc").First(&prev).Error == nil

	row := model.TokenUsage{
		SessionID:  rec.SessionID,
		TurnSeq:    prev.TurnSeq + 1,
		TenantID:   sess.TenantID,
		AgentID:    sess.AgentID,
		Flavor:     flavor,
		Model:      settingsModel(sess.LastSettings),
		Project:    gitrepo.ProjectOf(sess.Cwd),
		Cwd:        sess.Cwd,
		Origin:     normalizeOrigin(sess.Origin),
		StartedAt:  rec.StartedAt,
		EndedAt:    rec.EndedAt,
		StopReason: rec.StopReason,
		ToolCalls:  rec.ToolCalls,
		ToolFailed: rec.ToolFailed,
	}
	if !hasPrev {
		row.TurnSeq = 1
	}
	if !rec.EndedAt.IsZero() && !rec.StartedAt.IsZero() {
		row.DurationMs = rec.EndedAt.Sub(rec.StartedAt).Milliseconds()
	}
	if u := rec.Usage; u != nil {
		row.InputTokens = u.InputTokens
		row.OutputTokens = u.OutputTokens
		row.CacheReadTokens = u.CachedReadTokens
		row.CacheWriteTokens = u.CachedWriteTokens
		row.ThoughtTokens = u.ThoughtTokens
		row.TotalTokens = u.TotalTokens
	}
	if rec.Err != nil {
		row.ErrorCode = acp.ErrorCode(rec.Err)
		row.ErrorMsg = truncate(rec.Err.Error(), errMsgLimit)
	}
	applyReportedCost(&row, rec.CostCum, prev.CostCumMicro)

	return l.upsert(ctx, &row)
}

// upsert 按 <会话, 轮序号> 写入。唯一键让回填天然幂等：重扫几次转录，
// 结果都是同一批行，而不是每扫一次就多一份账。
func (l *Ledger) upsert(ctx context.Context, row *model.TokenUsage) error {
	err := l.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "session_id"}, {Name: "turn_seq"}},
		UpdateAll: true,
	}).Create(row).Error
	if err != nil {
		return fmt.Errorf("usage: record turn: %w", err)
	}
	return nil
}

// applyReportedCost 按 agent 自报的累计费用算出本轮成本。
//
// 不报（codex）或币种不是美元时留 CostNone——**零和「不知道」是两件事**，
// 界面要能把它们分开显示，折算交给单价表那一步。
func applyReportedCost(row *model.TokenUsage, cum *acp.UsageCost, prevCum int64) {
	if cum == nil || !strings.EqualFold(cum.Currency, "USD") {
		row.CostSource = model.CostNone
		return
	}
	now := usdToMicro(cum.Amount)
	delta := now - prevCum
	// 负差分意味着 agent 侧把累计值清了（跨 session/load 恢复时会发生）：
	// 从这里起当作新起点，而不是让一个负数把合计拉低。
	if delta < 0 {
		delta = now
	}
	row.CostMicro = delta
	row.CostCumMicro = now
	row.CostSource = model.CostReported
}

// usdToMicro 把美元转成百万分之一美元的整数。账目一律走整数：float64
// 累加几万行之后的合计会带误差尾巴。
func usdToMicro(amount float64) int64 {
	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0
	}
	return int64(math.Round(amount * 1e6))
}

// settingsModel 从会话的设置快照里取模型名。取不到就空着——
// claude 大多数轮次报的本来就是档位名而非模型 id，这里不做任何猜测。
func settingsModel(last model.JSONMap) string {
	if last == nil {
		return ""
	}
	name, _ := last["model"].(string)
	return truncate(name, 64)
}

// normalizeOrigin 把会话来源归一：空串是界面里的人开的，给它一个明确的
// 名字，分组时才不会出现一个没有标签的格子。
func normalizeOrigin(origin string) string {
	if origin == "" {
		return model.OriginUI
	}
	return origin
}

// truncate 按字节上限截断，且不切断多字节字符——错误文案里有中文，
// 半个汉字存进库比截短更难读。
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
