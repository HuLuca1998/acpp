package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"gorm.io/gorm"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
)

// BackfillResult 是一次重算的战果。
type BackfillResult struct {
	// Sessions 是重建了账目的会话数，Turns 是落下的行数。
	Sessions int `json:"sessions"`
	Turns    int `json:"turns"`
	// Orphans 是有转录但会话记录已经没了的份数。**这些跳过不记**：
	// 账目必须有主人，而归属（谁的、哪个项目）只有会话记录知道。
	Orphans int   `json:"orphans"`
	Failed  int   `json:"failed"`
	Elapsed int64 `json:"elapsedMs"`
}

// Backfill 照转录重算全部历史账目。
//
// 这是「事实源是转录」这句话的兑现：账本任何时候都能从零重建，因此
//   - 上线当天就有全部历史，不用从今天开始攒；
//   - 记账逻辑改了（比如成本口径修正），重跑一次就全部对齐；
//   - 实时落账偶尔漏了一轮（进程被杀），重跑一次补回来。
//
// 实测本机 224 份转录、949 轮，全扫一遍 1.5 秒。
func (l *Ledger) Backfill(ctx context.Context) (BackfillResult, error) {
	started := time.Now()
	var res BackfillResult
	if l.transcripts == nil {
		return res, fmt.Errorf("%w: 没有转录目录可重算", ErrInvalid)
	}

	keys, err := l.transcripts.Keys()
	if err != nil {
		return res, fmt.Errorf("usage: list transcripts: %w", err)
	}

	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		id, err := strconv.ParseUint(key, 10, 64)
		if err != nil {
			// 不是会话转录（备份文件之类），不是错。
			continue
		}
		turns, err := l.BackfillSession(ctx, uint(id))
		switch {
		case err == nil:
			res.Sessions++
			res.Turns += turns
		case errors.Is(err, ErrNotFound):
			res.Orphans++
		default:
			res.Failed++
			slog.Warn("backfill session", "session", id, "err", err)
		}
	}

	res.Elapsed = time.Since(started).Milliseconds()
	return res, nil
}

// BackfillSession 照转录重建一条会话的全部账目。
//
// 整条会话**先删后建**，不是逐行 upsert：转录是权威，重建之后这条会话的
// 账目必须与它逐轮对齐。只 upsert 的话，实时漏记一轮会让后面所有轮的序号
// 错位一格，旧的尾行还会留下来变成幽灵账。
func (l *Ledger) BackfillSession(ctx context.Context, sessionID uint) (int, error) {
	meta, err := l.sessionMeta(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	facts, err := l.scanTranscript(sessionID)
	if err != nil {
		return 0, err
	}

	rows := make([]model.TokenUsage, 0, len(facts))
	table := l.prices.get()
	var prevCum int64
	for i, f := range facts {
		row := buildRow(sessionID, i+1, meta, f, prevCum, table)
		if row.CostSource == model.CostReported {
			prevCum = row.CostCumMicro
		}
		rows = append(rows, row)
	}

	err = l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("session_id = ?", sessionID).Delete(&model.TokenUsage{}).Error; err != nil {
			return fmt.Errorf("clear old rows: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		if err := tx.CreateInBatches(&rows, 200).Error; err != nil {
			return fmt.Errorf("insert rows: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("usage: backfill session %d: %w", sessionID, err)
	}
	return len(rows), nil
}

// wireLine 是转录里的一行，只解账本要的那几样。
type wireLine struct {
	TS  time.Time `json:"ts"`
	Msg struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			ConfigID string          `json:"configId"`
			Value    json.RawMessage `json:"value"`
			Update   struct {
				Kind       string         `json:"sessionUpdate"`
				ToolCallID string         `json:"toolCallId"`
				Status     string         `json:"status"`
				Cost       *acp.UsageCost `json:"cost"`
			} `json:"update"`
		} `json:"params"`
		Result *struct {
			StopReason    string     `json:"stopReason"`
			Usage         *acp.Usage `json:"usage"`
			ConfigOptions []struct {
				ID           string          `json:"id"`
				CurrentValue json.RawMessage `json:"currentValue"`
			} `json:"configOptions"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	} `json:"msg"`
}

// scanTranscript 把一条会话的转录重放成逐轮的事实。
//
// 认的就是实时那条路上的同几个信号：session/prompt 发出是一轮的开始，
// 带 stopReason 的响应是收尾，其间的 usage_update 给费用累计值、
// tool_call 给工具终态、set_config_option 给当时生效的模型。
func (l *Ledger) scanTranscript(sessionID uint) ([]turnFacts, error) {
	var (
		out      []turnFacts
		curModel string
		lastCum  *acp.UsageCost
		// pending 是已发出还没收到响应的 prompt：id → 发出时刻。
		// claude 的排队轮会让两个 prompt 同时在途，所以按 id 对齐而不是
		// 只记一个「最近的」。
		pending = map[string]time.Time{}
		// tools 是**当前轮内**的工具终态。轮外的事件（session/load 重放
		// 历史）不计——那些工具属于早先的轮次，算进这一轮是凭空多出来的。
		tools = map[string]string{}
	)

	err := l.transcripts.ReadLines(strconv.FormatUint(uint64(sessionID), 10), func(line []byte) bool {
		var row wireLine
		// 半行（进程被杀时写了一半）跳过，不让整份转录不可读。
		if json.Unmarshal(line, &row) != nil {
			return true
		}
		msg := row.Msg
		id := string(msg.ID)

		switch msg.Method {
		case "session/prompt":
			pending[id] = row.TS
			return true
		case "session/set_config_option":
			if msg.Params.ConfigID == "model" {
				curModel = jsonString(msg.Params.Value)
			}
			return true
		case "session/update":
			u := msg.Params.Update
			switch u.Kind {
			case "usage_update":
				if u.Cost != nil {
					lastCum = u.Cost
				}
			case "tool_call", "tool_call_update":
				// 只在有轮在途时计数，理由见 tools 的注释。
				if len(pending) > 0 && u.ToolCallID != "" {
					switch u.Status {
					case "completed", "failed", "cancelled":
						tools[u.ToolCallID] = u.Status
					}
				}
			}
			return true
		}

		res := msg.Result
		if res == nil {
			// agent 报错：那一轮也要落账（token 多半已经烧掉了）。
			if msg.Error != nil {
				if start, ok := pending[id]; ok {
					delete(pending, id)
					out = append(out, turnFacts{
						StartedAt: start, EndedAt: row.TS, Model: curModel,
						CostCum:   lastCum,
						ErrorCode: msg.Error.Code,
						ErrorMsg:  truncate(msg.Error.Message, errMsgLimit),
					})
					tools = map[string]string{}
				}
			}
			return true
		}

		// session/new 与 session/load 的响应带全量配置，从里面取当前模型。
		for _, opt := range res.ConfigOptions {
			if opt.ID == "model" {
				curModel = jsonString(opt.CurrentValue)
			}
		}

		if res.StopReason == "" {
			return true
		}
		start, ok := pending[id]
		if !ok {
			// 没配上发出记录的收尾（转录从中间截断过），用收尾时刻兜底，
			// 耗时那一列留空而不是算出一个假的长度。
			start = time.Time{}
		}
		delete(pending, id)

		f := turnFacts{
			StartedAt:  start,
			EndedAt:    row.TS,
			Usage:      res.Usage,
			CostCum:    lastCum,
			StopReason: res.StopReason,
			Model:      curModel,
		}
		for _, status := range tools {
			f.ToolCalls++
			if status == "failed" {
				f.ToolFailed++
			}
		}
		tools = map[string]string{}
		out = append(out, f)
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("usage: read transcript %d: %w", sessionID, err)
	}
	return out, nil
}

// jsonString 把配置项的值解成字符串。它可能是字符串也可能是别的
// （fast 那类是布尔），不是字符串就当没有——模型名只认字符串。
func jsonString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}
