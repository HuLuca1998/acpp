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
	"acpp/server/internal/transcript"
)

// 可预期失败。httpapi 的 writeError 里映射状态码（与 service 的同义）。
var (
	ErrInvalid  = errors.New("usage: invalid")
	ErrNotFound = errors.New("usage: not found")
)

// Ledger 是账本本体：写入、回填与聚合都挂在它上面。
type Ledger struct {
	db *gorm.DB
	// transcripts 是回填的事实源。可为 nil，那时只有实时写入可用。
	transcripts *transcript.Store
	// prices 是折算用的单价表（没有实报费用的那些轮）。默认是空表，
	// 那时它们如实落「未计价」。
	prices prices
}

func NewLedger(db *gorm.DB, transcripts *transcript.Store) *Ledger {
	return &Ledger{db: db, transcripts: transcripts}
}

// turnFacts 是一轮的全部事实，实时落账与历史回填共用的中间形状。
//
// 两条路最终要写出**一模一样的行**，否则「重算历史」就会把实时记的账改得
// 面目全非。让它们都先归到这一个形状，是这份一致性唯一的保证点。
type turnFacts struct {
	StartedAt  time.Time
	EndedAt    time.Time
	Usage      *acp.Usage
	CostCum    *acp.UsageCost
	StopReason string
	// Model 空表示这一路不知道模型，退回会话的设置快照。回填能从转录里
	// 读出每一轮**当时**生效的模型，比快照准。
	Model      string
	ErrorCode  int
	ErrorMsg   string
	ToolCalls  int
	ToolFailed int
}

// sessionMeta 是一条会话的归属信息，落账时定格进每一行。
type sessionMeta struct {
	TenantID uint
	AgentID  uint
	Flavor   string
	Model    string
	Project  string
	Cwd      string
	Origin   string
}

// sessionMeta 查出会话的归属。会话不存在时是 ErrNotFound——**账目必须有
// 主人**：租户隔离靠 tenant_id 执行，凭空填一个都是错的。
func (l *Ledger) sessionMeta(ctx context.Context, sessionID uint) (sessionMeta, error) {
	// 用 Find 而不是 First：查不到是**预期内**的（回填会扫到会话早就删了
	// 的转录），First 会把每一次都当成错误打进日志，重算一遍刷十几行。
	var sess model.Session
	if err := l.db.WithContext(ctx).Limit(1).Find(&sess, sessionID).Error; err != nil {
		return sessionMeta{}, fmt.Errorf("usage: load session: %w", err)
	}
	if sess.ID == 0 {
		return sessionMeta{}, fmt.Errorf("%w: session %d", ErrNotFound, sessionID)
	}

	meta := sessionMeta{
		TenantID: sess.TenantID,
		AgentID:  sess.AgentID,
		Model:    settingsModel(sess.LastSettings),
		Project:  gitrepo.ProjectOf(sess.Cwd),
		Cwd:      sess.Cwd,
		Origin:   normalizeOrigin(sess.Origin),
	}
	if sess.AgentID != 0 {
		var agent model.Agent
		if l.db.WithContext(ctx).Select("flavor").Limit(1).Find(&agent, sess.AgentID).Error == nil {
			meta.Flavor = agent.Flavor
		}
	}
	return meta, nil
}

// buildRow 把「归属 + 一轮的事实」组装成一行账目。
// prevCum 是上一行留下的费用累计基准（没有上一行就传 0）。
func buildRow(sessionID uint, seq int, meta sessionMeta, f turnFacts, prevCum int64, table PriceTable) model.TokenUsage {
	row := model.TokenUsage{
		SessionID:  sessionID,
		TurnSeq:    seq,
		TenantID:   meta.TenantID,
		AgentID:    meta.AgentID,
		Flavor:     meta.Flavor,
		Model:      meta.Model,
		Project:    meta.Project,
		Cwd:        meta.Cwd,
		Origin:     meta.Origin,
		StartedAt:  f.StartedAt,
		EndedAt:    f.EndedAt,
		StopReason: f.StopReason,
		ErrorCode:  f.ErrorCode,
		ErrorMsg:   f.ErrorMsg,
		ToolCalls:  f.ToolCalls,
		ToolFailed: f.ToolFailed,
	}
	if f.Model != "" {
		row.Model = truncate(f.Model, 64)
	}
	if !f.EndedAt.IsZero() && !f.StartedAt.IsZero() {
		row.DurationMs = f.EndedAt.Sub(f.StartedAt).Milliseconds()
	}
	if u := f.Usage; u != nil {
		row.InputTokens = u.InputTokens
		row.OutputTokens = u.OutputTokens
		row.CacheReadTokens = u.CachedReadTokens
		row.CacheWriteTokens = u.CachedWriteTokens
		row.ThoughtTokens = u.ThoughtTokens
		row.TotalTokens = u.TotalTokens
	}
	applyReportedCost(&row, f.CostCum, prevCum)
	// 没有实报费用的按单价表折算；表里也没有就如实落「未计价」。
	applyEstimatedCost(&row, table)
	return row
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
	// Model 是这一轮生效的模型。空表示调用方不知道，退回会话的设置快照
	// ——网页会话的快照是轮末写的，而外部子系统（Discord）压根不写它，
	// 只能由那一侧把绑定的模型直接给过来。
	Model string
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

	meta, err := l.sessionMeta(ctx, rec.SessionID)
	if err != nil {
		return err
	}

	// 上一行给两样东西：轮序号的起点，与成本差分的基准。
	// 同样用 Find：每条会话的第一轮都查不到上一行，那不是错。
	var prev model.TokenUsage
	if err := l.db.WithContext(ctx).Where("session_id = ?", rec.SessionID).
		Order("turn_seq desc").Limit(1).Find(&prev).Error; err != nil {
		return fmt.Errorf("usage: load previous turn: %w", err)
	}
	seq := prev.TurnSeq + 1

	facts := turnFacts{
		StartedAt:  rec.StartedAt,
		EndedAt:    rec.EndedAt,
		Usage:      rec.Usage,
		CostCum:    rec.CostCum,
		StopReason: rec.StopReason,
		ToolCalls:  rec.ToolCalls,
		ToolFailed: rec.ToolFailed,
		Model:      rec.Model,
	}
	if rec.Err != nil {
		facts.ErrorCode = acp.ErrorCode(rec.Err)
		facts.ErrorMsg = truncate(rec.Err.Error(), errMsgLimit)
	}

	row := buildRow(rec.SessionID, seq, meta, facts, prev.CostCumMicro, l.prices.get())
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
