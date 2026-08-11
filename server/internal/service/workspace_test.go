package service

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildWorkspace 造一个真实目录结构：
//
//	cwd/
//	├── .gitignore        ("ignored.txt\n")
//	├── c.txt
//	├── a/
//	│   ├── b/
//	│   │   └── deep.txt   (第三层，depth=2 不应展开)
//	│   └── x.txt
//	├── ignored.txt        (gitignore 命中)
//	├── node_modules/junk.js  (固定黑名单)
//	└── .git/…             (git init 产生，固定黑名单)
func buildWorkspace(t *testing.T) string {
	t.Helper()
	cwd := t.TempDir()
	for _, dir := range []string{"a/b", "node_modules"} {
		if err := os.MkdirAll(filepath.Join(cwd, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"c.txt":                "hello",
		"a/x.txt":              "x",
		"a/b/deep.txt":         "deep",
		"ignored.txt":          "junk",
		"node_modules/junk.js": "junk",
		".gitignore":           "ignored.txt\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(cwd, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return cwd
}

// gitInit 在目录里建仓库；本机没有 git 时跳过该测试。
func gitInit(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init failed: %v: %s", err, out)
	}
}

func entryNames(entries []TreeEntry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return names
}

func findEntry(t *testing.T, entries []TreeEntry, name string) TreeEntry {
	t.Helper()
	for _, e := range entries {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("entry %q not found in %v", name, entryNames(entries))
	return TreeEntry{}
}

func TestWorkspaceTreeDepthAndFiltering(t *testing.T) {
	cwd := buildWorkspace(t)
	gitInit(t, cwd)

	listing, err := WorkspaceTree(context.Background(), cwd, "", 2)
	if err != nil {
		t.Fatal(err)
	}

	// 黑名单与 gitignore 都不该出现；目录排在文件前。
	got := entryNames(listing.Entries)
	want := []string{"a", ".gitignore", "c.txt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("level1 = %v, want %v", got, want)
	}

	a := findEntry(t, listing.Entries, "a")
	if !a.Listed {
		t.Fatal("dir a should be listed at depth 2")
	}
	if names := entryNames(a.Children); strings.Join(names, ",") != "b,x.txt" {
		t.Fatalf("a children = %v, want [b x.txt]", names)
	}
	b := findEntry(t, a.Children, "b")
	if b.Listed || b.Children != nil {
		t.Fatal("depth-2 dir b must stay unlisted (lazy)")
	}

	// 第二层单独请求：以 a 为根再取一层。
	sub, err := WorkspaceTree(context.Background(), cwd, a.Path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if names := entryNames(sub.Entries); strings.Join(names, ",") != "b,x.txt" {
		t.Fatalf("subtree = %v", names)
	}
}

func TestWorkspaceTreeGuard(t *testing.T) {
	cwd := buildWorkspace(t)
	outside := t.TempDir()

	cases := []struct {
		name string
		path string
	}{
		{"绝对路径越界", outside},
		{"相对路径逃逸", "../"},
		{"点点穿越", filepath.Join(cwd, "a", "..", "..")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := WorkspaceTree(context.Background(), cwd, tc.path, 1); !errors.Is(err, ErrInvalid) {
				t.Fatalf("path %q: want ErrInvalid, got %v", tc.path, err)
			}
		})
	}
}

func TestWorkspaceFile(t *testing.T) {
	cwd := buildWorkspace(t)

	t.Run("普通文本", func(t *testing.T) {
		view, err := WorkspaceFile(cwd, filepath.Join(cwd, "c.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if view.Content != "hello" || view.Name != "c.txt" || view.Size != 5 {
			t.Fatalf("unexpected view: %+v", view)
		}
		if view.Binary || view.Truncated {
			t.Fatalf("flags should be clear: %+v", view)
		}
	})

	t.Run("超限截断", func(t *testing.T) {
		big := filepath.Join(cwd, "big.txt")
		if err := os.WriteFile(big, bytes.Repeat([]byte("a"), workspaceMaxFileBytes+100), 0o644); err != nil {
			t.Fatal(err)
		}
		view, err := WorkspaceFile(cwd, big)
		if err != nil {
			t.Fatal(err)
		}
		if !view.Truncated || len(view.Content) > workspaceMaxFileBytes {
			t.Fatalf("want truncated content ≤ cap, got %d truncated=%v", len(view.Content), view.Truncated)
		}
	})

	t.Run("二进制不带内容", func(t *testing.T) {
		bin := filepath.Join(cwd, "bin.dat")
		if err := os.WriteFile(bin, []byte{1, 0, 2, 3}, 0o644); err != nil {
			t.Fatal(err)
		}
		view, err := WorkspaceFile(cwd, bin)
		if err != nil {
			t.Fatal(err)
		}
		if !view.Binary || view.Content != "" {
			t.Fatalf("want binary without content, got %+v", view)
		}
	})

	t.Run("目录报错", func(t *testing.T) {
		if _, err := WorkspaceFile(cwd, filepath.Join(cwd, "a")); !errors.Is(err, ErrInvalid) {
			t.Fatalf("want ErrInvalid, got %v", err)
		}
	})

	t.Run("越界报错", func(t *testing.T) {
		if _, err := WorkspaceFile(cwd, "/etc/hosts"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("want ErrInvalid, got %v", err)
		}
	})

	t.Run("空路径报错", func(t *testing.T) {
		if _, err := WorkspaceFile(cwd, ""); !errors.Is(err, ErrInvalid) {
			t.Fatalf("want ErrInvalid, got %v", err)
		}
	})
}
