package discord

import (
	"context"
	"errors"
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

// worktreeResult 是一次工作树准备的结果。
type worktreeResult struct {
	// Dir 是频道要用的工作目录。
	Dir string
	// Branch 是它检出的分支（传空时解析出的默认分支真名）。
	Branch string
	// Base 是这条工作分支切出来的基础分支（展示与 /git 的对比基准）。
	Base string
	// Reused 表示这棵树本来就在，这次只是复用。
	Reused bool
	// RetiredLegacy 非空时，老布局的克隆被挪到了这个路径（见 retireLegacyClone）。
	RetiredLegacy string
}

// worktreeSpec 是准备一棵工作树要的输入。
type worktreeSpec struct {
	CloneURL string
	// Home 是项目目录（`<workRoot>/<组织>/<仓库>`）。
	Home string
	// Branch 是频道自己的工作分支。空串＝自动生成一条不重名的；重绑时把
	// 上次那条传进来即可复用（不会每次 /init 都换一条新分支）。
	Branch string
	// NameHint 是自动生成分支名的素材（用频道名）。
	NameHint string
	// Base 是新分支切出来的基础分支，空串表示仓库默认分支。
	Base string
}

// ensureWorktree 保证 `<home>/.worktree/<目录>` 是这个频道工作分支的一棵树。
//
// 频道**永远工作在自己的分支上**，绝不直接用 base：pre / prod / live 这类
// 分支多半有保护规则，agent 干完活提交推不上去，整条链就断在最后一步。
// base 只是起点；分支名与目录名都由这里生成，且保证不与任何已有分支/已有
// 工作树重名（adr-018）。
//
// 三种建树情形，取舍都在「别弄丢已有的提交」上：
//
//   - 本地已有这条分支（这个频道之前建过）：直接检出，**不**对齐 origin
//     ——上面可能有还没推的提交。
//   - 本地没有、远端有（之前推上去过）：跟踪 origin 上那条。
//   - 都没有：从 origin/<base> 切一条新的。
func ensureWorktree(ctx context.Context, spec worktreeSpec) (worktreeResult, error) {
	var res worktreeResult
	home := spec.Home
	legacy, err := retireLegacyClone(home)
	if err != nil {
		return res, err
	}
	res.RetiredLegacy = legacy

	git := gitHome(home)
	if err := ensureGitHome(ctx, spec.CloneURL, git); err != nil {
		return res, err
	}
	base := spec.Base
	if base == "" {
		if base, err = defaultBranchOf(ctx, git); err != nil {
			return res, err
		}
	}
	// fetch 失败不拦路：本地已有 ref 就还能建树，离线也该能干活。放在生成
	// 分支名之前——重名判断要拿最新的远端分支清单来比。
	if out, ferr := runGit(ctx, fetchTimeout, git, "fetch", "--prune", "origin"); ferr != nil {
		slog.Warn("fetch 失败，用本地已有的 ref 继续", "repo", home, "err", gitReason(out, ferr))
	}

	branch := spec.Branch
	if branch == "" {
		branch = uniqueBranchName(ctx, git, spec.NameHint)
	}
	res.Branch, res.Base = branch, base

	dir := uniqueWorktreeDir(ctx, home, branch)
	res.Dir = dir
	if _, statErr := os.Stat(dir); statErr == nil {
		// uniqueWorktreeDir 只会把「本来就是这条分支的树」这一种情况返回成
		// 已存在的目录，所以走到这里就是复用。
		res.Reused = true
		return res, nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return res, fmt.Errorf("建工作树目录: %w", err)
	}

	var args []string
	switch {
	case hasLocalBranch(ctx, git, branch):
		// 本地已经有这条分支：直接检出。**不**对齐 origin——这是频道自己的
		// 工作分支，上面可能有还没推送的提交，强行对齐就把活丢了。
		args = []string{"worktree", "add", dir, branch}
	case hasRemoteBranch(ctx, git, branch):
		// 远端有（之前推上去过）：跟踪它，接着往下干。
		args = []string{"worktree", "add", "--track", "-b", branch, dir, "origin/" + branch}
	default:
		// 全新分支：从 origin/<base> 切出来。
		start := "origin/" + base
		if !hasRemoteBranch(ctx, git, base) {
			start = base
		}
		args = []string{"worktree", "add", "-b", branch, dir, start}
	}
	if out, err := runGit(ctx, cloneTimeout, git, args...); err != nil {
		_ = os.RemoveAll(dir)
		_, _ = runGit(ctx, 30*time.Second, git, "worktree", "prune")
		return res, fmt.Errorf("git worktree add 失败: %s", gitReason(out, err))
	}
	return res, nil
}

// uniqueBranchName 生成一条不与任何已有分支重名的工作分支名。
//
// 形如 `discord/pp-prod`，撞了就 `-2`、`-3` 往下排。「已有」同时看本地与
// 远端：远端撞上意味着可能撞到别人的分支或受保护分支，本地撞上意味着别的
// 频道已经占着那棵树。
func uniqueBranchName(ctx context.Context, git, hint string) string {
	stem := "discord/" + safeBranchSegment(hint)
	name := stem
	for i := 2; i < 500; i++ {
		if !hasLocalBranch(ctx, git, name) && !hasRemoteBranch(ctx, git, name) {
			return name
		}
		name = fmt.Sprintf("%s-%d", stem, i)
	}
	return name
}

// safeBranchSegment 把频道名压成一段合法的分支名：git 的 ref 名不许有空格、
// `~^:?*[\`、连续点与结尾的点，中文之类的多字节字符 git 收得下但命令行里
// 太难用，一并转成 `-`。
func safeBranchSegment(hint string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '-'
		}
	}, hint)
	safe = strings.Trim(strings.ReplaceAll(safe, "--", "-"), "-.")
	if safe == "" {
		return "channel"
	}
	return trimRunes(safe, 40)
}

// uniqueWorktreeDir 给一条分支挑一个不冲突的工作树目录。
//
// 分支名里的 `/` 会被压成 `-`，两条不同分支因此可能落到同一个目录名上；
// 目录已经被别的分支占着就往后排 `-2`、`-3`。已经是这条分支的树则原样返回
// （那是复用，不是冲突）。
func uniqueWorktreeDir(ctx context.Context, home, branch string) string {
	stem := worktreeDir(home, branch)
	dir := stem
	for i := 2; i < 500; i++ {
		cur, err := currentBranch(ctx, dir)
		switch {
		case os.IsNotExist(dirErr(dir)):
			return dir
		case err == nil && cur == branch:
			return dir
		}
		dir = fmt.Sprintf("%s-%d", stem, i)
	}
	return dir
}

// dirErr 报告目录是否存在（把 Stat 的错误原样带出来给 os.IsNotExist 判）。
func dirErr(dir string) error {
	_, err := os.Stat(dir)
	return err
}

// worktreeSalvage 判断一棵工作树能不能安全删掉，keep=true 时 why 说明为什么
// 要留。判定从严：读不出状态、算不出与 base 的差距，一律当作「有东西」——
// 解绑是个日常动作，误删别人几小时的活远比多占几百兆磁盘严重。
func worktreeSalvage(ctx context.Context, dir, base string) (keep bool, why string) {
	st, err := readGitStatus(ctx, dir, base)
	switch {
	case err != nil:
		return true, "读不出工作树状态"
	case !st.clean():
		return true, "还有未提交的改动"
	case base == "" || st.Base == "":
		return true, "算不出与 base 的差距"
	case st.BaseAhead > 0:
		return true, fmt.Sprintf("有 %d 个提交还没合回 %s", st.BaseAhead, base)
	}
	return false, ""
}

// removeWorktree 删掉一棵工作树连同它那条分支。只在 worktreeSalvage 说可以
// 时调用：这里**不加** --force，git 自己再把一道关——有改动它会拒绝删。
func removeWorktree(ctx context.Context, home, dir, branch string) error {
	git := gitHome(home)
	if out, err := runGit(ctx, time.Minute, git, "worktree", "remove", dir); err != nil {
		return fmt.Errorf("git worktree remove: %s", gitReason(out, err))
	}
	// 分支是为这个频道生成的，树没了也没人用。删不掉不算失败（可能被别处
	// 引用），树已经清掉了，留个日志即可。
	if out, err := runGit(ctx, 30*time.Second, git, "branch", "-D", branch); err != nil {
		slog.Warn("删工作分支失败（工作树已清理）", "branch", branch, "err", gitReason(out, err))
	}
	_, _ = runGit(ctx, 30*time.Second, git, "worktree", "prune")
	return nil
}

// retireLegacyClone 给老布局的克隆让路，返回它被挪到哪（没有就返回空串）。
//
// 老布局把默认分支的克隆直接放在 `<组织>/<仓库>`——正好是新布局的项目
// 容器目录。不挪开的话 .repo 与 .worktree 会长进那棵老工作树里：老频道的
// AI 一 grep 就扫到别的分支的副本，正是这次要根治的问题。
//
// **只重命名不删除**：里面可能有没推送的活，也可能有 .gitignore 掉的
// .env、上传件与构建产物——git status 看不见它们，删了就找不回来了。
func retireLegacyClone(home string) (string, error) {
	st, err := os.Stat(filepath.Join(home, ".git"))
	if err != nil || !st.IsDir() {
		// 新布局的项目目录里没有 .git；工作树的 .git 是文件，也不该走到这。
		return "", nil
	}
	dest := home + "@legacy"
	for i := 2; ; i++ {
		if _, err := os.Stat(dest); errors.Is(err, os.ErrNotExist) {
			break
		}
		dest = fmt.Sprintf("%s@legacy%d", home, i)
	}
	if err := os.Rename(home, dest); err != nil {
		return "", fmt.Errorf("给老克隆让路失败（%s → %s）: %w", home, dest, err)
	}
	slog.Info("老布局克隆已挪开", "from", home, "to", dest)
	return dest, nil
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
	// Base 与它的差距：频道的工作分支多半还没推上去（没有 upstream），
	// 这时「比 base 多几个提交、落后几个」才是有用的那把尺。
	Base                  string
	BaseAhead, BaseBehind int

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
//
// base 非空时顺带算一次与 origin/<base> 的差距：频道工作在自己那条还没推
// 的分支上，跟 base 比才知道这轮干了多少。
func readGitStatus(ctx context.Context, dir, base string) (gitStatus, error) {
	out, err := runGit(ctx, 30*time.Second, dir, "status", "--porcelain=v1", "-b", "--untracked-files=all")
	if err != nil {
		return gitStatus{}, fmt.Errorf("git status 失败: %s", gitReason(out, err))
	}
	st := parseGitStatus(out)
	if base != "" {
		// 算不出来（base 还没 fetch 过之类）就把 Base 留空——展示会跳过那
		// 一行，解绑清理也据此判断「不知道有没有没合回去的提交」，宁可留着。
		if counts, err := runGit(ctx, 30*time.Second, dir,
			"rev-list", "--left-right", "--count", "origin/"+base+"...HEAD"); err == nil {
			st.Base = base
			fmt.Sscanf(strings.TrimSpace(counts), "%d\t%d", &st.BaseBehind, &st.BaseAhead)
		}
	}
	return st, nil
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
