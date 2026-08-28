package discord

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Config 是 discord 子系统的全部持久状态：开关、bot 凭证、工作根与
// 频道绑定。单文件 JSON（<dataDir>/discord.json，0600），不进数据库——
// 整个子系统的回退面就是删掉这个文件（adr-016）。
type Config struct {
	Enabled  bool      `json:"enabled"`
	BotToken string    `json:"botToken,omitempty"`
	WorkRoot string    `json:"workRoot,omitempty"`
	Bindings []Binding `json:"bindings,omitempty"`
	// Threads 是子区 ↔ acp 会话的对应：进程重启后凭 ACPSessionID 走
	// session/load 恢复上下文，子区因此可以一直聊下去。
	Threads []Thread `json:"threads,omitempty"`
}

// Thread 是一个对话子区（Discord thread）。ChannelID 是它所属的绑定频道。
type Thread struct {
	ThreadID     string    `json:"threadId"`
	ChannelID    string    `json:"channelId"`
	ACPSessionID string    `json:"acpSessionId,omitempty"`
	Title        string    `json:"title,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

// Binding 是一条「频道 ↔ 仓库工作区」的绑定：/init 表单的落盘结果。
// 频道内后续的 acp 工作目录就是 Workdir（本期只建绑定，对话在下一期）。
type Binding struct {
	ChannelID   string `json:"channelId"`
	ChannelName string `json:"channelName,omitempty"`
	GuildID     string `json:"guildId,omitempty"`
	// Repo 是规范化的 `<组织>/<仓库>`；CloneURL 是实际交给 git 的地址。
	Repo     string `json:"repo"`
	CloneURL string `json:"cloneUrl"`
	// Branch 空串表示默认分支；指定分支的克隆落在 `<仓库>@<分支>` 目录，
	// 与默认分支的克隆互不打扰（多个频道可能共享同一仓库）。
	Branch  string `json:"branch,omitempty"`
	Workdir string `json:"workdir"`
	// Agent/Model 对齐内置工具的探测缓存（claude/codex 与其模型 id）；
	// Effort 是统一思考深度五档，空串表示用 agent 默认。
	Agent      string `json:"agent"`
	Model      string `json:"model"`
	ModelLabel string `json:"modelLabel,omitempty"`
	Effort     string `json:"effort,omitempty"`
	// Access 是统一权限档（safe/auto-edit/full，对齐 acp.AccessLevel）；
	// 空串按 auto-edit 兜底（老绑定没这字段）。
	Access string `json:"access,omitempty"`
	// CardMessageID 是早期版本置顶身份卡的遗留（卡已退役，频道侧常驻
	// 信息面只有主题）；非空时下次同步会把卡删掉并清空此字段。
	CardMessageID string    `json:"cardMessageId,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// store 负责 Config 的加载与原子写回。锁只保护内存副本与文件——
// 业务判断在 Service 层。
type store struct {
	path string
	mu   sync.Mutex
	cfg  Config
}

func newStore(path string) (*store, error) {
	s := &store{path: path}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, fmt.Errorf("读 discord 配置: %w", err)
	}
	if err := json.Unmarshal(data, &s.cfg); err != nil {
		return nil, fmt.Errorf("解 discord 配置 %s: %w", path, err)
	}
	return s, nil
}

// config 返回配置副本（含 bindings 切片拷贝），调用方随便改不影响存储。
func (s *store) config() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.clone()
}

// update 在锁内改配置并写盘。mutate 里只做赋值，不做慢操作。
func (s *store) update(mutate func(*Config)) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cfg.clone()
	mutate(&next)
	if err := s.write(next); err != nil {
		return Config{}, err
	}
	s.cfg = next
	return next.clone(), nil
}

// write 原子落盘：临时文件 + rename，0600（里面有 bot token）。
func (s *store) write(cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("建配置目录: %w", err)
	}
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("写 discord 配置: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("落盘 discord 配置: %w", err)
	}
	return nil
}

func (c Config) clone() Config {
	out := c
	out.Bindings = append([]Binding(nil), c.Bindings...)
	out.Threads = append([]Thread(nil), c.Threads...)
	return out
}

// AccessOrDefault 是权限档的读取口径：老绑定没存按 auto-edit 算。
func (b Binding) AccessOrDefault() string {
	if b.Access == "" {
		return "auto-edit"
	}
	return b.Access
}

// binding 按频道查绑定。
func (c Config) binding(channelID string) (Binding, bool) {
	for _, b := range c.Bindings {
		if b.ChannelID == channelID {
			return b, true
		}
	}
	return Binding{}, false
}

// upsertBinding 以频道为键写入或覆盖绑定（一个频道只有一份工作区）。
func (c *Config) upsertBinding(b Binding) {
	for i := range c.Bindings {
		if c.Bindings[i].ChannelID == b.ChannelID {
			b.CreatedAt = c.Bindings[i].CreatedAt
			c.Bindings[i] = b
			return
		}
	}
	c.Bindings = append(c.Bindings, b)
}

// removeBinding 解绑频道；报告是否真的删了东西。子区记录一并清掉——
// 绑定没了它们指向的工作区也没了。
func (c *Config) removeBinding(channelID string) bool {
	for i := range c.Bindings {
		if c.Bindings[i].ChannelID == channelID {
			c.Bindings = append(c.Bindings[:i], c.Bindings[i+1:]...)
			kept := c.Threads[:0]
			for _, t := range c.Threads {
				if t.ChannelID != channelID {
					kept = append(kept, t)
				}
			}
			c.Threads = kept
			return true
		}
	}
	return false
}

// thread 按子区 id 查记录。
func (c Config) thread(threadID string) (Thread, bool) {
	for _, t := range c.Threads {
		if t.ThreadID == threadID {
			return t, true
		}
	}
	return Thread{}, false
}

// upsertThread 以子区为键写入或覆盖。
func (c *Config) upsertThread(t Thread) {
	for i := range c.Threads {
		if c.Threads[i].ThreadID == t.ThreadID {
			if t.CreatedAt.IsZero() {
				t.CreatedAt = c.Threads[i].CreatedAt
			}
			c.Threads[i] = t
			return
		}
	}
	c.Threads = append(c.Threads, t)
}
