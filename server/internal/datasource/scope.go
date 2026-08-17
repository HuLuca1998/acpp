package datasource

import (
	"os"
	"path/filepath"
	"strings"
)

// 项目归属：会话只能看见并操作**自己所在项目**的数据源。
//
// 这条约束是这个功能的安全底座。数据源里躺着的是生产库凭证，一个开在
// A 项目的会话（尤其是 AI）能列出 B 项目的连接，就等于把所有项目的库
// 摊在同一张桌子上——迟早有人在错的库上跑对的语句。所以过滤不做在
// 界面层，做在取数据源的那一步（见 Service.ForCwd），MCP 与斜杠命令
// 走的是同一个入口。
//
// 推不出项目就一个都看不见：宁可让用户去项目目录里开会话，也不要给
// 一个「在家目录随便聊聊就能连生产库」的口子。

// projectCandidates 从工作目录推出可能的项目名。
//
// 返回多个候选是因为项目名有两种写法都合理：工作区根下的相对路径
// （`BDBGAME2024/pp-game`）与仓库名本身（`pp-game`）。用户在配置里
// 填哪种都能对上，不用去背约定。
func projectCandidates(cwd, workspaceRoot string) []string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return nil
	}
	cwd = filepath.Clean(cwd)

	var names []string
	add := func(s string) {
		s = strings.Trim(s, "/")
		if s == "" {
			return
		}
		for _, existing := range names {
			if strings.EqualFold(existing, s) {
				return
			}
		}
		names = append(names, s)
	}

	if root := filepath.Clean(workspaceRoot); root != "." && within(root, cwd) {
		rel, err := filepath.Rel(root, cwd)
		if err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			// 隔离工作区（`<仓库>/worktrees/<名字>`）归属它的主仓库——
			// 在 worktree 里干活的会话和在主仓库里的是同一个项目。
			if i := strings.Index(rel, string(filepath.Separator)+"worktrees"+string(filepath.Separator)); i >= 0 {
				rel = rel[:i]
			}
			add(rel)
			add(filepath.Base(rel))
		}
	}

	// 工作区根之外的目录（owner 可以把会话开在任意位置）：用最近的
	// git 仓库目录名，仍然对得上「项目」这个概念。
	if repo := nearestRepo(cwd); repo != "" {
		add(filepath.Base(repo))
	}
	return names
}

// nearestRepo 向上找最近的含 .git 的目录（worktree 的 .git 是文件，同样算）。
func nearestRepo(dir string) string {
	for i := 0; i < 64; i++ {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
	return ""
}

// within 判断 path 是否在 root 之内（含 root 自身）。
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || !strings.HasPrefix(rel, "..")
}
