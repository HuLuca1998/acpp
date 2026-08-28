package project

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"acpp/server/internal/gitrepo"
	"acpp/server/internal/service"
)

// cloneTimeout 是单次克隆的硬上限。大仓库拉几分钟正常，卡住不动的
// （等凭证、网络黑洞）不该占着资源到天荒地老。
const cloneTimeout = 20 * time.Minute

// CloneState 是一次克隆任务的状态。
type CloneState string

const (
	CloneRunning CloneState = "running"
	CloneDone    CloneState = "done"
	CloneFailed  CloneState = "failed"
)

// Clone 是一次克隆任务。任务只存在于内存：进程重启时 git 子进程也一起
// 没了，留个「进行中」的假记录只会骗人。
type Clone struct {
	ID    string     `json:"id"`
	Name  string     `json:"name"`
	URL   string     `json:"url"`
	Path  string     `json:"path"`
	State CloneState `json:"state"`
	// Error 是失败原因（git 的 stderr 尾部），成功时为空。
	Error     string     `json:"error,omitempty"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
	// tenantID 用于列表过滤：克隆任务只对发起它的身份可见。
	tenantID uint
	owner    bool
}

type cloneTracker struct {
	mu     sync.Mutex
	clones map[string]*Clone
	seq    uint64
}

func newCloneTracker() *cloneTracker {
	return &cloneTracker{clones: map[string]*Clone{}}
}

// CloneInput 是发起克隆的入参。Name 为空时从 URL 推导 `<组织>/<仓库>`。
type CloneInput struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

// Clone 起一个后台克隆。立即返回任务，前端轮询 Clones 看进度——
// 大仓库要拉几分钟，HTTP 请求等不了。
func (s *Service) Clone(scope service.Scope, in CloneInput) (*Clone, error) {
	url := strings.TrimSpace(in.URL)
	if url == "" {
		return nil, fmt.Errorf("%w: url is required", service.ErrInvalid)
	}
	if !gitrepo.ValidCloneURL(url) {
		return nil, fmt.Errorf("%w: only https:// or git@host:owner/repo URLs are accepted", service.ErrInvalid)
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = gitrepo.Name(url)
	}
	rel, err := cleanProjectName(name)
	if err != nil {
		return nil, err
	}

	root, err := scope.GuardPath("")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(root, rel)
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("%w: %s already exists", service.ErrInvalid, rel)
	}
	// 组织目录（`<root>/<组织>`）先建出来，克隆目标本身留给 git 创建。
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create project parent: %w", err)
	}
	if _, err := scope.GuardNewPath(path); err != nil {
		return nil, err
	}

	clone := s.clones.add(scope, url, rel, path)
	go s.runClone(clone)
	return clone, nil
}

// Clones 列出当前身份发起的克隆任务（最近的在前）。
func (s *Service) Clones(scope service.Scope) []Clone {
	return s.clones.list(scope)
}

// runClone 执行克隆。
//
// **访客与 owner 用同一套本机 git 凭证**（产品决定）：这台机器上能拉到的
// 仓库，局域网访客也能拉。清单只管「界面上列出什么」，不再兼作能力边界——
// 手输 URL 同样会用凭证，包括 owner 个人账号名下的私有仓库。
// 换句话说：谁拿到邀请链接，谁就能拉走你 git 凭证够得着的任何仓库。
func (s *Service) runClone(clone *Clone) {
	ctx, cancel := context.WithTimeout(context.Background(), cloneTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "clone", clone.URL, clone.Path)
	cmd.Env = append(os.Environ(),
		// 凭证助手照常用；只是不要挂在终端提示上等一个永远不会来的输入。
		"GIT_TERMINAL_PROMPT=0",
	)
	output, err := cmd.CombinedOutput()

	s.clones.finish(clone.ID, err, string(output))
	if err != nil {
		// 半个仓库比没有仓库更糟：失败就把目录收干净，用户可以直接重来。
		_ = os.RemoveAll(clone.Path)
	}
}

func (t *cloneTracker) add(scope service.Scope, url, name, path string) *Clone {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.seq++
	clone := &Clone{
		ID:        fmt.Sprintf("clone-%d", t.seq),
		Name:      name,
		URL:       url,
		Path:      path,
		State:     CloneRunning,
		StartedAt: time.Now(),
		tenantID:  scope.TenantID,
		owner:     scope.Owner,
	}
	t.clones[clone.ID] = clone
	return clone
}

func (t *cloneTracker) finish(id string, err error, output string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	clone, ok := t.clones[id]
	if !ok {
		return
	}
	now := time.Now()
	clone.EndedAt = &now
	if err != nil {
		clone.State = CloneFailed
		clone.Error = tailLines(output, 6)
		if clone.Error == "" {
			clone.Error = err.Error()
		}
		return
	}
	clone.State = CloneDone
}

func (t *cloneTracker) list(scope service.Scope) []Clone {
	t.mu.Lock()
	defer t.mu.Unlock()

	out := make([]Clone, 0, len(t.clones))
	for _, c := range t.clones {
		if c.owner != scope.Owner || (!scope.Owner && c.tenantID != scope.TenantID) {
			continue
		}
		out = append(out, *c)
	}
	// 最近发起的在前。
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].StartedAt.After(out[i].StartedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// tailLines 取输出末尾的若干行：git 的失败原因基本都在最后几行，
// 全量塞进错误信息只会淹没它。
func tailLines(output string, n int) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
