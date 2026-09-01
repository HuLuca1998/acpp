package datasource

import (
	"path/filepath"
	"strings"

	"acpp/server/internal/gitrepo"
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

// Scope 是一次取数的作用域：先由 Cwd 定项目（下面那条硬隔离底座），
// 再由 Only 决定要不要进一步锁死到某一条数据源。
//
// Only 是给 discord 频道这类「一个入口对应一个环境」的场景用的：
// pp-game 的 prod/pre/dev 三个频道各绑各的库，AI 在 prod 频道里连
// dev 的连接都列不出来，也就无从查错库。锁定时不再推项目——显式绑定
// 本身就比路径推断精确（见 ForScope）。
type Scope struct {
	// Cwd 是会话的工作目录，决定项目归属。
	Cwd string
	// Only 非零时把可见范围锁死到这一条数据源（model.DataSource.ID）。
	Only uint
}

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
			rel = gitrepo.TrimWorktreeSeg(rel)
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
	if repo := gitrepo.NearestRepo(cwd); repo != "" {
		repo = gitrepo.TrimWorktreeSeg(repo)
		base := filepath.Base(repo)
		add(base)
		if i := strings.Index(base, "@"); i > 0 {
			add(base[:i])
		}
		// **项目就是一个 git 仓库**：身份取自 origin 的 URL（`<组织>/<仓库>`），
		// 与它被克隆到哪儿无关——同一个仓库在租户目录下、在 discord 工作树里、
		// 在 owner 自己的目录里，都该匹配到同一条数据源。
		//
		// 这也是数据库页项目下拉给的那个值。**不能拿路径的后两段代替**：
		// 工作区里那几层可能是租户名、分组目录，也可能压根没有组织层。
		add(gitrepo.NameOfDir(repo))
	}
	return names
}

// within 判断 path 是否在 root 之内（含 root 自身）。
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || !strings.HasPrefix(rel, "..")
}
