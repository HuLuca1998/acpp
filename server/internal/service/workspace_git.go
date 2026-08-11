package service

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// 工作区 git 数据面（adr-002 M2）：overview 一次返回分支/领先落后/变更
// 文件/未推送 commit，diff 返回 old/new 全文交给前端行级对齐。全部参数
// 走 exec 数组传递（无 shell），路径经 guard 后再拼 `--` 之后。

// GitFileChange 是一条文件级变更。Added/Deleted 为 -1 表示无行数概念（二进制）。
type GitFileChange struct {
	Path    string `json:"path"` // 相对仓库根
	Status  string `json:"status"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
}

// GitCommit 是未推送列表里的一条提交。
type GitCommit struct {
	SHA     string `json:"sha"`
	Short   string `json:"short"`
	Subject string `json:"subject"`
	Author  string `json:"author"`
	Time    int64  `json:"time"`
}

// GitOverview 是 diff 面板与 commit 面板共享的一次性视图。
type GitOverview struct {
	IsRepo   bool            `json:"isRepo"`
	Branch   string          `json:"branch,omitempty"`
	Upstream string          `json:"upstream,omitempty"`
	Ahead    int             `json:"ahead"`
	Behind   int             `json:"behind"`
	Files    []GitFileChange `json:"files"`
	// 未推送提交；无 upstream 时退化为最近 20 条（前端据 Upstream 空标注）。
	Commits []GitCommit `json:"commits"`
}

// GitDiffView 是单文件 diff 的两端全文，行级对齐由前端完成。
type GitDiffView struct {
	Path      string `json:"path"`
	OldText   string `json:"oldText"`
	NewText   string `json:"newText"`
	Binary    bool   `json:"binary,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// GitCommitDetail 是一条提交的文件清单。
type GitCommitDetail struct {
	Commit GitCommit       `json:"commit"`
	Files  []GitFileChange `json:"files"`
}

var shaPattern = regexp.MustCompile(`^[0-9a-fA-F]{4,64}$`)

// WorkspaceGitOverview 汇总会话工作目录的 git 状态。非 git 仓库不是错误，
// 诚实返回 isRepo=false 由前端画空态。
func WorkspaceGitOverview(ctx context.Context, cwd string) (*GitOverview, error) {
	overview := &GitOverview{Files: []GitFileChange{}, Commits: []GitCommit{}}

	branch, err := runGit(ctx, cwd, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return overview, nil
	}
	overview.IsRepo = true
	overview.Branch = strings.TrimSpace(branch)

	if up, err := runGit(ctx, cwd, "rev-parse", "--abbrev-ref", "@{u}"); err == nil {
		overview.Upstream = strings.TrimSpace(up)
	}
	if overview.Upstream != "" {
		if counts, err := runGit(ctx, cwd, "rev-list", "--left-right", "--count", "@{u}...HEAD"); err == nil {
			parts := strings.Fields(counts)
			if len(parts) == 2 {
				overview.Behind, _ = strconv.Atoi(parts[0])
				overview.Ahead, _ = strconv.Atoi(parts[1])
			}
		}
	}

	overview.Files = gitStatusFiles(ctx, cwd)
	overview.Commits = gitUnpushed(ctx, cwd, overview.Upstream)
	return overview, nil
}

// WorkspaceGitDiff 取单文件的 HEAD 版与工作区版全文。文件可能已删除，
// 所以路径只做词法 guard（不解析符号链接）——读取范围与 @ 引用同级，
// 真正的隔离仍靠 runtime 沙箱与 OS。
func WorkspaceGitDiff(ctx context.Context, cwd, path string) (*GitDiffView, error) {
	rel, abs, err := workspaceRelPath(cwd, path)
	if err != nil {
		return nil, err
	}

	view := &GitDiffView{Path: rel}
	// HEAD 里没有（新文件/未跟踪）不是错误，old 就是空。
	if old, err := runGit(ctx, cwd, "show", "HEAD:"+toGitPath(rel)); err == nil {
		view.OldText = old
	}
	if data, err := os.ReadFile(abs); err == nil {
		view.NewText = string(data)
	}
	finishDiffView(view)
	return view, nil
}

// WorkspaceGitCommit 取一条提交的元信息与文件清单；带 path 时改取该文件
// 在这条提交前后的全文。
func WorkspaceGitCommit(ctx context.Context, cwd, sha, path string) (*GitCommitDetail, *GitDiffView, error) {
	if !shaPattern.MatchString(sha) {
		return nil, nil, fmt.Errorf("%w: bad sha", ErrInvalid)
	}

	if path != "" {
		rel, _, err := workspaceRelPath(cwd, path)
		if err != nil {
			return nil, nil, err
		}
		view := &GitDiffView{Path: rel}
		if old, err := runGit(ctx, cwd, "show", sha+"^:"+toGitPath(rel)); err == nil {
			view.OldText = old
		}
		if now, err := runGit(ctx, cwd, "show", sha+":"+toGitPath(rel)); err == nil {
			view.NewText = now
		}
		finishDiffView(view)
		return nil, view, nil
	}

	meta, err := runGit(ctx, cwd, "show", "-s", "--format=%H%x01%h%x01%s%x01%an%x01%ct", sha)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	commit, ok := parseCommitLine(strings.TrimRight(meta, "\n"))
	if !ok {
		return nil, nil, fmt.Errorf("%w: unexpected git output", ErrInvalid)
	}

	detail := &GitCommitDetail{Commit: commit, Files: []GitFileChange{}}
	if out, err := runGit(ctx, cwd, "show", "--numstat", "--format=", "-z", sha); err == nil {
		stats := parseNumstat(out)
		for _, f := range stats.order {
			s := stats.byPath[f]
			detail.Files = append(detail.Files, GitFileChange{Path: f, Status: "M", Added: s[0], Deleted: s[1]})
		}
	}
	return detail, nil, nil
}

// ---- 内部实现 ----

func runGit(ctx context.Context, cwd string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// workspaceRelPath 把请求路径规约成「仓库相对 + 绝对」双形态，词法级
// 防止越出 cwd（不依赖文件存在，删除的文件也能 diff）。
func workspaceRelPath(cwd, path string) (rel string, abs string, err error) {
	if path == "" {
		return "", "", fmt.Errorf("%w: path required", ErrInvalid)
	}
	cwd = filepath.Clean(cwd)
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	abs = filepath.Clean(path)
	rel, err = filepath.Rel(cwd, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("%w: path escapes workspace", ErrInvalid)
	}
	return rel, abs, nil
}

// toGitPath 把 OS 相对路径转成 git 对象路径（正斜杠）。
func toGitPath(rel string) string {
	return filepath.ToSlash(rel)
}

// finishDiffView 补二进制探测与超大截断（两端各 1MB，行边界不追求精确，
// 前端 lineDiff 对超大输入本身还有整删整增退化）。
func finishDiffView(view *GitDiffView) {
	const limit = workspaceMaxFileBytes
	probe := func(s string) bool {
		n := min(len(s), 8192)
		return strings.IndexByte(s[:n], 0) >= 0
	}
	if probe(view.OldText) || probe(view.NewText) {
		view.Binary = true
		view.OldText, view.NewText = "", ""
		return
	}
	if len(view.OldText) > limit {
		view.OldText, view.Truncated = view.OldText[:limit], true
	}
	if len(view.NewText) > limit {
		view.NewText, view.Truncated = view.NewText[:limit], true
	}
}

// gitStatusFiles 解析 `status --porcelain -z`，行数统计并入 numstat；
// untracked 文件现场数行（有上限），比显示"未知"更有用。
func gitStatusFiles(ctx context.Context, cwd string) []GitFileChange {
	out, err := runGit(ctx, cwd, "status", "--porcelain", "-z")
	if err != nil {
		return []GitFileChange{}
	}
	stats := parseNumstat("")
	if numOut, err := runGit(ctx, cwd, "diff", "--numstat", "-z", "HEAD", "--"); err == nil {
		stats = parseNumstat(numOut)
	}

	files := []GitFileChange{}
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		entry := fields[i]
		if len(entry) < 4 {
			continue
		}
		xy, path := entry[:2], entry[3:]
		status := strings.TrimSpace(string(xy[0]))
		if status == "" {
			status = string(xy[1])
		}
		change := GitFileChange{Path: path, Status: status, Added: -1, Deleted: -1}
		if xy == "??" {
			change.Status = "A"
			change.Added = countFileLines(filepath.Join(cwd, path))
			change.Deleted = 0
		} else if s, ok := stats.byPath[path]; ok {
			change.Added, change.Deleted = s[0], s[1]
		}
		files = append(files, change)
		// rename 条目后面跟旧路径，跳过。
		if xy[0] == 'R' || xy[0] == 'C' {
			i++
		}
	}
	return files
}

func gitUnpushed(ctx context.Context, cwd, upstream string) []GitCommit {
	rangeArg := "@{u}..HEAD"
	args := []string{"log", "--format=%H%x01%h%x01%s%x01%an%x01%ct"}
	if upstream == "" {
		args = append(args, "-n", "20")
	} else {
		args = append(args, rangeArg)
	}
	out, err := runGit(ctx, cwd, args...)
	if err != nil {
		return []GitCommit{}
	}
	commits := []GitCommit{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if commit, ok := parseCommitLine(line); ok {
			commits = append(commits, commit)
		}
	}
	return commits
}

func parseCommitLine(line string) (GitCommit, bool) {
	parts := strings.Split(line, "\x01")
	if len(parts) != 5 {
		return GitCommit{}, false
	}
	ts, _ := strconv.ParseInt(parts[4], 10, 64)
	return GitCommit{SHA: parts[0], Short: parts[1], Subject: parts[2], Author: parts[3], Time: ts}, true
}

type numstat struct {
	byPath map[string][2]int
	order  []string
}

// parseNumstat 解析 `--numstat -z`：普通条目 "a\td\tpath\0"；rename 是
// "a\td\t\0old\0new\0" 三段。二进制的 a/d 是 "-"，记 -1。
func parseNumstat(out string) numstat {
	result := numstat{byPath: map[string][2]int{}}
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		entry := fields[i]
		parts := strings.SplitN(entry, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		added, deleted := -1, -1
		if parts[0] != "-" {
			added, _ = strconv.Atoi(parts[0])
		}
		if parts[1] != "-" {
			deleted, _ = strconv.Atoi(parts[1])
		}
		path := parts[2]
		if path == "" && i+2 < len(fields) {
			// rename：取新路径。
			path = fields[i+2]
			i += 2
		}
		if path == "" {
			continue
		}
		result.byPath[path] = [2]int{added, deleted}
		result.order = append(result.order, path)
	}
	return result
}

// countFileLines 数 untracked 文件的行数；二进制或超限返回 -1。
func countFileLines(path string) int {
	data, err := os.ReadFile(path)
	if err != nil || len(data) > workspaceMaxFileBytes {
		return -1
	}
	if bytes.IndexByte(data[:min(len(data), 8192)], 0) >= 0 {
		return -1
	}
	if len(data) == 0 {
		return 0
	}
	n := bytes.Count(data, []byte{'\n'})
	if data[len(data)-1] != '\n' {
		n++
	}
	return n
}
