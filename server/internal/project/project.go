// Package project 负责工作区里的「项目」：发现、克隆、与会话的关联。
//
// 项目不进数据库，磁盘即事实源（与技能库同一套哲学）：目录里有 .git 就是
// 一个项目，用户在终端里手动 clone 出来的目录刷新页面就能看见。会话与项目
// 的关联同样不靠外键，靠 cwd 前缀匹配——改名、搬目录都不会留下悬空引用。
package project

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

// maxDepth 是项目扫描深度。租户的项目形状是 <组织>/<仓库>（两层），
// owner 的工作区根下还多一层租户名，所以给到 3。
const maxDepth = 3

// skipDirs 是扫描时不进入的目录：要么体量大得离谱，要么里面的 .git
// 不代表一个「项目」。
var skipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"worktrees":    true, // 会话的 worktree，归属它的主仓库
	"Library":      true,
	"Applications": true,
}

// Project 是工作区里的一个仓库目录。Name 是相对工作区根的路径
// （如 `BDBGAME2024/pp-game`），它同时是显示名与前端的标识。
type Project struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Remote string `json:"remote,omitempty"`
	Branch string `json:"branch,omitempty"`
	// SessionCount 是 cwd 落在这个项目里的会话数（含 worktree 子目录）。
	SessionCount int `json:"sessionCount"`
	// UpdatedAt 取目录修改时间，用于「最近用过的项目」排序。
	UpdatedAt time.Time `json:"updatedAt"`
}

// Service 是项目的业务面。持有 db 只为统计会话数与做项目分组，
// 项目本身不入库。
type Service struct {
	db     *gorm.DB
	clones *cloneTracker
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db, clones: newCloneTracker()}
}

// List 列出当前身份工作区根下的全部项目，按目录修改时间倒序。
func (s *Service) List(ctx context.Context, scope service.Scope) ([]Project, error) {
	root, err := scope.GuardPath("")
	if err != nil {
		return nil, err
	}

	projects, err := discover(root)
	if err != nil {
		return nil, err
	}

	sessions, err := s.sessionCwds(ctx, scope)
	if err != nil {
		return nil, err
	}
	// 项目路径来自扫 canonical 根，会话 cwd 却不一定解析过（owner 的路径
	// 不过 EvalSymlinks）。不对齐的话 /var 与 /private/var 这种同一目录的
	// 两种写法就永远匹配不上，会话数会白白显示成 0。
	for i := range sessions {
		sessions[i] = resolvePath(sessions[i])
	}
	for i := range projects {
		for _, cwd := range sessions {
			if underPath(projects[i].Path, cwd) {
				projects[i].SessionCount++
			}
		}
	}

	sort.Slice(projects, func(i, j int) bool {
		return projects[i].UpdatedAt.After(projects[j].UpdatedAt)
	})
	return projects, nil
}

// Create 在工作区根下建一个空项目目录（`<组织>/<仓库>` 两层）。
// 给的是「不从远端克隆，先建个地方干活」这条路。
func (s *Service) Create(scope service.Scope, name string) (*Project, error) {
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
	if _, err := scope.GuardNewPath(path); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, fmt.Errorf("create project dir: %w", err)
	}
	return &Project{Name: rel, Path: path, UpdatedAt: time.Now()}, nil
}

// Delete 删项目目录。会话记录不动——删掉工作目录不等于删掉干过的活的记录，
// 那些会话仍在列表里（只是打不开工作区面板）。
func (s *Service) Delete(scope service.Scope, name string) error {
	rel, err := cleanProjectName(name)
	if err != nil {
		return err
	}
	root, err := scope.GuardPath("")
	if err != nil {
		return err
	}
	path, err := scope.GuardPath(filepath.Join(root, rel))
	if err != nil {
		return err
	}
	// 兜一道：guard 已保证在 root 内，这里挡住「把 root 自己删了」。
	if path == root {
		return fmt.Errorf("%w: refusing to delete workspace root", service.ErrInvalid)
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return nil
}

// sessionCwds 取当前身份可见会话的 cwd 列表。
func (s *Service) sessionCwds(ctx context.Context, scope service.Scope) ([]string, error) {
	var cwds []string
	err := scope.FilterSessions(s.db.WithContext(ctx).Model(&model.Session{})).
		Where("cwd <> ''").Pluck("cwd", &cwds).Error
	if err != nil {
		return nil, fmt.Errorf("list session cwds: %w", err)
	}
	return cwds, nil
}

// discover 扫描根目录找出全部 git 仓库。命中 .git 就收下并**不再深入**：
// 仓库里的子模块与嵌套仓库不该各自成为一个项目。
func discover(root string) ([]Project, error) {
	projects := []Project{}

	var walk func(dir string, depth int) error
	walk = func(dir string, depth int) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			// 权限不足或目录消失都不该让整个列表失败。
			return nil
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || skipDirs[e.Name()] {
				continue
			}
			path := filepath.Join(dir, e.Name())
			if isGitRepo(path) {
				rel, err := filepath.Rel(root, path)
				if err != nil {
					continue
				}
				projects = append(projects, Project{
					Name:      filepath.ToSlash(rel),
					Path:      path,
					Remote:    gitRemote(path),
					Branch:    gitBranch(path),
					UpdatedAt: modTime(path),
				})
				continue
			}
			if depth < maxDepth {
				if err := walk(path, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if err := walk(root, 1); err != nil {
		return nil, err
	}
	return projects, nil
}

func isGitRepo(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	if err != nil {
		return false
	}
	// worktree 的 .git 是文件（gitdir 指针），主仓库是目录，两者都算。
	return info.IsDir() || info.Mode().IsRegular()
}

func modTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// cleanProjectName 校验项目名：相对路径、最多两层、不含上跳与隐藏段。
// 它会被拼进文件系统路径，所以宁可严格。
func cleanProjectName(name string) (string, error) {
	name = strings.Trim(strings.TrimSpace(name), "/")
	if name == "" {
		return "", fmt.Errorf("%w: project name is required", service.ErrInvalid)
	}
	segments := strings.Split(name, "/")
	if len(segments) > 2 {
		return "", fmt.Errorf("%w: project name must be <repo> or <owner>/<repo>", service.ErrInvalid)
	}
	for _, seg := range segments {
		if seg == "" || seg == "." || seg == ".." || strings.HasPrefix(seg, ".") ||
			strings.ContainsAny(seg, `\:*?"<>|`) {
			return "", fmt.Errorf("%w: invalid segment %q", service.ErrInvalid, seg)
		}
	}
	return filepath.Join(segments...), nil
}

// resolvePath 解析符号链接。目录已经不存在时（会话的 worktree 被删、
// 项目被搬走）逐级往上解析到最深的存在祖先，再把剩下的部分接回去——
// 否则一条指向已删目录的会话会因为解析失败而归不进它原本的项目。
func resolvePath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	parent := filepath.Dir(path)
	if parent == path {
		return filepath.Clean(path)
	}
	return filepath.Join(resolvePath(parent), filepath.Base(path))
}

// underPath 判断 path 是否落在 base 之内（或就是 base）。
func underPath(base, path string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// gitRemote 读 origin 的 URL。只读 .git/config 不 exec git：列表路径上
// 每个项目 fork 一次进程，项目一多就卡。
func gitRemote(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, ".git", "config"))
	if err != nil {
		return ""
	}
	inOrigin := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inOrigin = strings.HasPrefix(trimmed, `[remote "origin"]`)
			continue
		}
		if inOrigin && strings.HasPrefix(trimmed, "url") {
			if _, value, ok := strings.Cut(trimmed, "="); ok {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

// gitBranch 读当前分支名，理由同 gitRemote：不 exec git。
// worktree 的 .git 是指向真实 gitdir 的指针文件，跟一跳。
func gitBranch(dir string) string {
	gitPath := filepath.Join(dir, ".git")
	if info, err := os.Stat(gitPath); err == nil && !info.IsDir() {
		data, err := os.ReadFile(gitPath)
		if err != nil {
			return ""
		}
		if _, value, ok := strings.Cut(strings.TrimSpace(string(data)), "gitdir:"); ok {
			gitPath = strings.TrimSpace(value)
			if !filepath.IsAbs(gitPath) {
				gitPath = filepath.Join(dir, gitPath)
			}
		}
	}
	data, err := os.ReadFile(filepath.Join(gitPath, "HEAD"))
	if err != nil {
		return ""
	}
	head := strings.TrimSpace(string(data))
	if ref, ok := strings.CutPrefix(head, "ref: refs/heads/"); ok {
		return ref
	}
	if len(head) >= 7 {
		return head[:7] // detached HEAD 显示短 hash
	}
	return ""
}
