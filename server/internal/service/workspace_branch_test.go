package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitRepo 造一个有一条提交的真实仓库——分支与 worktree 的行为全在 git
// 自己身上，用假目录测不出任何东西。
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	cmds := [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
	}
	for _, args := range cmds {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "init"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

// 契约：非 git 目录返回 IsRepo=false 而不是错误——会话开在普通目录里是
// 正常用法，底部的分支控件安静消失即可。
func TestWorkspaceGitBranches_NonRepo(t *testing.T) {
	view, err := WorkspaceGitBranches(context.Background(), t.TempDir())
	if err != nil || view.IsRepo {
		t.Fatalf("view = %+v, err = %v; want IsRepo=false without error", view, err)
	}
}

// 契约：分支视图给出当前分支、本地分支清单与工作区清单（主工作区标 Main）。
func TestWorkspaceGitBranches_ReportsCurrentAndWorktrees(t *testing.T) {
	repo := gitRepo(t)
	ctx := context.Background()

	view, err := WorkspaceGitBranches(ctx, repo)
	if err != nil {
		t.Fatalf("WorkspaceGitBranches: %v", err)
	}
	if !view.IsRepo || view.Current != "main" || view.Detached || view.Dirty {
		t.Fatalf("view = %+v, want clean main", view)
	}
	if len(view.Local) != 1 || view.Local[0].Name != "main" || !view.Local[0].Current {
		t.Fatalf("local = %+v, want just main marked current", view.Local)
	}
	if len(view.Worktrees) != 1 || !view.Worktrees[0].Main || !view.Worktrees[0].Current {
		t.Fatalf("worktrees = %+v, want the main worktree marked", view.Worktrees)
	}
}

// 契约：可以新建分支并切过去；有未提交改动时**拒绝切换**——git 会把改动
// 带到新分支上，那对使用者是惊吓，宁可让他先决定提交还是储藏。
func TestWorkspaceGitCheckout(t *testing.T) {
	repo := gitRepo(t)
	ctx := context.Background()

	view, err := WorkspaceGitCheckout(ctx, repo, CheckoutInput{Branch: "feature/x", Create: true})
	if err != nil {
		t.Fatalf("checkout -b: %v", err)
	}
	if view.Current != "feature/x" {
		t.Fatalf("current = %q, want feature/x", view.Current)
	}

	if _, err := WorkspaceGitCheckout(ctx, repo, CheckoutInput{Branch: "main"}); err != nil {
		t.Fatalf("checkout back: %v", err)
	}

	// 脏工作区不给切。
	if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := WorkspaceGitCheckout(ctx, repo, CheckoutInput{Branch: "feature/x"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("dirty checkout err = %v, want ErrInvalid", err)
	}

	// 名字里的注入尝试一律挡在 git 之前。
	for _, branch := range []string{"", "--upload-pack=touch /tmp/x", "a b", "../evil", "a;rm -rf /"} {
		if _, err := WorkspaceGitCheckout(ctx, repo, CheckoutInput{Branch: branch}); !errors.Is(err, ErrInvalid) {
			t.Errorf("checkout(%q) err = %v, want ErrInvalid", branch, err)
		}
	}
}

// 契约：worktree 落在 `<仓库>/worktrees/<名字>`，分支不存在时顺手建出来；
// 建好后它出现在工作区清单里，且占用的分支被标记（git 不允许两处同时
// checkout 同一分支，界面要据此禁掉切换）。
func TestCreateWorktree(t *testing.T) {
	repo := gitRepo(t)
	ctx := context.Background()
	scope := TenantScope(1, repo)

	path, err := CreateWorktree(ctx, scope, repo, WorktreeInput{Name: "feature-a"})
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	if filepath.Base(filepath.Dir(path)) != worktreeDir {
		t.Fatalf("worktree path = %q, want under %s/", path, worktreeDir)
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatalf("worktree dir missing: %v", err)
	}

	view, err := WorkspaceGitBranches(ctx, repo)
	if err != nil {
		t.Fatalf("branches: %v", err)
	}
	if len(view.Worktrees) != 2 {
		t.Fatalf("worktrees = %+v, want main + feature-a", view.Worktrees)
	}
	var occupied bool
	for _, b := range view.Local {
		if b.Name == "feature-a" && b.Worktree != "" {
			occupied = true
		}
	}
	if !occupied {
		t.Fatalf("local = %+v, want feature-a marked as taken by a worktree", view.Local)
	}

	// 重名拒绝，不覆盖已有工作区。
	if _, err := CreateWorktree(ctx, scope, repo, WorktreeInput{Name: "feature-a"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate worktree err = %v, want ErrInvalid", err)
	}
	// 名字要能安全地拼进路径与命令行。
	for _, name := range []string{"", "../escape", "a/b", "-b", "a b"} {
		if _, err := CreateWorktree(ctx, scope, repo, WorktreeInput{Name: name}); !errors.Is(err, ErrInvalid) {
			t.Errorf("CreateWorktree(%q) err = %v, want ErrInvalid", name, err)
		}
	}

	if err := RemoveWorktree(ctx, scope, path); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still there: %v", err)
	}
	// 分支保留：删工作区不等于删掉在上面干过的活。
	view, err = WorkspaceGitBranches(ctx, repo)
	if err != nil {
		t.Fatalf("branches: %v", err)
	}
	var kept bool
	for _, b := range view.Local {
		if b.Name == "feature-a" {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("local = %+v, want feature-a branch kept after worktree removal", view.Local)
	}
}

// 契约：worktree 只能开在自己的工作区里，也只能拆自己工作区里的。
func TestWorktree_ScopeGuarded(t *testing.T) {
	repo := gitRepo(t)
	other := TenantScope(2, t.TempDir())

	if _, err := CreateWorktree(context.Background(), other, repo, WorktreeInput{Name: "x"}); err == nil {
		t.Fatal("CreateWorktree outside own root succeeded, want rejection")
	}
	if err := RemoveWorktree(context.Background(), other, filepath.Join(repo, worktreeDir, "x")); err == nil {
		t.Fatal("RemoveWorktree outside own root succeeded, want rejection")
	}
}
