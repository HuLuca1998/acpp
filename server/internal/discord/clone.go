package discord

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"acpp/server/internal/gitrepo"
)

// cloneTimeout 压在 interaction token 的 15 分钟时效之内：超过它连
// 「失败了」都没法回写到频道里，不如果断掐掉让用户看到原因。
const cloneTimeout = 12 * time.Minute

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

// ensureWorkdir 保证 dir 是 cloneURL 的一个克隆（branch 非空时克隆并检出
// 该分支）：已经是 git 仓库就直接复用（「克隆过就继续」），否则现场克隆。
// 失败把半个目录收干净。
func ensureWorkdir(ctx context.Context, cloneURL, branch, dir string) (reused bool, err error) {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return true, nil
	}
	if _, err := os.Stat(dir); err == nil {
		return false, fmt.Errorf("目录已存在但不是 git 仓库: %s", dir)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return false, fmt.Errorf("建工作根: %w", err)
	}

	cctx, cancel := context.WithTimeout(ctx, cloneTimeout)
	defer cancel()
	args := []string{"clone"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, cloneURL, dir)
	cmd := exec.CommandContext(cctx, "git", args...)
	cmd.Env = append(os.Environ(),
		// 凭证助手照常用；只是不要挂在终端提示上等一个永远不会来的输入。
		"GIT_TERMINAL_PROMPT=0",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(dir)
		reason := tailLines(string(output), 6)
		if reason == "" {
			reason = err.Error()
		}
		return false, fmt.Errorf("git clone 失败: %s", reason)
	}
	return false, nil
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

// workdirName 是仓库在工作根下的相对落点：默认分支就是 `<组织>/<仓库>`，
// 指定分支加 `@<分支>` 后缀——同一仓库多个频道可能各用各的分支，共用一个
// 检出会互相拽着走。
func workdirName(repo, branch string) string {
	if branch == "" {
		return repo
	}
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			return r
		default:
			return '-'
		}
	}, branch)
	return repo + "@" + safe
}

// tailLines 取输出末尾几行：git 的失败原因基本都在最后。
func tailLines(output string, n int) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
