package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/coder/websocket"

	"acpp/server/internal/service"
)

// terminalHandler 是工作区终端的传输层：REST 管生命周期，ws 桥 pty 双向流。
// ws 协议：二进制帧 = 终端原始字节（双向）；客户端文本帧 = JSON 控制消息
// （目前只有 resize）。
type terminalHandler struct {
	cwdOf cwdResolver
	terms *service.TerminalService
	// originPatterns 供 ws 升级校验跨域（开发态 vite 端口与后端不同源）。
	originPatterns []string
	// keyOffset 隔离终端配额与列表的会话 key 空间：编排会话与普通会话的
	// 数字 id 会重叠，TerminalService 以 uint 分组，偏移错开。
	keyOffset uint
}

// orchTerminalKeyOffset 把编排会话的终端分组 key 错开普通会话的 id 空间。
const orchTerminalKeyOffset = 1 << 30

type terminalResize struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

func (h terminalHandler) create(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	cwd, err := h.cwdOf(r)
	if err != nil {
		writeError(w, err)
		return
	}
	info, err := h.terms.Create(h.keyOffset+id, cwd)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusCreated, info)
}

func (h terminalHandler) list(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, h.terms.List(h.keyOffset+id))
}

func (h terminalHandler) remove(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	if err := h.terms.Close(h.keyOffset+id, r.PathValue("tid")); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, nil)
}

// attach 升级到 websocket 并桥接 pty：输出经 Attach 的 sink 推送（含
// 重连回放），输入/resize 在读循环里处理；pty 退出时正常关闭连接。
func (h terminalHandler) attach(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	proxy, err := h.terms.Get(h.keyOffset+id, r.PathValue("tid"))
	if err != nil {
		writeError(w, err)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: h.originPatterns,
	})
	if err != nil {
		slog.Warn("terminal ws accept", "err", err)
		return
	}

	// 终端输出洪峰远超默认 32KB 读限制的对称场景；写侧不受限，
	// 读侧只有键盘输入与 resize，小限额足够。
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	replay, done, detach := proxy.Attach(func(data []byte) error {
		return conn.Write(ctx, websocket.MessageBinary, data)
	})
	defer detach()

	if len(replay) > 0 {
		if err := conn.Write(ctx, websocket.MessageBinary, replay); err != nil {
			return
		}
	}

	// pty 退出 → 正常关闭 ws，前端据此画「已结束」态。
	go func() {
		select {
		case <-done:
			conn.Close(websocket.StatusNormalClosure, "exit")
			cancel()
		case <-ctx.Done():
		}
	}()

	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		switch typ {
		case websocket.MessageBinary:
			if err := proxy.Write(data); err != nil {
				return
			}
		case websocket.MessageText:
			var msg terminalResize
			if json.Unmarshal(data, &msg) == nil && msg.Type == "resize" {
				_ = proxy.Resize(msg.Cols, msg.Rows)
			}
		}
	}
}
