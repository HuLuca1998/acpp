package upload

import (
	"acpp/server/internal/service"

	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tenantScope(t *testing.T) service.Scope {
	t.Helper()
	root := t.TempDir()
	return service.TenantScope(1, root)
}

// 契约：同内容不重复写盘。
func TestSaveUploadDedupes(t *testing.T) {
	s := tenantScope(t)

	first, err := SaveUpload(s, "report.csv", strings.NewReader("a,b,c\n1,2,3\n"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Reused {
		t.Error("第一次上传不该算复用")
	}

	second, err := SaveUpload(s, "report.csv", strings.NewReader("a,b,c\n1,2,3\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !second.Reused {
		t.Error("同内容同名的第二次上传该复用已有文件")
	}
	if second.Path != first.Path {
		t.Errorf("复用时路径该一致：%s vs %s", second.Path, first.Path)
	}

	// 内容不同就是另一个文件，哪怕原名一样。
	third, err := SaveUpload(s, "report.csv", strings.NewReader("different"))
	if err != nil {
		t.Fatal(err)
	}
	if third.Reused || third.Path == first.Path {
		t.Error("内容不同不该复用")
	}

	list, err := ListUploads(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("该有 2 个文件，实际 %d", len(list))
	}
}

// 契约：浏览器给的文件名是不可信输入，不能靠它拼出上传目录之外的路径。
func TestSaveUploadRejectsPathEscape(t *testing.T) {
	s := tenantScope(t)
	dir, err := UploadDir(s)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"../../evil.txt", "/etc/passwd", "a/b/c.txt"} {
		got, err := SaveUpload(s, name, strings.NewReader("x"))
		if err != nil {
			continue // 拒绝也是合格的结果
		}
		// 现在的落点是 `<上传目录>/<hash>/<原名>`，所以比对的是「在
		// 上传目录之下」而不是「就在上传目录里」。
		if !strings.HasPrefix(got.Path, dir+string(filepath.Separator)) {
			t.Errorf("%q 落到了上传目录外：%s", name, got.Path)
		}
		if filepath.Base(got.Path) != filepath.Base(name) &&
			filepath.Base(got.Path) != strings.TrimLeft(filepath.Base(name), ".") {
			t.Errorf("%q 的落点文件名不对：%s", name, got.Path)
		}
	}

	// 纯 `..` 与空名字压完之后什么都不剩，必须报错而不是写出一个怪文件。
	for _, name := range []string{"..", "...", "   ", "/"} {
		if _, err := SaveUpload(s, name, strings.NewReader("x")); !errors.Is(err, service.ErrInvalid) {
			t.Errorf("SaveUpload(%q) 应报 service.ErrInvalid，实际 %v", name, err)
		}
	}
}

// 契约：两个租户的上传互不可见——隔离是目录给的，不靠过滤。
func TestUploadsAreScopedByRoot(t *testing.T) {
	a := service.TenantScope(1, t.TempDir())
	b := service.TenantScope(2, t.TempDir())

	if _, err := SaveUpload(a, "mine.txt", strings.NewReader("secret")); err != nil {
		t.Fatal(err)
	}
	list, err := ListUploads(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("另一个租户不该看到别人的上传，实际 %d 条", len(list))
	}
}

func TestDeleteUpload(t *testing.T) {
	s := tenantScope(t)
	saved, err := SaveUpload(s, "tmp.txt", strings.NewReader("bye"))
	if err != nil {
		t.Fatal(err)
	}
	if err := DeleteUpload(s, saved.Hash, saved.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(saved.Path); !os.IsNotExist(err) {
		t.Error("文件该被删掉")
	}
	if err := DeleteUpload(s, saved.Hash, saved.Name); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("删不存在的该报 service.ErrNotFound，实际 %v", err)
	}
}

// 目录里混进来的其他文件不该被当成上传件列出来。
func TestListUploadsIgnoresForeignFiles(t *testing.T) {
	s := tenantScope(t)
	dir, err := UploadDir(s)
	if err != nil {
		t.Fatal(err)
	}
	// 上传目录里混进来的散装文件、以及名字不像 hash 桶的目录，都不该被
	// 当成上传件。
	for _, name := range []string{"README.md", ".hidden"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "notabucket", "a.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	list, err := ListUploads(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("不该认这些文件：%+v", list)
	}
}
