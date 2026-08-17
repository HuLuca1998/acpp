package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// worktreeDir 是会话 worktree 的固定落点：`<仓库>/worktrees/<名字>`。
// 固定下来，界面才能一眼认出哪些目录是本系统开的隔离工作区。
const worktreeDir = "worktrees"

// branchNameRe 限制分支/worktree 名字。git 自己的 check-ref-format 更全，
// 但这些名字要拼进命令行与文件路径，宁可只放行一眼安全的字符集。
var branchNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,99}$`)

// GitBranch 是分支清单里的一条。
type GitBranch struct {
	Name    string `json:"name"`
	Current bool   `json:"current"`
	// Worktree 非空表示这个分支已经被某个 worktree 占着——git 不允许两处
	// 同时 checkout 同一分支，界面要据此把它标成不可切。
	Worktree string `json:"worktree,omitempty"`
}

// GitWorktree 是一个工作区（含主工作区）。
type GitWorktree struct {
	Path   string `json:"path"`
	Branch string `json:"branch,omitempty"`
	// Main 表示这是仓库的主工作区（不是 worktree add 出来的）。
	Main bool `json:"main"`
	// Current 表示这就是当前会话的工作目录。
	Current bool `json:"current"`
}

// GitBranchView 是会话底部分支控件的全部数据。
type GitBranchView struct {
	IsRepo  bool   `json:"isRepo"`
	Current string `json:"current,omitempty"`
	// Detached 时 Current 是短 hash，不能当分支名用。
	Detached  bool          `json:"detached"`
	Dirty     bool          `json:"dirty"`
	Local     []GitBranch   `json:"local"`
	Remote    []string      `json:"remote"`
	Tags      []string      `json:"tags"`
	Worktrees []GitWorktree `json:"worktrees"`
}

// WorkspaceGitBranches 读分支与工作区清单，供会话底部的分支控件用。
func WorkspaceGitBranches(ctx context.Context, cwd string) (*GitBranchView, error) {
	if cwd == "" {
		return &GitBranchView{}, nil
	}
	if _, err := runGit(ctx, cwd, "rev-parse", "--is-inside-work-tree"); err != nil {
		return &GitBranchView{}, nil
	}

	// 三个清单一律给空切片而不是 nil：JSON 里 null 与 [] 对前端是两种东西，
	// 让界面到处写 `?? []` 不如后端一次性给干净。
	view := &GitBranchView{
		IsRepo:    true,
		Local:     []GitBranch{},
		Remote:    []string{},
		Tags:      []string{},
		Worktrees: []GitWorktree{},
	}

	head, err := runGit(ctx, cwd, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, err
	}
	view.Current = strings.TrimSpace(head)
	if view.Current == "HEAD" {
		view.Detached = true
		if short, err := runGit(ctx, cwd, "rev-parse", "--short", "HEAD"); err == nil {
			view.Current = strings.TrimSpace(short)
		}
	}

	if status, err := runGit(ctx, cwd, "status", "--porcelain"); err == nil {
		view.Dirty = strings.TrimSpace(status) != ""
	}

	if worktrees := parseWorktrees(ctx, cwd); worktrees != nil {
		view.Worktrees = worktrees
	}
	byBranch := map[string]string{}
	for _, wt := range view.Worktrees {
		if wt.Branch != "" {
			byBranch[wt.Branch] = wt.Path
		}
	}

	if out, err := runGit(ctx, cwd, "branch", "--format=%(refname:short)"); err == nil {
		for _, name := range strings.Split(strings.TrimSpace(out), "\n") {
			if name = strings.TrimSpace(name); name == "" {
				continue
			}
			branch := GitBranch{Name: name, Current: !view.Detached && name == view.Current}
			// 当前工作区占用的分支不算「被占」——切到自己没有意义但也不是冲突。
			if path, ok := byBranch[name]; ok && !sameDir(path, cwd) {
				branch.Worktree = path
			}
			view.Local = append(view.Local, branch)
		}
	}

	if out, err := runGit(ctx, cwd, "branch", "-r", "--format=%(refname:short)"); err == nil {
		for _, name := range strings.Split(strings.TrimSpace(out), "\n") {
			name = strings.TrimSpace(name)
			// origin/HEAD 是个符号引用，不是能切过去的分支。
			if name == "" || strings.HasSuffix(name, "/HEAD") {
				continue
			}
			view.Remote = append(view.Remote, name)
		}
	}
	// 标签按创建时间倒序取前 50：老标签谁也不会在面板里翻到底。
	if out, err := runGit(ctx, cwd, "tag", "--list", "--sort=-creatordate"); err == nil {
		for _, name := range strings.Split(strings.TrimSpace(out), "\n") {
			if name = strings.TrimSpace(name); name == "" {
				continue
			}
			view.Tags = append(view.Tags, name)
			if len(view.Tags) >= 50 {
				break
			}
		}
	}
	return view, nil
}

// CheckoutInput 是切换分支的入参。
type CheckoutInput struct {
	Branch string `json:"branch"`
	// Create 为 true 时从当前 HEAD 新建分支再切过去。
	Create bool `json:"create"`
}

// WorkspaceGitCheckout 切换分支，返回切换后的分支视图。
//
// 有未提交改动时**不切**：git 自己会在冲突时拒绝、在不冲突时把改动带过去，
// 后者对使用者是惊吓（改动跟着跑到另一条分支上）。这里统一挡住，让人先
// 自己决定提交还是储藏。
func WorkspaceGitCheckout(ctx context.Context, cwd string, in CheckoutInput) (*GitBranchView, error) {
	branch := strings.TrimSpace(in.Branch)
	if !branchNameRe.MatchString(branch) {
		return nil, fmt.Errorf("%w: invalid branch name", ErrInvalid)
	}
	if _, err := runGit(ctx, cwd, "rev-parse", "--is-inside-work-tree"); err != nil {
		return nil, fmt.Errorf("%w: not a git repository", ErrInvalid)
	}

	if status, err := runGit(ctx, cwd, "status", "--porcelain"); err == nil && strings.TrimSpace(status) != "" {
		return nil, fmt.Errorf("%w: working tree has uncommitted changes", ErrInvalid)
	}

	args := []string{"checkout"}
	if in.Create {
		args = append(args, "-b")
	}
	args = append(args, branch)
	if _, err := runGit(ctx, cwd, args...); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	return WorkspaceGitBranches(ctx, cwd)
}

// WorktreeInput 是开 worktree 的入参。Branch 为空时用 Name 当分支名。
type WorktreeInput struct {
	Name   string `json:"name"`
	Branch string `json:"branch"`
}

// CreateWorktree 在仓库下开一个隔离工作区（`<仓库>/worktrees/<名字>`）并
// 返回它的路径——会话勾选 worktree 时，工作目录就指向这里。
//
// 分支已存在就直接 checkout 到新工作区，不存在则新建：使用者要的是「给我
// 一个不打扰主工作区的地方干活」，不该被分支存不存在这种细节绊住。
func CreateWorktree(ctx context.Context, scope Scope, repo string, in WorktreeInput) (string, error) {
	name := strings.TrimSpace(in.Name)
	if !branchNameRe.MatchString(name) || strings.Contains(name, "/") {
		return "", fmt.Errorf("%w: invalid worktree name", ErrInvalid)
	}
	branch := strings.TrimSpace(in.Branch)
	if branch == "" {
		branch = name
	}
	if !branchNameRe.MatchString(branch) {
		return "", fmt.Errorf("%w: invalid branch name", ErrInvalid)
	}

	repo, err := scope.GuardPath(repo)
	if err != nil {
		return "", err
	}
	if _, err := runGit(ctx, repo, "rev-parse", "--is-inside-work-tree"); err != nil {
		return "", fmt.Errorf("%w: %s is not a git repository", ErrInvalid, repo)
	}
	// 已经在 worktree 里就回到主仓库开，避免套娃。
	if root, err := runGit(ctx, repo, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil {
		if main := filepath.Dir(strings.TrimSpace(root)); main != "" && main != "." {
			if guarded, err := scope.GuardPath(main); err == nil {
				repo = guarded
			}
		}
	}

	path := filepath.Join(repo, worktreeDir, name)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%w: worktree %s already exists", ErrInvalid, name)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create worktree dir: %w", err)
	}
	if _, err := scope.GuardNewPath(path); err != nil {
		return "", err
	}

	// 分支存在就把它 checkout 到新工作区，不存在就顺手建出来——两种写法
	// 的参数顺序不同（`add -b <新分支> <路径>` vs `add <路径> <已有分支>`）。
	_, err = runGit(ctx, repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	var args []string
	if err != nil {
		args = []string{"worktree", "add", "-b", branch, path}
	} else {
		args = []string{"worktree", "add", path, branch}
	}
	if _, err := runGit(ctx, repo, args...); err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	return path, nil
}

// RemoveWorktree 拆掉一个 worktree 并清理目录。分支保留——删工作区不等于
// 删掉在上面干过的活。
func RemoveWorktree(ctx context.Context, scope Scope, path string) error {
	path, err := scope.GuardPath(path)
	if err != nil {
		return err
	}
	if filepath.Base(filepath.Dir(path)) != worktreeDir {
		return fmt.Errorf("%w: not a session worktree", ErrInvalid)
	}
	if _, err := runGit(ctx, path, "rev-parse", "--is-inside-work-tree"); err != nil {
		return fmt.Errorf("%w: not a git worktree", ErrInvalid)
	}
	// 从 worktree 目录本身发 remove 会失败（git 不让删自己脚下的工作区），
	// 所以退到它的父仓库去执行。
	repo := filepath.Dir(filepath.Dir(path))
	if _, err := runGit(ctx, repo, "worktree", "remove", "--force", path); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	return nil
}

// parseWorktrees 解析 `git worktree list --porcelain`：每条记录以空行分隔，
// 字段是 `worktree <path>` / `branch refs/heads/<name>` / `detached`。
func parseWorktrees(ctx context.Context, cwd string) []GitWorktree {
	out, err := runGit(ctx, cwd, "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}

	var (
		list    []GitWorktree
		current GitWorktree
		started bool
	)
	flush := func() {
		if started && current.Path != "" {
			current.Main = len(list) == 0 // 第一条永远是主工作区
			current.Current = sameDir(current.Path, cwd)
			list = append(list, current)
		}
		current = GitWorktree{}
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			flush()
			started = false
		case strings.HasPrefix(line, "worktree "):
			flush()
			started = true
			current.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch "):
			current.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	flush()
	return list
}

// sameDir 比较两个目录是否同一位置（符号链接解析后比）。
func sameDir(a, b string) bool {
	if a == b {
		return true
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}
