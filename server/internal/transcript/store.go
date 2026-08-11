// Package transcript 以 JSONL 形式完整留存每条会话的 ACP 线级消息。
// 每个会话一个文件，每行一个 Entry：原始 JSON-RPC 消息加方向与时间戳。
// 这是对话内容的唯一持久化形态，数据库只保留会话元数据。
package transcript

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry 是 JSONL 里的一行。
type Entry struct {
	TS time.Time `json:"ts"`
	// Dir 是消息方向："send" 我们发给 agent，"recv" agent 发给我们。
	Dir string          `json:"dir"`
	Msg json.RawMessage `json:"msg"`
}

// Store 管理全部会话的转录文件，按会话 key（数据库 session id）索引。
type Store struct {
	dir string

	mu    sync.Mutex
	files map[string]*os.File
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create transcript dir: %w", err)
	}
	return &Store{dir: dir, files: make(map[string]*os.File)}, nil
}

// Path 返回会话转录文件的路径。
func (s *Store) Path(key string) string {
	return filepath.Join(s.dir, key+".jsonl")
}

// Append 追加一行。转录是旁路：写失败只记日志，绝不打断对话本身。
func (s *Store) Append(key, dir string, msg json.RawMessage) {
	line, err := json.Marshal(Entry{TS: time.Now(), Dir: dir, Msg: msg})
	if err != nil {
		slog.Error("transcript: marshal entry", "session", key, "err", err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	f, ok := s.files[key]
	if !ok {
		f, err = os.OpenFile(s.Path(key), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			slog.Error("transcript: open file", "session", key, "err", err)
			return
		}
		s.files[key] = f
	}

	if _, err := f.Write(append(line, '\n')); err != nil {
		slog.Error("transcript: append", "session", key, "err", err)
	}
}

// Read 读出一条会话的全部转录。文件不存在返回空列表——新会话就是这样。
func (s *Store) Read(key string) ([]Entry, error) {
	f, err := os.Open(s.Path(key))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []Entry
	scanner := bufio.NewScanner(f)
	// 单行可能带很大的 rawOutput，与 conn 的读缓冲同级。
	scanner.Buffer(make([]byte, 0, 64<<10), 16<<20)
	for scanner.Scan() {
		var e Entry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			// 半行（比如进程被杀时写了一半）跳过，不让整个文件不可读。
			continue
		}
		entries = append(entries, e)
	}
	return entries, scanner.Err()
}

// Close 关闭该会话的文件句柄；会话进程回收时调用。
func (s *Store) Close(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.files[key]; ok {
		_ = f.Close()
		delete(s.files, key)
	}
}

// CloseAll 在服务退出时关闭全部句柄。
func (s *Store) CloseAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, f := range s.files {
		_ = f.Close()
		delete(s.files, key)
	}
}
