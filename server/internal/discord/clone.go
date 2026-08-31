package discord

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"acpp/server/internal/gitrepo"
)

// 频道工作区的磁盘布局（adr-018）：一个仓库在工作根下占一个**项目目录**，
// git 数据只有 `.repo` 一份（bare），每个分支一棵 `.worktree/<分支>` 工作树。
//
//	<workRoot>/<组织>/<仓库>/
//	  ├── .repo/             唯一一份 git 数据（bare，没有工作区）
//	  └── .worktree/<分支>/   频道的工作目录
//
// 三条都是踩出来的：
//
//   - **频道的 cwd 永远是一棵单分支工作树**。AI 的 grep/glob 会把工作目录
//     整棵扫一遍，目录里躺着别的分支的副本，就会出现「在 prod 频道问，答
//     的却是 live 分支的代码」。所以项目目录只是容器，不是工作树。
//   - **git 数据 bare 且只有一份**：三个环境频道 ≈ 一份历史 + 三份工作文件，
//     `.repo` 一处 fetch 三棵树都受益。bare 没有主工作树占着分支，因此
//     **默认分支也能建出工作树**（老布局那种「一频道一个完整 clone」既
//     浪费，同分支的两个频道还会共用一个检出互相拽）。
//   - **fetch 的 refspec 必须改**：`clone --bare` 默认把远端分支直接写进
//     本地 `refs/heads/*`，而本地分支正被工作树占用，fetch 会被拒。改成
//     `+refs/heads/*:refs/remotes/origin/*` 之后，远端状态落在 origin/*，
//     本地分支由各棵工作树自己管。

const (
	// cloneTimeout 压在 interaction token 的 15 分钟时效之内：超过它连
	// 「失败了」都没法回写到频道里，不如果断掐掉让用户看到原因。
	cloneTimeout = 12 * time.Minute
	// fetchTimeout 短得多——建树前的 fetch 只为拿到最新的远端分支，拿不到
	// 也能用本地已有的 ref 继续（离线可用比永远最新重要）。
	fetchTimeout = 3 * time.Minute

	gitDirName      = ".repo"
	worktreeDirName = ".worktree"
)

// shorthandRe 认 `owner/repo` 简写（用户日常输入形态，如 BDBGAME2024/pp-game）。
var shorthandRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// resolveRepo 把表单输入解析成规范名与克隆地址。简写按 GitHub https 解析
// （本机 git 凭证助手负责私仓授权）；完整 URL 沿用 gitrepo 的安全闸。
func resolveRepo(input string) (name, cloneURL string, err error) {
	in := strings.TrimSpace(input)
	switch {
	case in == "":
		return "", "", fmt.Errorf("%w: 仓库不能为空", ErrInvalid)
	case shorthandRe.MatchString(in):
		name, cloneURL = strings.TrimSuffix(in, ".git"), "https://github.com/"+in
		if !strings.HasSuffix(cloneURL, ".git") {
			cloneURL += ".git"
		}
	case gitrepo.ValidCloneURL(in):
		name, cloneURL = gitrepo.Name(in), in
	default:
		return "", "", fmt.Errorf("%w: 认不出的仓库形式（要 owner/repo 或 https/git@ 地址）", ErrInvalid)
	}
	// 名字要当磁盘落点用，段里不许藏路径把戏。
	for seg := range strings.SplitSeq(name, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.HasPrefix(seg, ".") {
			return "", "", fmt.Errorf("%w: 仓库名不合法: %s", ErrInvalid, name)
		}
	}
	return name, cloneURL, nil
}

// repoHome 是一个仓库在工作根下的项目目录（纯容器，不是工作树）。
func repoHome(workRoot, repo string) string {
	return filepath.Join(workRoot, filepath.FromSlash(repo))
}

// gitHome 是项目目录里那份 bare git 数据。
func gitHome(home string) string { return filepath.Join(home, gitDirName) }

// worktreeDir 是一个分支的工作树落点。目录名由分支唯一决定——绑同一分支的
// 两个频道因此共用一棵树（同分支同代码，git 本来也不许一个分支挂两棵树）。
func worktreeDir(home, branch string) string {
	return filepath.Join(home, worktreeDirName, safeBranchDir(branch))
}

// safeBranchDir 把分支名压成一段安全的目录名：git 分支允许 `/`（feat/login）
// 和 `.`，直接当路径用会拆成多层，`..` 更是直接穿越出去。
func safeBranchDir(branch string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			return r
		default:
			return '-'
		}
	}, branch)
	// 点开头会与 .repo/.worktree 混作一类，`.` 与 `..` 是路径穿越。
	safe = strings.TrimLeft(safe, ".")
	if safe == "" {
		safe = "branch"
	}
	return safe
}

// ensureWorktree 保证 `<home>/.worktree/<分支>` 是这个分支的一棵工作树，
// 返回工作目录、**实际**分支名与是否复用了已有的树。
//
// branch 传空表示默认分支：克隆之后从 HEAD 现读出来，因此落盘的绑定里
// 存的永远是真实分支名，展示与 /status 不用再猜「默认」是哪个。
func ensureWorktree(ctx context.Context, cloneURL, home, branch string) (dir, resolved string, reused bool, err error) {
	git := gitHome(home)
	if err := ensureGitHome(ctx, cloneURL, git); err != nil {
		return "", "", false, err
	}
	if branch == "" {
		if branch, err = defaultBranchOf(ctx, git); err != nil {
			return "", "", false, err
		}
	}
	// fetch 失败不拦路：本地已有 ref 就还能建树，离线也该能干活。
	if out, ferr := runGit(ctx, fetchTimeout, git, "fetch", "--prune", "origin"); ferr != nil {
		slog.Warn("fetch 失败，用本地已有的 ref 继续", "repo", home, "err", gitReason(out, ferr))
	}

	dir = worktreeDir(home, branch)
	if st, statErr := os.Stat(dir); statErr == nil {
		if !st.IsDir() {
			return "", "", false, fmt.Errorf("工作树落点被文件占了: %s", dir)
		}
		// 工作树的 .git 是文件（指回 .repo/worktrees/<名>），不是目录。
		if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
			return "", "", false, fmt.Errorf("目录已存在但不是工作树: %s", dir)
		}
		// 分支名里的 `/` 被压成 `-`，理论上两个分支可能撞到同一目录名——
		// 复用前核一次它检出的到底是谁，别把 feat/x 的树当成 feat-x 用。
		if cur, curErr := currentBranch(ctx, dir); curErr == nil && cur != branch {
			return "", "", false, fmt.Errorf("目录 %s 已被分支 %s 占用，换个分支名再绑", dir, cur)
		}
		return dir, branch, true, nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", "", false, fmt.Errorf("建工作树目录: %w", err)
	}

	args := []string{"worktree", "add", dir, branch}
	if hasLocalBranch(ctx, git, branch) {
		// 本地分支是上次 clone/建树时的快照，可能落后于远端；分支没被任何
		// 工作树占用（占用的话上面已经复用返回了），可以直接对齐到 origin。
		if hasRemoteBranch(ctx, git, branch) {
			if out, err := runGit(ctx, time.Minute, git, "branch", "-f", branch, "origin/"+branch); err != nil {
				slog.Warn("对齐本地分支失败，按本地状态建树", "branch", branch, "err", gitReason(out, err))
			}
		}
	} else {
		// 本地还没有这个分支：以 origin/<分支> 为起点建一个跟踪分支。
		args = []string{"worktree", "add", "--track", "-b", branch, dir, "origin/" + branch}
	}
	if out, err := runGit(ctx, cloneTimeout, git, args...); err != nil {
		_ = os.RemoveAll(dir)
		_, _ = runGit(ctx, 30*time.Second, git, "worktree", "prune")
		return "", "", false, fmt.Errorf("git worktree add 失败: %s", gitReason(out, err))
	}
	return dir, branch, false, nil
}

// ensureGitHome 保证 bare git 数据就位：clone 过就直接用，否则现场克隆并
// 把 fetch refspec 改成 origin/*（理由见文件头第三条）。
func ensureGitHome(ctx context.Context, cloneURL, git string) error {
	if _, err := os.Stat(filepath.Join(git, "HEAD")); err == nil {
		return nil
	}
	if _, err := os.Stat(git); err == nil {
		return fmt.Errorf("目录已存在但不是 git 仓库: %s", git)
	}
	if err := os.MkdirAll(filepath.Dir(git), 0o755); err != nil {
		return fmt.Errorf("建项目目录: %w", err)
	}
	if out, err := runGit(ctx, cloneTimeout, "", "clone", "--bare", cloneURL, git); err != nil {
		_ = os.RemoveAll(git)
		return fmt.Errorf("git clone 失败: %s", gitReason(out, err))
	}
	if out, err := runGit(ctx, time.Minute, git,
		"config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		_ = os.RemoveAll(git)
		return fmt.Errorf("设置 fetch refspec 失败: %s", gitReason(out, err))
	}
	return nil
}

// defaultBranchOf 读 bare 仓库 HEAD 指向的分支（clone 时从远端 HEAD 带过来的）。
func defaultBranchOf(ctx context.Context, git string) (string, error) {
	out, err := runGit(ctx, 30*time.Second, git, "symbolic-ref", "--short", "HEAD")
	name := strings.TrimSpace(out)
	if err != nil || name == "" {
		return "", fmt.Errorf("读默认分支失败: %s", gitReason(out, err))
	}
	return name, nil
}

// currentBranch 读一棵工作树当前检出的分支。
func currentBranch(ctx context.Context, dir string) (string, error) {
	out, err := runGit(ctx, 30*time.Second, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func hasLocalBranch(ctx context.Context, git, branch string) bool {
	_, err := runGit(ctx, 30*time.Second, git, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

func hasRemoteBranch(ctx context.Context, git, branch string) bool {
	_, err := runGit(ctx, 30*time.Second, git, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branch)
	return err == nil
}

// runGit 跑一条 git：dir 非空时加 -C，凭证助手照常用但不许挂在终端提示上
// 等一个永远不会来的输入。
func runGit(ctx context.Context, timeout time.Duration, dir string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	cmd := exec.CommandContext(cctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// gitReason 从 git 的输出里挑一句能给人看的原因（失败原因基本在末尾）。
func gitReason(output string, err error) string {
	if reason := tailLines(output, 6); reason != "" {
		return reason
	}
	if err != nil {
		return err.Error()
	}
	return "未知错误"
}

// lsRemoteBranches 现查远端的默认分支与分支清单（--symref 让 HEAD 的指向
// 一起回来，一次网络往返拿全）。
func lsRemoteBranches(ctx context.Context, cloneURL string) (defaultBranch string, branches []string, err error) {
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--symref", cloneURL)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		reason := tailLines(string(output), 4)
		if reason == "" {
			reason = err.Error()
		}
		return "", nil, fmt.Errorf("git ls-remote 失败: %s", reason)
	}
	defaultBranch, branches = parseLsRemote(string(output))
	return defaultBranch, branches, nil
}

// parseLsRemote 解 ls-remote --symref 输出：
//
//	ref: refs/heads/main\tHEAD        → 默认分支
//	<sha>\trefs/heads/feature/x       → 分支清单
func parseLsRemote(output string) (defaultBranch string, branches []string) {
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[0] == "ref:" && len(fields) >= 3 && fields[2] == "HEAD" {
			defaultBranch = strings.TrimPrefix(fields[1], "refs/heads/")
			continue
		}
		if name, ok := strings.CutPrefix(fields[1], "refs/heads/"); ok {
			branches = append(branches, name)
		}
	}
	return defaultBranch, branches
}

// gitStatus 是一棵工作树的改动摘要（/git 命令用）。
type gitStatus struct {
	// Branch 是当前分支；detached HEAD 时是空串。
	Branch string
	// Upstream 非空时 Ahead/Behind 才有意义。
	Upstream      string
	Ahead, Behind int

	Modified   []string
	Added      []string
	Deleted    []string
	Renamed    []string
	Conflicted []string
}

// clean 报告工作树是否干净（没有任何未提交的改动）。
func (g gitStatus) clean() bool {
	return len(g.Modified)+len(g.Added)+len(g.Deleted)+len(g.Renamed)+len(g.Conflicted) == 0
}

// readGitStatus 读一棵工作树的改动。`--untracked-files=all` 把新目录里的
// 文件逐个列出来——只报一个目录名，用户看不出到底多了什么。
func readGitStatus(ctx context.Context, dir string) (gitStatus, error) {
	out, err := runGit(ctx, 30*time.Second, dir, "status", "--porcelain=v1", "-b", "--untracked-files=all")
	if err != nil {
		return gitStatus{}, fmt.Errorf("git status 失败: %s", gitReason(out, err))
	}
	return parseGitStatus(out), nil
}

// branchLineRe 解 `## main...origin/main [ahead 1, behind 2]` 这一行。
var branchLineRe = regexp.MustCompile(`^## ([^ .]+)(?:\.\.\.(\S+))?(?: \[(.+)\])?$`)

// parseGitStatus 解 porcelain v1 输出。两位状态码 XY：X 是暂存区、
// Y 是工作区，任一为 U（或 AA/DD）即冲突。同一个文件既改又暂存只归一类，
// 因为这里要回答的是「动了哪些文件」，不是「git 内部处于什么状态」。
func parseGitStatus(output string) gitStatus {
	var g gitStatus
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			m := branchLineRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			if m[1] != "HEAD" {
				g.Branch = m[1]
			}
			g.Upstream = m[2]
			for _, part := range strings.Split(m[3], ", ") {
				var n int
				if _, err := fmt.Sscanf(part, "ahead %d", &n); err == nil {
					g.Ahead = n
				}
				if _, err := fmt.Sscanf(part, "behind %d", &n); err == nil {
					g.Behind = n
				}
			}
			continue
		}
		if len(line) < 4 {
			continue
		}
		x, y, path := line[0], line[1], line[3:]
		switch {
		case x == 'U' || y == 'U' || (x == 'A' && y == 'A') || (x == 'D' && y == 'D'):
			g.Conflicted = append(g.Conflicted, path)
		case x == 'R':
			g.Renamed = append(g.Renamed, path)
		case x == '?', x == 'A':
			g.Added = append(g.Added, path)
		case x == 'D' || y == 'D':
			g.Deleted = append(g.Deleted, path)
		default:
			g.Modified = append(g.Modified, path)
		}
	}
	return g
}

// tailLines 取输出末尾几行：git 的失败原因基本都在最后。
func tailLines(output string, n int) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
