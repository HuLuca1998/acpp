package stream

import "sync"

// Notice 是一条与会话流无关的全局通知：某条会话上发生了值得打扰用户的事。
//
// 它刻意**不判断「要不要弹」**——那要知道用户此刻正在看哪一页、页面在不在
// 前台、他是不是就坐在这台机器前，只有客户端自己清楚。后端只负责把事情如实
// 广播给有资格看见它的人，打不打扰由收到的人自己决定。
type Notice struct {
	// Kind 固定是 "notify"，与全局流里的 hello、心跳区分。
	Kind string `json:"kind"`
	// Event 是这条通知的由来：permission / elicitation / turn_end / error。
	Event string `json:"event"`

	SessionID    uint   `json:"sessionId"`
	SessionTitle string `json:"sessionTitle,omitempty"`
	// Text 是给人看的一句话摘要（错误原因、agent 想干什么），可为空。
	Text string `json:"text,omitempty"`

	// TenantID 是会话归属，投递前的过滤依据。**不进 JSON**——它是路由信息，
	// 不该出现在推给浏览器的载荷里。
	TenantID uint `json:"-"`
}

// noticeBuffer 是每个订阅者的缓冲深度。通知是低频事件，几条就够；
// 满了就丢，迟到的打扰没有补发的价值。
const noticeBuffer = 16

// Hub 是全局通知的广播器：不分会话、不重放、按身份投递。
//
// 与 Broker 的分工：Broker 是一条会话流的内容管道，要重放、要顺序、丢一条
// 就是残缺的消息；Hub 只送「发生了一件值得看一眼的事」，错过就错过。
type Hub struct {
	mu   sync.Mutex
	subs map[chan Notice]uint
}

func NewHub() *Hub { return &Hub{subs: make(map[chan Notice]uint)} }

// Subscribe 以某个身份加入广播，tenantID 为 0 表示 owner。
// 返回的 cancel 必须被调用以释放订阅。
func (h *Hub) Subscribe(tenantID uint) (<-chan Notice, func()) {
	ch := make(chan Notice, noticeBuffer)
	h.mu.Lock()
	h.subs[ch] = tenantID
	h.mu.Unlock()

	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
	}
}

// Publish 把通知送给归属相符的订阅者。
//
// 归属是**精确匹配**，不是「owner 收全部」：owner 确实看得见租户的所有会话
// （adr-007），但看得见不等于该被打扰——租户会话上的决策卡片长在租户自己
// 那一页，owner 收到也按不了，只会被别人的活动刷屏。
func (h *Hub) Publish(n Notice) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch, tenantID := range h.subs {
		if tenantID != n.TenantID {
			continue
		}
		select {
		case ch <- n:
		default:
			// 订阅者跟不上就丢掉这条，不阻塞广播。
		}
	}
}
