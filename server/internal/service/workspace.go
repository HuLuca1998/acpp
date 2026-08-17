package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// workspaceSkip 是文件树永远不展示的目录/文件：没有浏览价值，且 .git
// 展开会把仓库内部结构当成项目文件误导引用。其余垃圾目录（dist、build
// 等）交给 gitignore 过滤——项目自己最清楚什么该忽略。
var workspaceSkip = map[string]bool{
	".git":         true,
	"node_modules": true,
	".DS_Store":    true,
}

const (
	// workspaceMaxEntries 限制一次树请求的总节点数：超大仓库把两层展开
	// 也可能拉出上万节点，砍在后端并带 truncated 标记比前端硬扛诚实。
	workspaceMaxEntries = 2000
	// workspaceMaxDepth：初始两层是产品决策（adr-002 §2.3），更深由前端逐层懒加载。
	workspaceMaxDepth = 2
	// workspaceMaxFileBytes 是文件预览内容上限，超出截断并标记。
	workspaceMaxFileBytes = 1 << 20
)

// TreeEntry 是工作区文件树的一个节点。
type TreeEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Kind string `json:"kind"` // dir | file
	Size int64  `json:"size,omitempty"`
	// Listed 表示该目录本次已展开（Children 可信，空就是真的空）；
	// false 的目录由前端展开时再请求。
	Listed   bool        `json:"listed,omitempty"`
	Children []TreeEntry `json:"children,omitempty"`
}

// TreeListing 是一次文件树请求的结果。Root 是 canonical 化后的实际根。
type TreeListing struct {
	Root      string      `json:"root"`
	Entries   []TreeEntry `json:"entries"`
	Truncated bool        `json:"truncated,omitempty"`
}

// WorkspaceFileView 是文件预览内容。Binary 时不带 Content。
type WorkspaceFileView struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated,omitempty"`
	Binary    bool   `json:"binary,omitempty"`
}

// WorkspaceTree 列出会话工作目录下的文件树，最多两层。gitignore 命中的
// 条目被过滤（git 不可用时退化为全量展示——过滤是体验优化不是安全边界）。
func WorkspaceTree(ctx context.Context, cwd, path string, depth int) (*TreeListing, error) {
	if depth < 1 {
		depth = 1
	}
	if depth > workspaceMaxDepth {
		depth = workspaceMaxDepth
	}
	root, err := workspacePath(cwd, path)
	if err != nil {
		return nil, err
	}

	listing := &TreeListing{Root: root, Entries: []TreeEntry{}}
	budget := workspaceMaxEntries

	level1, err := listWorkspaceDir(root)
	if err != nil {
		return nil, err
	}
	level1 = dropIgnored(ctx, cwd, level1)
	if len(level1) > budget {
		level1, listing.Truncated = level1[:budget], true
	}
	budget -= len(level1)

	if depth >= 2 {
		// 先列全部子目录再一次性 check-ignore：exec 次数随深度恒定（2 次），
		// 不随目录数增长。
		type sub struct {
			idx      int
			children []TreeEntry
		}
		var subs []sub
		var paths []string
		for i := range level1 {
			if level1[i].Kind != "dir" {
				continue
			}
			if budget <= 0 {
				listing.Truncated = true
				break
			}
			children, err := listWorkspaceDir(level1[i].Path)
			if err != nil {
				// 无权限等个别目录失败不整树报错：保持未展开，前端点击时再报。
				continue
			}
			level1[i].Listed = true
			if len(children) > budget {
				children, listing.Truncated = children[:budget], true
			}
			budget -= len(children)
			subs = append(subs, sub{i, children})
			for _, c := range children {
				paths = append(paths, c.Path)
			}
		}
		ignored := gitIgnored(ctx, cwd, paths)
		for _, s := range subs {
			kept := make([]TreeEntry, 0, len(s.children))
			for _, c := range s.children {
				if !ignored[c.Path] {
					kept = append(kept, c)
				}
			}
			level1[s.idx].Children = kept
		}
	}

	listing.Entries = level1
	return listing, nil
}

// WorkspaceFile 读取会话工作目录内的文件内容供预览。
func WorkspaceFile(cwd, path string) (*WorkspaceFileView, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: path required", ErrInvalid)
	}
	target, err := workspacePath(cwd, path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%w: not a file", ErrInvalid)
	}

	view := &WorkspaceFileView{Path: target, Name: filepath.Base(target), Size: info.Size()}
	f, err := os.Open(target)
	if err != nil {
		return nil, fmt.Errorf("open workspace file: %w", err)
	}
	defer f.Close()

	buf := make([]byte, workspaceMaxFileBytes+1)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("read workspace file: %w", err)
	}
	data := buf[:n]
	if n > workspaceMaxFileBytes {
		data, view.Truncated = data[:workspaceMaxFileBytes], true
		// 截断可能落在多字节字符中间，回退到完整 rune 边界。
		for len(data) > 0 && !utf8.RuneStart(data[len(data)-1]) {
			data = data[:len(data)-1]
		}
		if len(data) > 0 {
			data = data[:len(data)-1]
		}
	}

	probe := data
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	if bytes.IndexByte(probe, 0) >= 0 {
		view.Binary = true
		return view, nil
	}
	view.Content = string(data)
	return view, nil
}

// WorkspaceFilePath 解析出一个可以直接下发的文件绝对路径。
//
// 与 WorkspaceFile 的区别：那个是给预览用的（文本、截断、二进制只标记），
// 这个是给下载用的——原样、不截断、二进制也照给。路径闸是同一道。
func WorkspaceFilePath(cwd, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("%w: path required", ErrInvalid)
	}
	target, err := workspacePath(cwd, path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	if info.IsDir() {
		// 目录要下载得先打包，那是另一件事；这里诚实拒绝而不是给个空文件。
		return "", fmt.Errorf("%w: %s is a directory", ErrInvalid, path)
	}
	return target, nil
}

// workspacePath 把请求路径解析成 canonical 绝对路径，并强制落在会话 cwd 内。
// 与 acp fs 代理同款姿态：符号链接先解析再比对，防止链接逃逸。
func workspacePath(cwd, path string) (string, error) {
	canonCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", fmt.Errorf("%w: cwd unavailable: %s", ErrInvalid, err)
	}
	if path == "" {
		return canonCwd, nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(canonCwd, path)
	}
	canon, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	rel, err := filepath.Rel(canonCwd, canon)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: path escapes workspace", ErrInvalid)
	}
	return canon, nil
}

// listWorkspaceDir 列单个目录：跳过固定黑名单，目录在前、名字不分大小写排序。
// 符号链接一律当文件（不展开），避免循环引用。
func listWorkspaceDir(dir string) ([]TreeEntry, error) {
	raw, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	entries := make([]TreeEntry, 0, len(raw))
	for _, e := range raw {
		if workspaceSkip[e.Name()] {
			continue
		}
		entry := TreeEntry{Name: e.Name(), Path: filepath.Join(dir, e.Name())}
		if e.IsDir() {
			entry.Kind = "dir"
		} else {
			entry.Kind = "file"
			if info, err := e.Info(); err == nil {
				entry.Size = info.Size()
			}
		}
		entries = append(entries, entry)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind == "dir"
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, nil
}

// dropIgnored 过滤掉 gitignore 命中的条目。
func dropIgnored(ctx context.Context, cwd string, entries []TreeEntry) []TreeEntry {
	paths := make([]string, len(entries))
	for i, e := range entries {
		paths[i] = e.Path
	}
	ignored := gitIgnored(ctx, cwd, paths)
	if len(ignored) == 0 {
		return entries
	}
	kept := make([]TreeEntry, 0, len(entries))
	for _, e := range entries {
		if !ignored[e.Path] {
			kept = append(kept, e)
		}
	}
	return kept
}

// gitIgnored 批量问 git 哪些路径被 ignore。git 不存在、cwd 不是仓库或
// 命令失败时返回空集：过滤只是体验优化，缺了就全量展示，不报错。
func gitIgnored(ctx context.Context, cwd string, paths []string) map[string]bool {
	if len(paths) == 0 {
		return nil
	}
	cmd := exec.CommandContext(ctx, "git", "-C", cwd, "check-ignore", "--stdin", "-z")
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\x00") + "\x00")
	out, err := cmd.Output()
	if err != nil {
		// exit 1 = 没有任何命中，是正常结果；其余错误一律视作无过滤。
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 1 {
			return nil
		}
	}
	ignored := make(map[string]bool)
	for p := range bytes.SplitSeq(out, []byte{0}) {
		if len(p) > 0 {
			ignored[string(p)] = true
		}
	}
	return ignored
}

// DirReferenceListing 生成文件夹引用嵌入 prompt 的两层目录清单：给 agent
// 一张地图并提示自行读取，避免把整个目录的内容灌进上下文（adr-002 §5.3）。
func DirReferenceListing(path string) string {
	const maxEntries = 200
	var b strings.Builder
	fmt.Fprintf(&b, "Directory listing (2 levels) of %s:\n", path)

	level1, err := listWorkspaceDir(path)
	if err != nil {
		return b.String() + "(unreadable)\n"
	}
	count := 0
	for _, e := range level1 {
		if count >= maxEntries {
			b.WriteString("…\n")
			break
		}
		if e.Kind == "dir" {
			fmt.Fprintf(&b, "%s/\n", e.Name)
			count++
			children, err := listWorkspaceDir(e.Path)
			if err != nil {
				continue
			}
			for _, c := range children {
				if count >= maxEntries {
					break
				}
				suffix := ""
				if c.Kind == "dir" {
					suffix = "/"
				}
				fmt.Fprintf(&b, "  %s%s\n", c.Name, suffix)
				count++
			}
		} else {
			fmt.Fprintf(&b, "%s\n", e.Name)
			count++
		}
	}
	b.WriteString("\nRead individual files as needed for their contents.\n")
	return b.String()
}
