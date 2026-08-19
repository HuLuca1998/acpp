package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// 契约：合法名字在 parent 下真实建出目录，返回的 path 能被 ListDirs 看见
// ——调用方（工作目录选择器）建完直接导航进去。
func TestCreateDir_CreatesNavigableDir(t *testing.T) {
	parent := t.TempDir()

	entry, err := CreateDir(OwnerScope(), parent, "acp-workspace")
	if err != nil {
		t.Fatalf("CreateDir: %v", err)
	}
	if entry.Path != filepath.Join(parent, "acp-workspace") {
		t.Fatalf("path = %q, want %q", entry.Path, filepath.Join(parent, "acp-workspace"))
	}

	info, err := os.Stat(entry.Path)
	if err != nil || !info.IsDir() {
		t.Fatalf("created path not a dir: info=%v err=%v", info, err)
	}
	listing, err := ListDirs(OwnerScope(), parent, false, false)
	if err != nil {
		t.Fatalf("ListDirs: %v", err)
	}
	found := false
	for _, d := range listing.Dirs {
		if d.Path == entry.Path {
			found = true
		}
	}
	if !found {
		t.Fatalf("new dir not visible in listing: %+v", listing.Dirs)
	}
}

// 契约：重名冲突返回 ErrInvalid，不静默成功。
func TestCreateDir_DuplicateIsInvalid(t *testing.T) {
	parent := t.TempDir()
	if _, err := CreateDir(OwnerScope(), parent, "projects"); err != nil {
		t.Fatalf("first CreateDir: %v", err)
	}
	if _, err := CreateDir(OwnerScope(), parent, "projects"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate err = %v, want ErrInvalid", err)
	}
}

// 契约：单层目录名之外的输入（逃逸、隐藏、空白、相对 parent）一律 ErrInvalid，
// 且不在磁盘上留下任何东西。
func TestCreateDir_RejectsUnsafeNames(t *testing.T) {
	parent := t.TempDir()
	cases := []struct {
		label  string
		parent string
		name   string
	}{
		{"路径逃逸 ..", parent, ".."},
		{"嵌套路径", parent, "a/b"},
		{"反斜杠", parent, `a\b`},
		{"绝对路径", parent, "/etc/acp-evil"},
		{"隐藏目录", parent, ".config"},
		{"空白", parent, "   "},
		{"相对 parent", "some/relative", "workspace"},
	}
	for _, tc := range cases {
		if _, err := CreateDir(OwnerScope(), tc.parent, tc.name); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", tc.label, err)
		}
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("rejected names left artifacts: %v", entries)
	}
}

// 契约：隐藏项默认不列、showHidden 才给（~/.ssh 这类目录得能导航进去）；
// 文件条目带大小与修改时间——访达式列表靠它们撑起来。
func TestListDirs_HiddenAndMetadata(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	plain, err := ListDirs(OwnerScope(), dir, true, false)
	if err != nil {
		t.Fatalf("ListDirs: %v", err)
	}
	if len(plain.Dirs) != 0 {
		t.Fatalf("默认不该列出隐藏目录: %+v", plain.Dirs)
	}
	if len(plain.Files) != 1 || plain.Files[0].Name != "note.txt" {
		t.Fatalf("files = %+v, want note.txt", plain.Files)
	}
	if plain.Files[0].Size != 5 || plain.Files[0].ModTime == "" {
		t.Errorf("文件元数据缺失: size=%d modTime=%q", plain.Files[0].Size, plain.Files[0].ModTime)
	}

	shown, err := ListDirs(OwnerScope(), dir, true, true)
	if err != nil {
		t.Fatalf("ListDirs(hidden): %v", err)
	}
	if len(shown.Dirs) != 1 || shown.Dirs[0].Name != ".hidden" {
		t.Fatalf("showHidden 应列出隐藏目录: %+v", shown.Dirs)
	}
}
