package service

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// workspaceSkip 是文件树永远不展示的目录/文件：没有浏览价值，且 .git
// 展开会把仓库内部结构当成项目文件误导引用。除此之外的所有条目一律
// 展示——gitignore 命中的产物目录（tmp、build 等）往往正是用户要看的。
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

// WorkspaceTree 列出会话工作目录下的文件树，最多两层。除固定黑名单外
// 全量展示，gitignore 命中的条目也照列——树是「磁盘上有什么」的事实视图。
func WorkspaceTree(cwd, path string, depth int) (*TreeListing, error) {
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
	if len(level1) > budget {
		level1, listing.Truncated = level1[:budget], true
	}
	budget -= len(level1)

	if depth >= 2 {
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
			level1[i].Children = children
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

// zipMaxBytes 是打包下载的总量上限。目录打包是「把产出物取走」，不是
// 备份整个磁盘——超了就明确失败，好过让人等一个永远下不完的文件。
const zipMaxBytes int64 = 512 << 20 // 512 MiB

// WorkspaceZip 把一个目录流式打包成 zip 写进 w。
//
// 跳过的东西与文件树看到的一致（固定黑名单 + 隐藏项）：界面上没显示的
// 东西不该悄悄出现在下载包里，`.git` 与依赖目录更是没人想要。符号链接
// 一律跳过，避免打包时绕进循环。
func WorkspaceZip(cwd, path string, w io.Writer) (string, error) {
	target, err := workspacePath(cwd, path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%w: %s is not a directory", ErrInvalid, path)
	}

	name := filepath.Base(target)
	zw := zip.NewWriter(w)
	var total int64

	err = filepath.WalkDir(target, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// 读不了的单个条目跳过：一个权限不足的文件不该让整包失败。
			return nil //nolint:nilerr // 有意吞掉单条目错误
		}
		if p == target {
			return nil
		}
		base := d.Name()
		if workspaceSkip[base] || strings.HasPrefix(base, ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // 符号链接与设备文件不打包
		}

		info, err := d.Info()
		if err != nil {
			return nil //nolint:nilerr // 同上：单条目失败不牵连整包
		}
		total += info.Size()
		if total > zipMaxBytes {
			return fmt.Errorf("%w: directory exceeds %d MiB", ErrInvalid, zipMaxBytes>>20)
		}

		rel, err := filepath.Rel(target, p)
		if err != nil {
			return nil //nolint:nilerr
		}
		entry, err := zw.Create(filepath.ToSlash(filepath.Join(name, rel)))
		if err != nil {
			return err
		}
		file, err := os.Open(p)
		if err != nil {
			return nil //nolint:nilerr
		}
		defer file.Close()
		_, err = io.Copy(entry, file)
		return err
	})
	if err != nil {
		return "", err
	}
	return name, zw.Close()
}

// ——— 表格文件预览 ———
//
// csv/tsv 与 xlsx 摊平成同一个形状：前者是只有一页的特例，后者一个文件
// 多页。前端因此只需要一个表格渲染器，不必为「像 Excel 的东西」写两套。

const (
	// maxTableRows / maxTableCols 是下发上限。表格是拿来「看一眼」的，
	// 十万行摊到浏览器里只会把面板拖垮——超了截断并如实标记。
	maxTableRows = 5000
	maxTableCols = 200
	// maxTableBytes 是可解析的文件大小上限：xlsx 是压缩包，解析要整个
	// 读进内存，没有闸门的话一个几百 MB 的报表能把进程撑爆。
	maxTableBytes = 32 << 20
)

// TableSheet 是表格文件里的一页。
type TableSheet struct {
	Name string     `json:"name"`
	Rows [][]string `json:"rows"`
	// Truncated 表示这一页有内容没下发（行数或列数超限）。
	Truncated bool `json:"truncated,omitempty"`
}

// TableView 是「能摊平成表格」的文件的统一视图。
type TableView struct {
	Path   string       `json:"path"`
	Name   string       `json:"name"`
	Sheets []TableSheet `json:"sheets"`
}

// IsTableFile 报告这个路径能不能走表格预览。前端据此决定拉哪个端点。
func IsTableFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".csv", ".tsv", ".xlsx", ".xlsm", ".xltx":
		return true
	}
	return false
}

// WorkspaceTable 把 csv/tsv/xlsx 解析成表格视图。
func WorkspaceTable(cwd, path string) (*TableView, error) {
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
	if info.Size() > maxTableBytes {
		return nil, fmt.Errorf("%w: 文件超过 %d MB，表格预览撑不住这个体量", ErrInvalid, maxTableBytes>>20)
	}

	view := &TableView{Path: target, Name: filepath.Base(target)}
	switch strings.ToLower(filepath.Ext(target)) {
	case ".csv", ".tsv":
		sheet, err := readDelimited(target)
		if err != nil {
			return nil, err
		}
		view.Sheets = []TableSheet{*sheet}
	case ".xlsx", ".xlsm", ".xltx":
		sheets, err := readWorkbook(target)
		if err != nil {
			return nil, err
		}
		view.Sheets = sheets
	default:
		return nil, fmt.Errorf("%w: %s 不是表格文件", ErrInvalid, filepath.Base(target))
	}
	return view, nil
}

// readDelimited 读 csv/tsv。
func readDelimited(target string) (*TableSheet, error) {
	raw, err := os.ReadFile(target)
	if err != nil {
		return nil, fmt.Errorf("read table: %w", err)
	}
	raw = decodeText(raw)

	reader := csv.NewReader(bytes.NewReader(raw))
	// 不要求每行列数一致：手写的 csv 常有空尾列，为此报错太严苛。
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	if strings.EqualFold(filepath.Ext(target), ".tsv") {
		reader.Comma = '\t'
	}

	sheet := &TableSheet{Name: filepath.Base(target)}
	for len(sheet.Rows) < maxTableRows {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return sheet, nil
		}
		if err != nil {
			// 半路解析失败就把已读到的交出去：看半张表也好过一句报错。
			if len(sheet.Rows) > 0 {
				sheet.Truncated = true
				return sheet, nil
			}
			return nil, fmt.Errorf("%w: %s", ErrInvalid, err)
		}
		sheet.Rows = append(sheet.Rows, clampRow(row, sheet))
	}
	sheet.Truncated = true
	return sheet, nil
}

// readWorkbook 读 xlsx 的每一页。取的是**显示值**（日期、百分比按单元格
// 格式渲染后的样子），不是底层序列号——预览要的是人看到的那张表。
func readWorkbook(target string) ([]TableSheet, error) {
	f, err := excelize.OpenFile(target)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	defer f.Close()

	names := f.GetSheetList()
	sheets := make([]TableSheet, 0, len(names))
	for _, name := range names {
		rows, err := f.GetRows(name)
		if err != nil {
			// 单页坏了不牵连整本：留个空页，其余照常看。
			sheets = append(sheets, TableSheet{Name: name})
			continue
		}
		sheet := TableSheet{Name: name}
		if len(rows) > maxTableRows {
			rows, sheet.Truncated = rows[:maxTableRows], true
		}
		for _, row := range rows {
			sheet.Rows = append(sheet.Rows, clampRow(row, &sheet))
		}
		sheets = append(sheets, sheet)
	}
	return sheets, nil
}

// clampRow 砍掉超宽的列并在页上标记截断。
func clampRow(row []string, sheet *TableSheet) []string {
	if len(row) > maxTableCols {
		sheet.Truncated = true
		return row[:maxTableCols]
	}
	return row
}

// decodeText 把内容归一成 UTF-8：剥 BOM，非法 UTF-8 按 GBK 再试一次。
//
// 中文环境里这是刚需——Excel 导出的 csv 默认就是 GBK，直接当 UTF-8 读
// 会整张表变成问号。判据是「解不出合法 UTF-8」而不是猜编码：真是 UTF-8
// 的文件不会走到这一步。
func decodeText(raw []byte) []byte {
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(raw) {
		return raw
	}
	if decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes(raw); err == nil {
		return decoded
	}
	return raw
}
