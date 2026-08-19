package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// 契约：租户的路径闸只放行 root 内的已存在路径。上跳、绝对路径逃逸、
// 符号链接指向外部，三种逃法都必须挡住——这是目录隔离的唯一执行点。
func TestScope_GuardPath_BlocksEscapes(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "tenant")
	inside := filepath.Join(root, "project")
	outside := filepath.Join(base, "other")
	for _, dir := range []string{inside, outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	// 从 root 里指向外面的符号链接：解析前看着在 root 内，解析后不在。
	link := filepath.Join(root, "escape-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	scope := TenantScope(1, root)

	if got, err := scope.GuardPath(inside); err != nil || got == "" {
		t.Fatalf("GuardPath(inside) = %q, %v; want the path", got, err)
	}
	if got, err := scope.GuardPath(""); err != nil || got == "" {
		t.Fatalf("GuardPath(\"\") = %q, %v; want root", got, err)
	}

	for _, path := range []string{
		outside,
		filepath.Join(root, ".."),
		filepath.Join(root, "..", "other"),
		link,
	} {
		if _, err := scope.GuardPath(path); !errors.Is(err, ErrForbidden) && !errors.Is(err, ErrInvalid) {
			t.Fatalf("GuardPath(%q) err = %v, want rejection", path, err)
		}
	}
}

// 契约：owner 不受目录限制——本机主人本来就能开终端读整台机器，
// 在界面上假装拦住他只会碍事。
func TestScope_GuardPath_OwnerUnrestricted(t *testing.T) {
	outside := t.TempDir()
	scope := OwnerScope()

	if got, err := scope.GuardPath(outside); err != nil || got != filepath.Clean(outside) {
		t.Fatalf("owner GuardPath = %q, %v; want %q", got, err, outside)
	}
	if _, err := scope.GuardPath("relative/path"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("owner relative path err = %v, want ErrInvalid", err)
	}

	// owner 的 `~` 按 CLI 直觉展开成家目录（私钥路径、文件选择器起点都这么写）。
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	if got, err := scope.GuardPath("~/.ssh"); err != nil || got != filepath.Join(home, ".ssh") {
		t.Fatalf("owner GuardPath(~/.ssh) = %q, %v; want %q", got, err, filepath.Join(home, ".ssh"))
	}
}

// 契约：租户没有家目录语义——`~` 不展开，按相对路径 join 进 root 后
// 因不存在而被拒，绝不能泄出 root 之外。
func TestScope_GuardPath_TenantTildeStaysInRoot(t *testing.T) {
	root := t.TempDir()
	scope := TenantScope(1, root)
	if got, err := scope.GuardPath("~/.ssh"); err == nil {
		t.Fatalf("tenant GuardPath(~/.ssh) = %q, want error", got)
	}
}

// 契约：待创建路径（新建目录、克隆目标）自身可以不存在，但父目录必须在
// root 内——否则要么正常新建被误挡，要么克隆能落到别人家里。
func TestScope_GuardNewPath(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "tenant")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	scope := TenantScope(1, root)

	target := filepath.Join(root, "org", "repo")
	if _, err := scope.GuardNewPath(filepath.Join(root, "org")); err != nil {
		t.Fatalf("GuardNewPath(new dir in root): %v", err)
	}
	// 父目录还不存在时必须拒绝：克隆前得先把中间层建出来。
	if _, err := scope.GuardNewPath(target); err == nil {
		t.Fatal("GuardNewPath with missing parent = nil, want rejection")
	}

	if _, err := scope.GuardNewPath(filepath.Join(base, "elsewhere")); !errors.Is(err, ErrForbidden) {
		t.Fatalf("GuardNewPath(outside) err = %v, want ErrForbidden", err)
	}
	if _, err := scope.GuardNewPath("relative"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("GuardNewPath(relative) err = %v, want ErrInvalid", err)
	}
}
