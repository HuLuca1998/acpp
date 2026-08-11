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

// DirListing 是一次目录浏览的结果。
type DirListing struct {
	Path   string     `json:"path"`
	Parent string     `json:"parent,omitempty"`
	Dirs   []DirEntry `json:"dirs"`
}

// ListDirs 列出指定目录的子目录，供前端的工作目录选择器导航。
// 浏览器拿不到本地文件夹的绝对路径（File System Access API 只给 handle），
// 目录选择只能由后端代劳。path 为空从家目录开始；隐藏目录不列。
func ListDirs(path string) (*DirListing, error) {
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
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		listing.Dirs = append(listing.Dirs, DirEntry{
			Name: e.Name(),
			Path: filepath.Join(path, e.Name()),
		})
	}
	sort.Slice(listing.Dirs, func(i, j int) bool {
		return strings.ToLower(listing.Dirs[i].Name) < strings.ToLower(listing.Dirs[j].Name)
	})
	return listing, nil
}
