package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"acpp/server/internal/config"
)

// DefaultCwd 是工作区根：agent 干活的地方，也是租户 root 的父目录。
//
// 优先取设置里选定的目录，没设过用 ~/acpp。它与**数据目录**（~/.acpp，
// 装 db、转录、技能包）是两回事：让 agent 拿数据目录当工作目录，等于
// 请它往自家数据里乱写。
func DefaultCwd() string {
	if saved := config.SavedWorkspaceDir(); saved != "" {
		return saved
	}
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
// parent 先过 scope 的路径闸，租户建不到自己 root 外面去（adr-007）。
func CreateDir(scope Scope, parent, name string) (*DirEntry, error) {
	if !filepath.IsAbs(parent) {
		return nil, fmt.Errorf("%w: parent must be absolute", ErrInvalid)
	}
	parent, err := scope.GuardPath(parent)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return nil, fmt.Errorf("%w: invalid directory name", ErrInvalid)
	}
	if strings.HasPrefix(name, ".") {
		return nil, fmt.Errorf("%w: hidden directory not allowed", ErrInvalid)
	}
	path := filepath.Join(parent, name)
	if err := os.Mkdir(path, 0o755); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	return &DirEntry{Name: name, Path: path}, nil
}

// ListDirs 列出指定目录的子目录（withFiles 时连同文件），供前端的
// 目录/文件选择器导航。浏览器拿不到本地路径（File System Access API
// 只给 handle），选择只能由后端代劳。隐藏项不列。
//
// path 为空时从 scope 的起点开始：owner 是家目录，租户是自己的 root。
// 租户站在 root 上时不给 Parent——「上一层」按钮不该把人带出自己的地盘
// （后端即使被绕过也会在 GuardPath 拒掉，这里是让界面别显示假入口）。
func ListDirs(scope Scope, path string, withFiles bool) (*DirListing, error) {
	path, err := scope.GuardPath(path)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalid, err)
	}

	listing := &DirListing{Path: path, Dirs: []DirEntry{}}
	if parent := filepath.Dir(path); parent != path {
		if _, err := scope.GuardPath(parent); err == nil {
			listing.Parent = parent
		}
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
