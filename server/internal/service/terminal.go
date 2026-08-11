package service

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
)

const (
	// terminalRingSize 是重连回放缓冲：页面刷新后凭它恢复现场。
	terminalRingSize = 64 << 10
	// terminalDetachGrace 是 ws 断开后 pty 的保活窗口——断线 ≠ 终端死，
	// 刷新页面要能无感重连；超过窗口才真正回收进程。
	terminalDetachGrace = 30 * time.Second
)

// TerminalInfo 是终端实例的对外视图。
type TerminalInfo struct {
	ID      string `json:"id"`
	Num     int    `json:"num"`
	Running bool   `json:"running"`
}

// TerminalService 是工作区终端（真 PTY）的注册表。生命周期语义：
// 关闭 tab = 杀进程；ws 断开保留 terminalDetachGrace 供重连；
// 服务关闭统一回收（main 装配 Shutdown）。
type TerminalService struct {
	mu    sync.Mutex
	seq   int
	max   int
	terms map[string]*terminalProc
}

func NewTerminalService(maxPerSession int) *TerminalService {
	if maxPerSession <= 0 {
		maxPerSession = 5
	}
	return &TerminalService{max: maxPerSession, terms: map[string]*terminalProc{}}
}

type terminalProc struct {
	id        string
	num       int
	sessionID uint
	ptmx      *os.File
	cmd       *exec.Cmd

	mu   sync.Mutex
	ring []byte
	sink func([]byte) error
	// sinkGen 标识当前 sink 的代数：重连顶替后，旧连接的 detach/失败
	// 清理不会误伤新 sink（func 值不可比较，用代数替代）。
	sinkGen     int
	done        chan struct{}
	exited      bool
	detachTimer *time.Timer
}

// Create 在会话工作目录里拉起一个交互 shell。编号会话内递增、不回填。
func (s *TerminalService) Create(sessionID uint, cwd string) (*TerminalInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	alive := 0
	maxNum := 0
	for _, t := range s.terms {
		if t.sessionID != sessionID {
			continue
		}
		if t.num > maxNum {
			maxNum = t.num
		}
		t.mu.Lock()
		if !t.exited {
			alive++
		}
		t.mu.Unlock()
	}
	if alive >= s.max {
		return nil, fmt.Errorf("%w: terminal limit reached (%d)", ErrInvalid, s.max)
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("start pty: %w", err)
	}

	s.seq++
	t := &terminalProc{
		id:        fmt.Sprintf("t%d", s.seq),
		num:       maxNum + 1,
		sessionID: sessionID,
		ptmx:      ptmx,
		cmd:       cmd,
		done:      make(chan struct{}),
	}
	s.terms[t.id] = t

	go t.readLoop()
	slog.Info("terminal started", "session", sessionID, "id", t.id, "shell", shell)
	return &TerminalInfo{ID: t.id, Num: t.num, Running: true}, nil
}

// List 列出会话的全部终端（含已结束的，前端据 Running 画重启空态）。
func (s *TerminalService) List(sessionID uint) []TerminalInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	infos := []TerminalInfo{}
	for _, t := range s.terms {
		if t.sessionID != sessionID {
			continue
		}
		t.mu.Lock()
		infos = append(infos, TerminalInfo{ID: t.id, Num: t.num, Running: !t.exited})
		t.mu.Unlock()
	}
	return infos
}

// Close 杀掉一个终端并从注册表移除（关闭 tab 的诚实语义）。
func (s *TerminalService) Close(sessionID uint, id string) error {
	s.mu.Lock()
	t, ok := s.terms[id]
	if ok && t.sessionID == sessionID {
		delete(s.terms, id)
	}
	s.mu.Unlock()
	if !ok || t.sessionID != sessionID {
		return fmt.Errorf("%w: terminal not found", ErrNotFound)
	}
	t.kill()
	return nil
}

// Shutdown 回收全部 pty，服务优雅退出时调用。
func (s *TerminalService) Shutdown() {
	s.mu.Lock()
	terms := make([]*terminalProc, 0, len(s.terms))
	for _, t := range s.terms {
		terms = append(terms, t)
	}
	s.terms = map[string]*terminalProc{}
	s.mu.Unlock()
	for _, t := range terms {
		t.kill()
	}
}

// Get 取终端实例供传输层（ws handler）桥接。
func (s *TerminalService) Get(sessionID uint, id string) (*TerminalProxy, error) {
	s.mu.Lock()
	t, ok := s.terms[id]
	s.mu.Unlock()
	if !ok || t.sessionID != sessionID {
		return nil, fmt.Errorf("%w: terminal not found", ErrNotFound)
	}
	return &TerminalProxy{t: t}, nil
}

// TerminalProxy 是暴露给传输层的窄接口：写入、调宽高、挂接输出。
type TerminalProxy struct{ t *terminalProc }

func (p *TerminalProxy) Write(data []byte) error {
	_, err := p.t.ptmx.Write(data)
	return err
}

func (p *TerminalProxy) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("%w: bad size", ErrInvalid)
	}
	return pty.Setsize(p.t.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// Attach 挂接输出 sink（新连接顶掉旧的），返回回放缓冲、退出信号与
// 解除函数。解除后开始 detach 保活倒计时。
func (p *TerminalProxy) Attach(sink func([]byte) error) (replay []byte, done <-chan struct{}, detach func()) {
	t := p.t
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.detachTimer != nil {
		t.detachTimer.Stop()
		t.detachTimer = nil
	}
	replay = append([]byte(nil), t.ring...)
	t.sinkGen++
	gen := t.sinkGen
	t.sink = sink
	return replay, t.done, func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		// 已被新连接顶掉时不动别人的 sink，也不起回收倒计时。
		if t.exited || t.sinkGen != gen {
			return
		}
		t.sink = nil
		t.detachTimer = time.AfterFunc(terminalDetachGrace, t.kill)
	}
}

func (t *terminalProc) readLoop() {
	buf := make([]byte, 32<<10)
	for {
		n, err := t.ptmx.Read(buf)
		if n > 0 {
			t.push(buf[:n])
		}
		if err != nil {
			t.finish()
			return
		}
	}
}

func (t *terminalProc) push(data []byte) {
	t.mu.Lock()
	t.ring = append(t.ring, data...)
	if len(t.ring) > terminalRingSize {
		t.ring = t.ring[len(t.ring)-terminalRingSize:]
	}
	sink := t.sink
	gen := t.sinkGen
	t.mu.Unlock()
	if sink != nil {
		// 发送失败视作连接已断：置空等重连，进程不受影响。
		if err := sink(append([]byte(nil), data...)); err != nil {
			t.mu.Lock()
			// 已被新连接替换（代数变化）时保留新 sink。
			if t.sinkGen == gen {
				t.sink = nil
			}
			t.mu.Unlock()
		}
	}
}

func (t *terminalProc) finish() {
	t.mu.Lock()
	already := t.exited
	t.exited = true
	if t.detachTimer != nil {
		t.detachTimer.Stop()
		t.detachTimer = nil
	}
	t.mu.Unlock()
	if already {
		return
	}
	_ = t.cmd.Wait()
	close(t.done)
	slog.Info("terminal exited", "session", t.sessionID, "id", t.id)
}

func (t *terminalProc) kill() {
	t.mu.Lock()
	exited := t.exited
	t.mu.Unlock()
	if !exited && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
	_ = t.ptmx.Close()
}
