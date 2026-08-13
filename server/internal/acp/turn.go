package acp

import (
	"context"
	"errors"
	"strings"
)

// Prompt 发一轮对话，阻塞到整轮结束。turn 期间的内容通过 OnEvent 实时推出去。
//
// ACP 会话自带上下文，所以第二轮起只需要发用户这一句，不要重复系统提示。
func (m *Manager) Prompt(ctx context.Context, key string, blocks []ContentBlock) (PromptResult, error) {
	sess, ok := m.Get(key)
	if !ok {
		return PromptResult{}, ErrNoSession
	}

	// 一条会话同一时刻只允许一个 turn。
	if !sess.turnMu.TryLock() {
		return PromptResult{}, ErrBusy
	}
	defer sess.turnMu.Unlock()

	turnCtx, cancel := m.turnContext(ctx)
	defer cancel()

	sess.mu.Lock()
	sess.cancelTurn = cancel
	sess.mu.Unlock()

	defer func() {
		sess.mu.Lock()
		sess.cancelTurn = nil
		sess.mu.Unlock()
	}()

	result, err := sess.promptCall(turnCtx, blocks)
	if err != nil {
		// 用户主动中止（session/cancel 取消了 turnCtx）不是故障。codex 对
		// cancel 的反应是让 prompt 报 "context canceled" 错而非按 ACP 规范
		// 返回 stopReason=cancelled，这里折算回 cancelled 正常收尾。
		if errors.Is(turnCtx.Err(), context.Canceled) {
			sess.emit(Event{Kind: EventTurnEnd, StopReason: StopCancelled})
			return PromptResult{StopReason: StopCancelled}, nil
		}
		// 超时后只 reject 是不够的：agent 还在后台跑、还在烧钱、还可能继续改文件。
		if errors.Is(turnCtx.Err(), context.DeadlineExceeded) {
			m.cancelTurn(sess)
		}
		sess.emit(Event{Kind: EventError, Error: err.Error()})
		return PromptResult{}, err
	}

	sess.emit(Event{Kind: EventTurnEnd, StopReason: result.StopReason, Usage: result.Usage})
	return result, nil
}

// Interject 在 turn 进行中插入一条用户消息，翻译交给 adapter。
// followUp=true 表示这条消息产生了自己独立的一轮（结果已含在返回值里，
// 并已发出 turn_end 事件）；false 表示内容并入当前轮、由当前轮统一收尾。
func (m *Manager) Interject(ctx context.Context, key string, blocks []ContentBlock) (PromptResult, bool, error) {
	sess, ok := m.Get(key)
	if !ok {
		return PromptResult{}, false, ErrNoSession
	}

	ctx, cancel := m.turnContext(ctx)
	defer cancel()

	result, followUp, err := sess.adapter.Interject(ctx, sess, blocks)
	if err != nil {
		// 与 Prompt 的归一同理：用户中止会让在途的 steering/排队 prompt 报错，
		// 而 codex 只给一个 "...canceled" 错误字符串、不按 ACP 规范返回
		// stopReason=cancelled。字符串嗅探是对该协议缺陷的补偿，集中在协议层
		// 这一处；上层只认 StopReason，不碰错误文本。
		if errors.Is(ctx.Err(), context.Canceled) || isCancelledErr(err) {
			sess.emit(Event{Kind: EventTurnEnd, StopReason: StopCancelled})
			return PromptResult{StopReason: StopCancelled}, true, nil
		}
		return PromptResult{}, false, err
	}
	if followUp {
		sess.emit(Event{Kind: EventTurnEnd, StopReason: result.StopReason, Usage: result.Usage})
	}
	return result, followUp, nil
}

// isCancelledErr 识别「远端把取消表达成错误」的两种形态：包装过的
// context.Canceled，或 JSON-RPC 错误文本里的 cancel 字样。
func isCancelledErr(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.Canceled) ||
		strings.Contains(strings.ToLower(err.Error()), "cancel")
}

// Cancel 中止会话上正在跑的 turn。
func (m *Manager) Cancel(key string) error {
	sess, ok := m.Get(key)
	if !ok {
		return ErrNoSession
	}
	m.cancelTurn(sess)
	return nil
}

func (m *Manager) cancelTurn(sess *Session) {
	_ = sess.conn.Notify("session/cancel", CancelParams{SessionID: sess.acpSessionID})
	sess.mu.Lock()
	cancel := sess.cancelTurn
	sess.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// TurnActive 报告会话是否有在途的 prompt/steering 调用（一轮正在跑）。
func (m *Manager) TurnActive(key string) bool {
	sess, ok := m.Get(key)
	if !ok {
		return false
	}
	return sess.activeCalls.Load() > 0
}

// Commands 返回会话的斜杠命令清单快照。
func (m *Manager) Commands(key string) []Command {
	sess, ok := m.Get(key)
	if !ok {
		return nil
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return append([]Command(nil), sess.commands...)
}

// Settings 返回会话设置的统一视图。
func (m *Manager) Settings(key string) (Settings, error) {
	sess, ok := m.Get(key)
	if !ok {
		return Settings{}, ErrNoSession
	}
	return sess.adapter.Settings(sess.Caps()), nil
}

// Apply 逐项应用设置变更，翻译工作由会话的 adapter 完成；
// 任何一项失败立刻返回，已生效的项不回滚（与用户逐个点选的语义一致）。
func (m *Manager) Apply(ctx context.Context, key string, patch SettingsPatch) (Settings, error) {
	sess, ok := m.Get(key)
	if !ok {
		return Settings{}, ErrNoSession
	}
	ad := sess.adapter

	if patch.Model != nil {
		if err := ad.SetModel(ctx, sess, *patch.Model); err != nil {
			return Settings{}, err
		}
	}
	if patch.Effort != nil {
		if err := ad.SetEffort(ctx, sess, *patch.Effort); err != nil {
			return Settings{}, err
		}
	}
	if patch.Fast != nil {
		if err := ad.SetFast(ctx, sess, *patch.Fast); err != nil {
			return Settings{}, err
		}
	}
	// plan 必须先于 level：claude 退出 plan 会回落到默认档，level 放最后
	// 才能保证 {plan:false, level:x} 这类组合以 x 收尾。
	if patch.Plan != nil {
		if err := ad.SetPlan(ctx, sess, *patch.Plan); err != nil {
			return Settings{}, err
		}
	}
	if patch.Level != nil {
		if err := ad.SetAccessLevel(ctx, sess, *patch.Level); err != nil {
			return Settings{}, err
		}
	}
	return ad.Settings(sess.Caps()), nil
}
