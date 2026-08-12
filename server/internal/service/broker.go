package service

import (
	"log/slog"
	"sync"
)

// subscriberBuffer 是每个订阅者的缓冲深度。慢订阅者会丢事件而不是拖住整条流；
// 丢了也不致命——turn 结束后前端会重新拉取从转录重建的完整消息。
const subscriberBuffer = 512

// broker 负责一条会话的事件广播。
type broker struct {
	mu     sync.Mutex
	subs   map[chan StreamEvent]struct{}
	seq    int
	replay []StreamEvent
}

func newBroker() *broker {
	return &broker{subs: make(map[chan StreamEvent]struct{})}
}

// subscribe 加入一个订阅者，并先把本轮已发生的事件补给它，
// 这样中途刷新页面也能看到正在跑的这一轮。
func (b *broker) subscribe() (<-chan StreamEvent, func()) {
	b.mu.Lock()
	backlog := make([]StreamEvent, len(b.replay))
	copy(backlog, b.replay)
	// 容量必须装得下整个 backlog：长轮的事件数可以超过 subscriberBuffer，
	// 非阻塞补发会把尾部（含 turn_done）静默丢掉，前端 busy 就永久卡死。
	ch := make(chan StreamEvent, max(subscriberBuffer, len(backlog)+subscriberBuffer))
	b.subs[ch] = struct{}{}
	b.mu.Unlock()

	for _, ev := range backlog {
		ch <- ev
	}

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, ch)
			b.mu.Unlock()
			close(ch)
		})
	}
	return ch, cancel
}

func (b *broker) publish(ev StreamEvent) {
	b.mu.Lock()
	b.seq++
	ev.Seq = b.seq
	b.replay = append(b.replay, ev)
	subs := make([]chan StreamEvent, 0, len(b.subs))
	for ch := range b.subs {
		subs = append(subs, ch)
	}
	b.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
			slog.Warn("sse subscriber is too slow, dropping event", "kind", ev.Kind)
		}
	}
}

// startTurn 清空上一轮的回放缓冲。
func (b *broker) startTurn() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.replay = nil
}

func (b *broker) endTurn() {
	b.publish(StreamEvent{Kind: "turn_done"})
	// 轮已收尾，重放缓冲就完成使命了：之后刷新页面走消息接口拿权威转录，
	// 不需要（也不应该）重放一整轮的流式事件；agent 中止后迟到的残余
	// 事件也不该攒着毒化下一次订阅。
	b.mu.Lock()
	b.replay = nil
	b.mu.Unlock()
}
