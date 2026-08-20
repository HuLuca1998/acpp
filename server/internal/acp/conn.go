package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// maxLineBytes 是单行 ndjson 的上限。工具调用的 rawOutput 可以很大，
// bufio 默认的 64KB 会直接把连接读断。
const maxLineBytes = 16 << 20

// stderrKeepBytes 是保留的 stderr 尾部字节数。agent 崩溃、认证失败、版本错配的
// 信息只在 stderr 里，ACP 消息流里什么都没有。
const stderrKeepBytes = 8 << 10

// 关闭子进程的两段宽限：先关 stdin 等自然退出，再 SIGINT，最后 KILL。
const (
	closeGraceStdin  = 3 * time.Second
	closeGraceSignal = 2 * time.Second
)

// 反向调用的应答时限：普通请求一分钟；交互式提问要等真人作答，
// 给到和一轮 prompt 同级别的时间。
const (
	reverseCallTimeout     = time.Minute
	elicitationCallTimeout = 10 * time.Minute
)

// Handler 处理 agent 反向调用。fs 方法只有在 initialize 里声明了对应
// capability 时才会被调到。
type Handler interface {
	// RequestPermission 是唯一一条跨 runtime 统一的逐工具执法通道，agent 阻塞等返回。
	RequestPermission(ctx context.Context, p RequestPermissionParams) (RequestPermissionResult, error)
	// Elicitation 处理 agent 的交互式提问，阻塞到用户作答或超时。
	Elicitation(ctx context.Context, p ElicitationParams) (ElicitationResult, error)
	ReadTextFile(ctx context.Context, p ReadTextFileParams) (ReadTextFileResult, error)
	WriteTextFile(ctx context.Context, p WriteTextFileParams) error
	// OnUpdate 收到 session/update 通知。实现必须立刻返回，不要在里面做阻塞的活。
	OnUpdate(n SessionNotification)
}

// WireTap 观察线级消息：dir 为 "send"（我们→agent）或 "recv"（agent→我们）。
// 实现必须快速返回；msg 归调用方所有，可以直接持有。
type WireTap func(dir string, msg json.RawMessage)

// Conn 是一条 ACP 连接：一个 agent 子进程 + 其上的 JSON-RPC 往返。
type Conn struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	handler Handler
	tap     WireTap

	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan rpcMessage
	closed  bool

	stderr  *ringBuffer
	done    chan struct{}
	exitErr error
}

// Spawn 启动 agent 子进程并开始读取它的 stdout。
// tap 可为 nil；非 nil 时每条线级消息（收与发）都会经过它。
func Spawn(ctx context.Context, rt Runtime, handler Handler, tap WireTap) (*Conn, error) {
	cmd := exec.Command(rt.Command, rt.Args...)
	cmd.Env = rt.buildEnv(os.Environ())

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", rt.Command, err)
	}

	c := &Conn{
		cmd:     cmd,
		stdin:   stdin,
		handler: handler,
		tap:     tap,
		pending: make(map[int64]chan rpcMessage),
		stderr:  newRingBuffer(stderrKeepBytes),
		done:    make(chan struct{}),
	}

	go c.drainStderr(stderr)
	go c.readLoop(stdout)

	return c, nil
}

// Stderr 返回收集到的 stderr 尾部，用于把 agent 的真实死因带进错误信息。
func (c *Conn) Stderr() string { return strings.TrimSpace(c.stderr.String()) }

// Done 在子进程退出后关闭。
func (c *Conn) Done() <-chan struct{} { return c.done }

// Call 发一个请求并等待响应。
func (c *Conn) Call(ctx context.Context, method string, params, result any) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("acp: connection closed")
	}
	id := c.nextID
	c.nextID++
	ch := make(chan rpcMessage, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	raw, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal %s params: %w", method, err)
	}
	idRaw, _ := json.Marshal(id)

	if err := c.write(rpcMessage{JSONRPC: "2.0", ID: idRaw, Method: method, Params: raw}); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", method, ctx.Err())
	case <-c.done:
		return c.processExitError(method)
	case msg := <-ch:
		if msg.Error != nil {
			// %w 保留 rpcError 链条：错误码是协议契约（如 -32000 未登录），
			// 上层用 errors.Is 识别，不嗅探 message 字符串。
			return fmt.Errorf("%s: agent error %d: %w", method, msg.Error.Code, msg.Error)
		}
		if result == nil || len(msg.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(msg.Result, result); err != nil {
			return fmt.Errorf("decode %s result: %w", method, err)
		}
		return nil
	}
}

// Notify 发一个不需要响应的通知。
func (c *Conn) Notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal %s params: %w", method, err)
	}
	return c.write(rpcMessage{JSONRPC: "2.0", Method: method, Params: raw})
}

// Close 关闭 stdin 让 agent 自行退出，超时后 SIGKILL。
func (c *Conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	_ = c.stdin.Close()

	select {
	case <-c.done:
		return nil
	case <-time.After(closeGraceStdin):
	}

	if c.cmd.Process != nil {
		_ = c.cmd.Process.Signal(os.Interrupt)
	}
	select {
	case <-c.done:
		return nil
	case <-time.After(closeGraceSignal):
	}

	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	<-c.done
	return nil
}

func (c *Conn) write(msg rpcMessage) error {
	msg.JSONRPC = "2.0"
	line, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal rpc message: %w", err)
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.stdin.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write to agent: %w", err)
	}
	if c.tap != nil {
		c.tap("send", line)
	}
	return nil
}

func (c *Conn) readLoop(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64<<10), maxLineBytes)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			slog.Warn("acp: bad ndjson line", "err", err, "line", truncate(string(line), 256))
			continue
		}
		if c.tap != nil {
			// scanner 会复用底层数组，必须拷贝后再交出去。
			c.tap("recv", append([]byte(nil), line...))
		}
		c.dispatch(msg)
	}
	if err := scanner.Err(); err != nil {
		slog.Debug("acp: stdout closed", "err", err)
	}

	c.exitErr = c.cmd.Wait()

	c.mu.Lock()
	c.closed = true
	pending := c.pending
	c.pending = make(map[int64]chan rpcMessage)
	c.mu.Unlock()

	// 让所有在等的 Call 立即失败，而不是挂到 context 超时。
	for _, ch := range pending {
		close(ch)
	}
	close(c.done)
}

func (c *Conn) dispatch(msg rpcMessage) {
	switch {
	case msg.Method != "" && len(msg.ID) > 0:
		go c.handleRequest(msg)
	case msg.Method != "":
		c.handleNotification(msg)
	case len(msg.ID) > 0:
		var id int64
		if err := json.Unmarshal(msg.ID, &id); err != nil {
			slog.Warn("acp: response with non-numeric id", "id", string(msg.ID))
			return
		}
		c.mu.Lock()
		ch := c.pending[id]
		c.mu.Unlock()
		if ch != nil {
			ch <- msg
		}
	}
}

func (c *Conn) handleNotification(msg rpcMessage) {
	if msg.Method != "session/update" {
		slog.Debug("acp: unhandled notification", "method", msg.Method)
		return
	}
	var n SessionNotification
	if err := json.Unmarshal(msg.Params, &n); err != nil {
		slog.Warn("acp: bad session/update", "err", err)
		return
	}
	c.handler.OnUpdate(n)
}

// handleRequest 处理 agent 的反向调用。每个请求都必须回，agent 在阻塞等。
func (c *Conn) handleRequest(msg rpcMessage) {
	timeout := reverseCallTimeout
	if msg.Method == "elicitation/create" {
		timeout = elicitationCallTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	result, err := c.route(ctx, msg)
	if err != nil {
		slog.Warn("acp: reverse call failed", "method", msg.Method, "err", err)
		_ = c.write(rpcMessage{
			ID:    msg.ID,
			Error: &rpcError{Code: -32603, Message: err.Error()},
		})
		return
	}

	raw, err := json.Marshal(result)
	if err != nil {
		_ = c.write(rpcMessage{ID: msg.ID, Error: &rpcError{Code: -32603, Message: err.Error()}})
		return
	}
	_ = c.write(rpcMessage{ID: msg.ID, Result: raw})
}

func (c *Conn) route(ctx context.Context, msg rpcMessage) (any, error) {
	switch msg.Method {
	case "session/request_permission":
		var p RequestPermissionParams
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			return nil, err
		}
		return c.handler.RequestPermission(ctx, p)

	case "elicitation/create":
		var p ElicitationParams
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			return nil, err
		}
		return c.handler.Elicitation(ctx, p)

	case "fs/read_text_file":
		var p ReadTextFileParams
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			return nil, err
		}
		return c.handler.ReadTextFile(ctx, p)

	case "fs/write_text_file":
		var p WriteTextFileParams
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			return nil, err
		}
		if err := c.handler.WriteTextFile(ctx, p); err != nil {
			return nil, err
		}
		return map[string]any{}, nil

	default:
		// 没声明的 capability 被调到，如实回 method not found。
		return nil, fmt.Errorf("method not supported: %s", msg.Method)
	}
}

func (c *Conn) processExitError(method string) error {
	detail := c.Stderr()
	if detail == "" {
		detail = "no stderr output"
	}
	if c.exitErr != nil {
		return fmt.Errorf("%s: agent exited (%v): %s", method, c.exitErr, truncate(detail, 1024))
	}
	return fmt.Errorf("%s: agent exited: %s", method, truncate(detail, 1024))
}

func (c *Conn) drainStderr(r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			c.stderr.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// ringBuffer 只保留最近写入的 N 字节。
type ringBuffer struct {
	mu   sync.Mutex
	buf  []byte
	size int
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{size: size}
}

func (b *ringBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.size {
		b.buf = b.buf[len(b.buf)-b.size:]
	}
	return len(p), nil
}

func (b *ringBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}
