package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultCwd 是会话工作目录的全局兜底：~/acpp。
// agent 干活的产物不该混进任意目录，给它一个专门的工作区。
func DefaultCwd() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/acpp"
	}
	return filepath.Join(home, "acpp")
}

// DirEntry 是目录浏览器里的一个子目录。
type DirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// DirListing 是一次目录浏览的结果。Files 只在 withFiles 时带上
// （@ 文件引用的选择器要它，工作目录选择器不要）。
type DirListing struct {
	Path   string     `json:"path"`
	Parent string     `json:"parent,omitempty"`
	Dirs   []DirEntry `json:"dirs"`
	Files  []DirEntry `json:"files,omitempty"`
}

// CreateDir 在 parent 下新建一个子目录，供工作目录选择器就地开新目录。
// name 只允许单层目录名：路径分隔符与 "."/".." 一律拒绝（防目录逃逸）；
// 隐藏名也拒绝——列目录不展示隐藏项，建出来看不见只会造成困惑。
func CreateDir(parent, name string) (*DirEntry, error) {
	if !filepath.IsAbs(parent) {
		return nil, fmt.Errorf("%w: parent must be absolute", ErrInvalid)
	}
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return nil, fmt.Errorf("%w: invalid directory name", ErrInvalid)
	}
	if strings.HasPrefix(name, ".") {
		return nil, fmt.Errorf("%w: hidden directory not allowed", ErrInvalid)
	}
	path := filepath.Join(filepath.Clean(parent), name)
	if err := os.Mkdir(path, 0o755); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	return &DirEntry{Name: name, Path: path}, nil
}

// ListDirs 列出指定目录的子目录（withFiles 时连同文件），供前端的
// 目录/文件选择器导航。浏览器拿不到本地路径（File System Access API
// 只给 handle），选择只能由后端代劳。path 为空从家目录开始；隐藏项不列。
func ListDirs(path string, withFiles bool) (*DirListing, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home: %w", err)
		}
		path = home
	}
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%w: path must be absolute", ErrInvalid)
	}
	path = filepath.Clean(path)

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalid, err)
	}

	listing := &DirListing{Path: path, Dirs: []DirEntry{}}
	if parent := filepath.Dir(path); parent != path {
		listing.Parent = parent
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		entry := DirEntry{Name: e.Name(), Path: filepath.Join(path, e.Name())}
		if e.IsDir() {
			listing.Dirs = append(listing.Dirs, entry)
		} else if withFiles {
			listing.Files = append(listing.Files, entry)
		}
	}
	byName := func(list []DirEntry) func(i, j int) bool {
		return func(i, j int) bool {
			return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
		}
	}
	sort.Slice(listing.Dirs, byName(listing.Dirs))
	sort.Slice(listing.Files, byName(listing.Files))
	return listing, nil
}
