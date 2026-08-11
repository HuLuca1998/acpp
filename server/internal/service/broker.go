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
	ch := make(chan StreamEvent, subscriberBuffer)

	b.mu.Lock()
	backlog := make([]StreamEvent, len(b.replay))
	copy(backlog, b.replay)
	b.subs[ch] = struct{}{}
	b.mu.Unlock()

	for _, ev := range backlog {
		select {
		case ch <- ev:
		default:
		}
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
}
