package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildGitRepo 造一个有两条提交与工作区变更的真实仓库：
//
//	commit1: a.txt ("one\ntwo\n")
//	commit2: a.txt 改第二行、b.txt 新增
//	工作区:  a.txt 再改、c.txt untracked
func buildGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	cwd := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v failed: %v: %s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(cwd, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("init", "-q")
	write("a.txt", "one\ntwo\n")
	git("add", ".")
	git("commit", "-q", "-m", "first")
	write("a.txt", "one\nTWO\n")
	write("b.txt", "bee\n")
	git("add", ".")
	git("commit", "-q", "-m", "second")
	write("a.txt", "one\nTWO\nthree\n")
	write("c.txt", "cee\ncee\n")
	return cwd
}

func TestWorkspaceGitOverview(t *testing.T) {
	cwd := buildGitRepo(t)

	overview, err := WorkspaceGitOverview(context.Background(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	if !overview.IsRepo {
		t.Fatal("should detect repo")
	}
	if overview.Upstream != "" {
		t.Fatalf("no upstream expected, got %q", overview.Upstream)
	}

	byPath := map[string]GitFileChange{}
	for _, f := range overview.Files {
		byPath[f.Path] = f
	}
	a, ok := byPath["a.txt"]
	if !ok || a.Status != "M" || a.Added != 1 || a.Deleted != 0 {
		t.Fatalf("a.txt change wrong: %+v (ok=%v)", a, ok)
	}
	c, ok := byPath["c.txt"]
	if !ok || c.Status != "A" || c.Added != 2 {
		t.Fatalf("untracked c.txt wrong: %+v (ok=%v)", c, ok)
	}

	// 无 upstream 退化为最近提交列表，第一条是 second。
	if len(overview.Commits) != 2 || overview.Commits[0].Subject != "second" {
		t.Fatalf("commits wrong: %+v", overview.Commits)
	}
	if overview.Commits[0].Short == "" || overview.Commits[0].Time == 0 {
		t.Fatalf("commit meta incomplete: %+v", overview.Commits[0])
	}
}

func TestWorkspaceGitOverviewNotRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	overview, err := WorkspaceGitOverview(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if overview.IsRepo {
		t.Fatal("bare tempdir must not be a repo")
	}
}

func TestWorkspaceGitDiff(t *testing.T) {
	cwd := buildGitRepo(t)

	t.Run("修改文件：HEAD 版对工作区版", func(t *testing.T) {
		view, err := WorkspaceGitDiff(context.Background(), cwd, "a.txt")
		if err != nil {
			t.Fatal(err)
		}
		if view.OldText != "one\nTWO\n" || view.NewText != "one\nTWO\nthree\n" {
			t.Fatalf("diff texts wrong: %+v", view)
		}
	})

	t.Run("untracked：old 为空", func(t *testing.T) {
		view, err := WorkspaceGitDiff(context.Background(), cwd, "c.txt")
		if err != nil {
			t.Fatal(err)
		}
		if view.OldText != "" || view.NewText != "cee\ncee\n" {
			t.Fatalf("untracked diff wrong: %+v", view)
		}
	})

	t.Run("越界拒绝", func(t *testing.T) {
		if _, err := WorkspaceGitDiff(context.Background(), cwd, "../x"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("want ErrInvalid, got %v", err)
		}
	})
}

func TestWorkspaceGitCommit(t *testing.T) {
	cwd := buildGitRepo(t)
	overview, err := WorkspaceGitOverview(context.Background(), cwd)
	if err != nil || len(overview.Commits) == 0 {
		t.Fatalf("need commits: %v", err)
	}
	second := overview.Commits[0]

	t.Run("详情带文件清单", func(t *testing.T) {
		detail, diff, err := WorkspaceGitCommit(context.Background(), cwd, second.SHA, "")
		if err != nil || diff != nil {
			t.Fatalf("unexpected: %v %v", err, diff)
		}
		if detail.Commit.Subject != "second" {
			t.Fatalf("meta wrong: %+v", detail.Commit)
		}
		paths := []string{}
		for _, f := range detail.Files {
			paths = append(paths, f.Path)
		}
		if strings.Join(paths, ",") != "a.txt,b.txt" {
			t.Fatalf("files wrong: %v", paths)
		}
	})

	t.Run("单文件前后全文", func(t *testing.T) {
		_, diff, err := WorkspaceGitCommit(context.Background(), cwd, second.SHA, "a.txt")
		if err != nil || diff == nil {
			t.Fatalf("unexpected: %v", err)
		}
		if diff.OldText != "one\ntwo\n" || diff.NewText != "one\nTWO\n" {
			t.Fatalf("commit diff wrong: %+v", diff)
		}
	})

	t.Run("坏 sha 拒绝", func(t *testing.T) {
		if _, _, err := WorkspaceGitCommit(context.Background(), cwd, "$(rm)", ""); !errors.Is(err, ErrInvalid) {
			t.Fatalf("want ErrInvalid, got %v", err)
		}
	})
}
