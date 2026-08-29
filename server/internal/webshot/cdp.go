package webshot

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// mini CDP 客户端：请求-响应按 id 配对，事件按 (sessionId, method) 派发。
// 只覆盖截图要用的那点协议面——这是它存在的理由，别往里加通用能力。

type cdpClient struct {
	conn *websocket.Conn

	mu      sync.Mutex
	nextID  int
	pending map[int]chan cdpReply
	waiters map[string]chan struct{} // sessionId+"\x00"+method → 一次性事件等待
	closed  bool
}

type cdpReply struct {
	result map[string]any
	err    error
}

type cdpMessage struct {
	ID        int             `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    map[string]any  `json:"params,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func dialCDP(ctx context.Context, wsURL string) (*cdpClient, error) {
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("连 DevTools: %w", err)
	}
	// 截图的 base64 一帧就是好几 MB，默认读上限太小。
	conn.SetReadLimit(64 << 20)
	c := &cdpClient{conn: conn, pending: map[int]chan cdpReply{}, waiters: map[string]chan struct{}{}}
	go c.readLoop()
	return c, nil
}

func (c *cdpClient) readLoop() {
	for {
		_, data, err := c.conn.Read(context.Background())
		if err != nil {
			c.mu.Lock()
			c.closed = true
			for id, ch := range c.pending {
				ch <- cdpReply{err: fmt.Errorf("连接断开: %w", err)}
				delete(c.pending, id)
			}
			c.mu.Unlock()
			return
		}
		var msg cdpMessage
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg.ID != 0 {
			c.mu.Lock()
			ch := c.pending[msg.ID]
			delete(c.pending, msg.ID)
			c.mu.Unlock()
			if ch == nil {
				continue
			}
			if msg.Error != nil {
				ch <- cdpReply{err: fmt.Errorf("cdp: %s", msg.Error.Message)}
				continue
			}
			var result map[string]any
			_ = json.Unmarshal(msg.Result, &result)
			ch <- cdpReply{result: result}
			continue
		}
		if msg.Method != "" {
			key := msg.SessionID + "\x00" + msg.Method
			c.mu.Lock()
			if ch, ok := c.waiters[key]; ok {
				close(ch)
				delete(c.waiters, key)
			}
			c.mu.Unlock()
		}
	}
}

// call 发一条命令并等结果。sessionID 空串表示浏览器级命令。
func (c *cdpClient) call(ctx context.Context, sessionID, method string, params map[string]any) (map[string]any, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("连接已断开")
	}
	c.nextID++
	id := c.nextID
	ch := make(chan cdpReply, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	msg := cdpMessage{ID: id, Method: method, Params: params, SessionID: sessionID}
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	if err := c.conn.Write(ctx, websocket.MessageText, data); err != nil {
		return nil, fmt.Errorf("发 %s: %w", method, err)
	}
	select {
	case r := <-ch:
		return r.result, r.err
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

// waitEvent 等一条会话级事件，超时不算错误（调用方自己决定要不要继续）。
func (c *cdpClient) waitEvent(ctx context.Context, sessionID, method string, timeout time.Duration) {
	key := sessionID + "\x00" + method
	c.mu.Lock()
	ch, ok := c.waiters[key]
	if !ok {
		ch = make(chan struct{})
		c.waiters[key] = ch
	}
	c.mu.Unlock()
	select {
	case <-ch:
	case <-time.After(timeout):
	case <-ctx.Done():
	}
}

func (c *cdpClient) close() {
	_ = c.conn.Close(websocket.StatusNormalClosure, "")
}
