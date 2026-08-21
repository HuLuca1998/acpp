package stream

import (
	"sync"

	"acpp/server/internal/acp"
)

// Notice 是一条与会话流无关的全局通知：某条会话上发生了值得打扰用户的事。
//
// 它刻意**不判断「要不要弹」**——那要知道用户此刻正在看哪一页、页面在不在
// 前台、他是不是就坐在这台机器前，只有客户端自己清楚。后端只负责把事情如实
// 广播给有资格看见它的人，打不打扰由收到的人自己决定。
type Notice struct {
	// Kind 固定是 "notify"，与全局流里的 hello、心跳区分。
	Kind string `json:"kind"`
	// Event 是这条通知的由来：permission / elicitation / turn_end / error，
	// 外加两条撤回信号 permission_done / elicitation_done——事情在页面上处理
	// 掉了，挂在通知中心里的那条就得收回去，否则它还在替一个已经结束的
	// 请求要决定。
	Event string `json:"event"`

	SessionID    uint   `json:"sessionId"`
	SessionTitle string `json:"sessionTitle,omitempty"`
	// Text 是给人看的一句话摘要（错误原因、agent 想干什么），可为空。
	Text string `json:"text,omitempty"`

	// PermissionID 与 Options 是决策专用：系统通知上的按钮就是这些选项，
	// 按一下即裁决，用户不必先把窗口翻出来。
	PermissionID string                 `json:"permissionId,omitempty"`
	Options      []acp.PermissionOption `json:"options,omitempty"`
	// ElicitationID 供撤回用：问题已经在页面上答完了，通知不该还挂着。
	ElicitationID string `json:"elicitationId,omitempty"`

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
	mu     sync.Mutex
	subs   map[chan Notice]uint
	closed bool
}

func NewHub() *Hub { return &Hub{subs: make(map[chan Notice]uint)} }

// Subscribe 以某个身份加入广播，tenantID 为 0 表示 owner。
// 返回的 cancel 必须被调用以释放订阅。
func (h *Hub) Subscribe(tenantID uint) (<-chan Notice, func()) {
	ch := make(chan Notice, noticeBuffer)
	h.mu.Lock()
	if h.closed {
		// 服务正在关停：给一个已关闭的 channel，订阅方立刻收到流结束。
		h.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
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
	if h.closed {
		return
	}
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

// Close 关掉全部订阅并拒绝新订阅，服务关停时调用。
//
// 为什么需要它：SSE 是不会自己收尾的在途请求，http.Server 的优雅关闭对它
// 只能干等超时——每次重启白白拖上十秒，浏览器那头还以为连接好好的。关停时
// 把订阅统一关闭，事件流 handler 立刻返回、连接立刻断，客户端马上进入
// 重连，新进程一起来就能拿到新版本的 hello。
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for ch := range h.subs {
		delete(h.subs, ch)
		close(ch)
	}
}
