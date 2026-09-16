package fswatch

import (
	"errors"
	"log/slog"
	"sync"
)

// Hub 按目录共用监视器：同一个工作目录被几个页面同时看着（两个浏览器
// 标签、桌面壳加一个局域网访客）时只监视一遍，最后一个订阅者离开即收摊。
//
// 不做这层的话，每开一个面板就是一整棵树的句柄，而它们要的是同一件事。
type Hub struct {
	mu    sync.Mutex
	roots map[string]*entry
}

// entry 是一个目录上的监视器与它的订阅者们。字段全部在 Hub.mu 下读写，
// fan-out 也在锁内完成——发送走容量 1 的非阻塞写，慢订阅者拖不住锁。
// 引用计数就是 len(subs)，不另记一份。
type entry struct {
	stop func()
	subs map[chan struct{}]struct{}
}

func NewHub() *Hub {
	return &Hub{roots: make(map[string]*entry)}
}

// ErrUnavailable 表示这个目录监视不了（没有装配 Hub，或平台拒绝）。
// 不是错误路径：调用方据此降级回手动刷新。
var ErrUnavailable = errors.New("fswatch: watching unavailable")

// Subscribe 订阅 root 下的文件变动。返回的 channel 每次合帧窗口结束收到
// 一个信号；cancel 退订，最后一个退订时监视器一并关掉。
func (h *Hub) Subscribe(root string) (<-chan struct{}, func(), error) {
	if h == nil {
		return nil, nil, ErrUnavailable
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	e := h.roots[root]
	if e == nil {
		events, stop, err := Watch(root)
		if err != nil {
			return nil, nil, err
		}
		e = &entry{stop: stop, subs: make(map[chan struct{}]struct{})}
		h.roots[root] = e
		go h.fanout(root, e, events)
	}

	ch := make(chan struct{}, 1)
	e.subs[ch] = struct{}{}

	var once sync.Once
	cancel := func() {
		once.Do(func() { h.unsubscribe(root, ch) })
	}
	return ch, cancel, nil
}

// fanout 把一个监视器的信号分给它的订阅者们。
func (h *Hub) fanout(root string, e *entry, events <-chan struct{}) {
	for range events {
		h.mu.Lock()
		for ch := range e.subs {
			select {
			case ch <- struct{}{}:
			default:
				// 上一声还没取走，合并——「变了」不累加。
			}
		}
		h.mu.Unlock()
	}
	// 监视器自己结束了（进程句柄用尽、目录被删）：把这一项摘掉，下一个
	// 订阅者来时重建一个，而不是挂在一个再也不会发信号的 entry 上。
	h.mu.Lock()
	if h.roots[root] == e {
		delete(h.roots, root)
	}
	h.mu.Unlock()
	slog.Debug("fswatch stopped", "root", root)
}

func (h *Hub) unsubscribe(root string, ch chan struct{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.roots[root]
	if e == nil {
		return
	}
	delete(e.subs, ch)
	close(ch)
	if len(e.subs) > 0 {
		return
	}
	// 没人看了就别再监视：句柄还给系统，下次有人来再建。
	delete(h.roots, root)
	e.stop()
}
