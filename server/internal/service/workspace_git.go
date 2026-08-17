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
	IsRepo bool `json:"isRepo"`
	// Root 是仓库根的绝对路径。status 给的文件路径相对它，而不是相对会话
	// cwd（cwd 可能是仓库的子目录）——没有它，界面无法把变更对应到文件树
	// 里的具体条目。
	Root     string          `json:"root,omitempty"`
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

	if root, err := runGit(ctx, cwd, "rev-parse", "--show-toplevel"); err == nil {
		overview.Root = strings.TrimSpace(root)
	}

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

// git 面板的历史数据面（adr-002 M4 / adr-007）：提交链路与两个 ref 的对比。
// overview 只管「当前工作区的状态」，这里管「历史与分支之间的关系」。

// maxHistoryLimit 是单次提交列表的上限。链路面板滚动加载，一次几千条既
// 拖慢 git 也没人看得完。
const maxHistoryLimit = 200

// GitHistory 是提交链路面板的一页数据。
type GitHistory struct {
	Commits []GitCommit `json:"commits"`
	// HasMore 表示还能继续往下翻（多取一条判断出来的）。
	HasMore bool `json:"hasMore"`
}

// GitCompare 是两个 ref 的对比结果：head 相对 base 多出的提交与文件变更。
type GitCompare struct {
	Base string `json:"base"`
	Head string `json:"head"`
	// Ahead/Behind 是 head 相对 base 的领先与落后提交数。
	Ahead   int             `json:"ahead"`
	Behind  int             `json:"behind"`
	Commits []GitCommit     `json:"commits"`
	Files   []GitFileChange `json:"files"`
}

// WorkspaceGitHistory 取提交链路。ref 为空时看当前 HEAD；ref 可以是分支、
// 标签或 sha，一律先过 refName 校验再交给 git。
func WorkspaceGitHistory(ctx context.Context, cwd, ref string, limit, offset int) (*GitHistory, error) {
	if _, err := runGit(ctx, cwd, "rev-parse", "--is-inside-work-tree"); err != nil {
		return &GitHistory{Commits: []GitCommit{}}, nil
	}
	if limit <= 0 || limit > maxHistoryLimit {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	args := []string{"log", "--format=%H%x01%h%x01%s%x01%an%x01%ct",
		// 多取一条用来判断「还有没有更多」，不用再跑一次 count。
		fmt.Sprintf("-n%d", limit+1),
		fmt.Sprintf("--skip=%d", offset),
	}
	if ref != "" {
		if err := checkRefName(ref); err != nil {
			return nil, err
		}
		args = append(args, ref)
	}
	args = append(args, "--")

	out, err := runGit(ctx, cwd, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalid, err)
	}

	commits := []GitCommit{}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if commit, ok := parseCommitLine(line); ok {
			commits = append(commits, commit)
		}
	}

	history := &GitHistory{Commits: commits}
	if len(commits) > limit {
		history.Commits = commits[:limit]
		history.HasMore = true
	}
	return history, nil
}

// WorkspaceGitCompare 对比两个 ref：提交清单取 `base..head`（head 独有的），
// 文件变更取三点 diff `base...head`（从共同祖先算起）——这正是「这条分支
// 做了什么」该看的东西，而不是把 base 后来的改动也算进来。
func WorkspaceGitCompare(ctx context.Context, cwd, base, head string) (*GitCompare, error) {
	if err := checkRefName(base); err != nil {
		return nil, err
	}
	if err := checkRefName(head); err != nil {
		return nil, err
	}
	if _, err := runGit(ctx, cwd, "rev-parse", "--is-inside-work-tree"); err != nil {
		return nil, fmt.Errorf("%w: not a git repository", ErrInvalid)
	}

	compare := &GitCompare{
		Base:    base,
		Head:    head,
		Commits: []GitCommit{},
		Files:   []GitFileChange{},
	}

	if counts, err := runGit(ctx, cwd, "rev-list", "--left-right", "--count",
		base+"..."+head, "--"); err == nil {
		fields := strings.Fields(strings.TrimSpace(counts))
		if len(fields) == 2 {
			compare.Behind = atoiSafe(fields[0])
			compare.Ahead = atoiSafe(fields[1])
		}
	}

	out, err := runGit(ctx, cwd, "log", "--format=%H%x01%h%x01%s%x01%an%x01%ct",
		"-n", "200", base+".."+head, "--")
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if commit, ok := parseCommitLine(line); ok {
			compare.Commits = append(compare.Commits, commit)
		}
	}

	stat, err := runGit(ctx, cwd, "diff", "--numstat", "-z", base+"..."+head, "--")
	if err != nil {
		return compare, nil
	}
	stats := parseNumstat(stat)
	for _, path := range stats.order {
		counts := stats.byPath[path]
		compare.Files = append(compare.Files, GitFileChange{
			Path:    path,
			Status:  "M",
			Added:   counts[0],
			Deleted: counts[1],
		})
	}
	return compare, nil
}

// checkRefName 挡住会被 git 当成选项或路径的 ref。ref 直接进命令行数组
// （没有 shell），但 `--upload-pack=...` 这类以 `-` 开头的值仍会被 git
// 自己解释成选项，必须先挡掉。
func checkRefName(ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("%w: ref is required", ErrInvalid)
	}
	if strings.HasPrefix(ref, "-") || strings.Contains(ref, "..") ||
		strings.ContainsAny(ref, " \t\n:?*[\\^~") {
		return fmt.Errorf("%w: invalid ref %q", ErrInvalid, ref)
	}
	return nil
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}
