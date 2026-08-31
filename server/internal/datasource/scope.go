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
			rel = trimWorktreeSeg(rel)
			add(rel)
			add(filepath.Base(rel))
			// 老 discord 工作区命名是 <仓库>@<分支>（新布局改成了工作树，
			// 见 trimWorktreeSeg）——旧绑定还在磁盘上，剥掉 @ 后缀再给一个
			// 候选（仓库名不含 @，安全）。
			if base := filepath.Base(rel); strings.Contains(base, "@") {
				add(base[:strings.Index(base, "@")])
			}
		}
	}

	// 工作区根之外的目录（owner 可以把会话开在任意位置）：用最近的
	// git 仓库目录名，仍然对得上「项目」这个概念。工作树的 .git 是文件，
	// 同样会被 nearestRepo 认出来，所以这里也要剥一次工作树段——否则
	// `<项目>/.worktree/live` 会把分支名 live 当成项目名。
	if repo := nearestRepo(cwd); repo != "" {
		base := filepath.Base(trimWorktreeSeg(repo))
		add(base)
		if i := strings.Index(base, "@"); i > 0 {
			add(base[:i])
		}
	}
	return names
}

// worktreeSegs 是「工作树容器目录」的两种写法：网页会话的隔离工作区是
// `<项目>/worktrees/<名字>`，discord 频道工作树是 `<项目>/.worktree/<分支>`。
// 两种都归属上面那个项目——在工作树里干活的会话和在主仓库里的是同一个项目。
var worktreeSegs = []string{"worktrees", ".worktree"}

// trimWorktreeSeg 把路径截到工作树容器目录之前（不含），没有就原样返回。
func trimWorktreeSeg(path string) string {
	for _, seg := range worktreeSegs {
		if i := strings.Index(path, string(filepath.Separator)+seg+string(filepath.Separator)); i >= 0 {
			return path[:i]
		}
	}
	return path
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
