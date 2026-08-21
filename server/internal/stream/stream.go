// Package stream 是会话事件广播的共享构件：Event 是推给浏览器的 SSE
// 事件形状，Broker 负责一条流的多订阅者广播与轮内重放。本包不含任何
// 业务规则。
package stream

import (
	"encoding/json"
	"log/slog"
	"sync"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
)

// Event 是推给浏览器的一条 SSE 事件。
type Event struct {
	Kind string `json:"kind"`
	// Seq 在同一条流内单调递增，供前端去重与断线续传。
	Seq int `json:"seq"`

	Text       string          `json:"text,omitempty"`
	ToolCallID string          `json:"toolCallId,omitempty"`
	Title      string          `json:"title,omitempty"`
	ToolKind   string          `json:"toolKind,omitempty"`
	Status     string          `json:"status,omitempty"`
	RawInput   json.RawMessage `json:"rawInput,omitempty"`
	RawOutput  json.RawMessage `json:"rawOutput,omitempty"`
	Content    json.RawMessage `json:"content,omitempty"`
	Locations  json.RawMessage `json:"locations,omitempty"`
	// 子代理归属，供界面把子代理的活从主对话流里摘出来单独成列表。
	// IsSubagent 标记「这次调用启动了一个子代理」，SubagentOf 标记
	// 「这条是某个子代理干的」；codex 的转录要凭 SubagentThreadID 另行拉取。
	IsSubagent       bool   `json:"isSubagent,omitempty"`
	SubagentOf       string `json:"subagentOf,omitempty"`
	SubagentThreadID string `json:"subagentThreadId,omitempty"`
	SubagentPath     string `json:"subagentPath,omitempty"`
	// Entries 是 plan 事件的任务条目数组。
	Entries       json.RawMessage `json:"entries,omitempty"`
	Settings      *acp.Settings   `json:"settings,omitempty"`
	Used          int64           `json:"used,omitempty"`
	Size          int64           `json:"size,omitempty"`
	Cost          *acp.UsageCost  `json:"cost,omitempty"`
	Commands      []acp.Command   `json:"commands,omitempty"`
	Usage         *acp.Usage      `json:"usage,omitempty"`
	ElicitationID string          `json:"elicitationId,omitempty"`
	// 权限请求：ID 用于回传裁决，Options 是 agent 给的选项。
	// PlanReview 非空时这是「计划完成」审批，前端渲染专门卡片。
	PermissionID string                 `json:"permissionId,omitempty"`
	Options      []acp.PermissionOption `json:"options,omitempty"`
	PlanReview   *acp.PlanReview        `json:"planReview,omitempty"`
	StopReason   string                 `json:"stopReason,omitempty"`
	Error        string                 `json:"error,omitempty"`

	// Message 在一条消息落库后带上完整记录，前端用它替换流式占位。
	Message *model.Message `json:"message,omitempty"`
}

// subscriberBuffer 是每个订阅者的缓冲深度。慢订阅者会丢事件而不是拖住
// 整条流；丢了也不致命——turn 结束后前端会重新拉取从转录重建的完整消息。
const subscriberBuffer = 512

// Broker 负责一条流的事件广播。
type Broker struct {
	mu     sync.Mutex
	subs   map[chan Event]struct{}
	seq    int
	replay []Event
	closed bool
}

func NewBroker() *Broker {
	return &Broker{subs: make(map[chan Event]struct{})}
}

// Subscribe 加入一个订阅者，并先把本轮已发生的事件补给它，
// 这样中途刷新页面也能看到正在跑的这一轮。
func (b *Broker) Subscribe() (<-chan Event, func()) {
	b.mu.Lock()
	// 容量必须装得下整个 backlog：长轮的事件数可以超过 subscriberBuffer，
	// 非阻塞补发会把尾部（含 turn_done）静默丢掉，前端 busy 就永久卡死。
	ch := make(chan Event, max(subscriberBuffer, len(b.replay)+subscriberBuffer))
	if b.closed {
		// 服务正在关停：给一个已关闭的 channel，订阅方立刻收到流结束。
		b.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	// 补发必须在临界区内完成：一旦注册进 subs，并发 Publish 就会向 ch 直投，
	// 锁外补发会让新事件抢在 backlog 前面，订阅者看到 Seq 乱序、chunk 错拼。
	// 容量已保证装得下全部 backlog，持锁 send 不会阻塞。
	for _, ev := range b.replay {
		ch <- ev
	}
	b.subs[ch] = struct{}{}
	b.mu.Unlock()

	// close 以「还在 subs 里」为前提：Close 关掉全部订阅后，晚到的 cancel
	// 查不到成员就什么都不做——两边都可能先动手，谁先谁关，不能各关一次。
	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
	}
	return ch, cancel
}

// Close 关掉全部订阅并拒绝新订阅，服务关停时调用（理由见 Hub.Close）。
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for ch := range b.subs {
		delete(b.subs, ch)
		close(ch)
	}
}

func (b *Broker) Publish(ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.seq++
	ev.Seq = b.seq
	b.replay = append(b.replay, ev)
	// 投递也留在临界区内：并发 Publish 各自锁外投递会彼此交错，订阅者
	// 收到的顺序就不再等于 Seq 顺序。channel 有缓冲、满了走 default 丢弃，
	// 持锁 send 不会阻塞。
	for ch := range b.subs {
		select {
		case ch <- ev:
		default:
			slog.Warn("sse subscriber is too slow, dropping event", "kind", ev.Kind)
		}
	}
}

// StartTurn 清空上一轮的回放缓冲。
func (b *Broker) StartTurn() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.replay = nil
}

func (b *Broker) EndTurn() {
	b.Publish(Event{Kind: "turn_done"})
	// 轮已收尾，重放缓冲就完成使命了：之后刷新页面走消息接口拿权威转录，
	// 不需要（也不应该）重放一整轮的流式事件；agent 中止后迟到的残余
	// 事件也不该攒着毒化下一次订阅。
	b.mu.Lock()
	b.replay = nil
	b.mu.Unlock()
}
