// Package gitrepo 提供 git 仓库地址的校验与命名推导。
// project（工作区克隆）与 discord（频道工作区克隆）共用，叶子包，不依赖本项目其他包。
package gitrepo

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// cloneURLRe 只放行 https 与 scp 形式的 git URL。挡掉 `file://`、`ext::`
// 这类能在本机乱指或直接执行命令的传输方式。
var cloneURLRe = regexp.MustCompile(`^(https://[\w.-]+/[\w./~-]+|[\w.-]+@[\w.-]+:[\w./~-]+)$`)

// ValidCloneURL 报告一个地址是否是可安全交给 git clone 的仓库 URL。
func ValidCloneURL(url string) bool {
	return cloneURLRe.MatchString(url)
}

// Name 把仓库 URL 还原成 `<组织>/<仓库>`——各处克隆落点都用这两层命名。
func Name(url string) string {
	trimmed := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(url), "/"), ".git")
	if _, after, ok := strings.Cut(trimmed, "@"); ok {
		// git@github.com:owner/repo
		if _, path, ok := strings.Cut(after, ":"); ok {
			trimmed = path
		}
	}
	segments := strings.Split(trimmed, "/")
	if len(segments) >= 2 {
		return segments[len(segments)-2] + "/" + segments[len(segments)-1]
	}
	return trimmed
}

// OriginURL 读一个仓库目录里 origin 的 URL。
//
// 只读 `.git/config` 不 exec git：这条路径在项目列表与会话取数据源时都会
// 走，每次 fork 一个 git 进程，项目一多就卡。
//
// 工作树的 `.git` 是指向真实 gitdir 的**文件**，那时 config 不在这个目录
// 下——跟一跳去找。
func OriginURL(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, ".git", "config"))
	if err != nil {
		if resolved := gitDirOf(dir); resolved != "" {
			data, err = os.ReadFile(filepath.Join(resolved, "config"))
		}
		if err != nil || len(data) == 0 {
			return ""
		}
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

// NameOfDir 是「这个目录属于哪个项目」的答案：`<组织>/<仓库>`。
//
// **项目就是一个 git 仓库**，与它被克隆到哪儿无关——同一个仓库在租户目录
// 下、在 discord 工作树里、在 owner 自己的目录里，都是同一个项目。所以身份
// 取自 origin 的 URL，而不是它落在磁盘上的位置。
//
// 没有 origin（本地新建、还没关联远端）时退回目录名——那时也没有更好的
// 答案，而目录名至少是人给它起的名字。工作树的 `@分支` 后缀会被剥掉。
func NameOfDir(dir string) string {
	if url := OriginURL(dir); url != "" {
		if name := Name(url); name != "" {
			return name
		}
	}
	base := filepath.Base(strings.TrimRight(dir, string(filepath.Separator)))
	if i := strings.Index(base, "@"); i > 0 {
		base = base[:i]
	}
	return base
}

// gitDirOf 跟一跳工作树的 `.git` 指针文件（内容形如 `gitdir: /path/to/.git/worktrees/x`）。
func gitDirOf(dir string) string {
	raw, err := os.ReadFile(filepath.Join(dir, ".git"))
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(raw))
	after, ok := strings.CutPrefix(line, "gitdir:")
	if !ok {
		return ""
	}
	p := strings.TrimSpace(after)
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	// 工作树的 gitdir 指向 `<主仓库>/.git/worktrees/<名>`，config 在主仓库的
	// .git 下——上跳两级。
	if base := filepath.Base(filepath.Dir(p)); base == "worktrees" {
		return filepath.Dir(filepath.Dir(p))
	}
	return p
}

// worktreeSegs 是「工作树容器目录」的名字：路径里出现它们，说明后面那一段
// 是分支的检出，项目本身在它们**之前**。
//
//   - `.worktree/<分支>` 是 discord 频道工作区的布局（adr-018）
//   - `worktrees/<名>`  是会话 worktree 的老布局
var worktreeSegs = []string{".worktree", "worktrees"}

// TrimWorktreeSeg 把路径截到工作树容器目录之前（不含），没有就原样返回。
func TrimWorktreeSeg(path string) string {
	for _, seg := range worktreeSegs {
		if i := strings.Index(path, string(filepath.Separator)+seg+string(filepath.Separator)); i >= 0 {
			return path[:i]
		}
	}
	return path
}

// NearestRepo 从 dir 往上找最近的 git 仓库目录（`.git` 存在即算，工作树的
// `.git` 是文件，同样认）。找不到返回空。
func NearestRepo(dir string) string {
	for range 64 {
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

// ProjectOf 是「这个工作目录属于哪个项目」的完整答案。
//
// **项目就是一个 git 仓库**：从 dir 往上找到仓库，再取它的身份
// （origin 推出的 `<组织>/<仓库>`，没有远端则用目录名）。因此
//   - 会话开在项目的子目录里 → 仍归属那个项目；
//   - 同一仓库克隆到租户目录、discord 工作树、owner 自己的目录 → 同一个项目；
//   - 工作树里的会话 → 归属主仓库，分支名不会被当成项目名。
//
// 不是仓库（会话可以开在任意目录）时返回空——那时它不属于任何项目，
// 调用方按「没有项目」处理，不要拿目录名硬凑一个。
func ProjectOf(dir string) string {
	repo := NearestRepo(strings.TrimSpace(dir))
	if repo == "" {
		return ""
	}
	return NameOfDir(TrimWorktreeSeg(repo))
}
