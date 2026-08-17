package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// commitFile 在仓库里写一个文件并提交，返回这条提交的 subject。
func commitFile(t *testing.T, repo, name, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", message}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// 契约：提交链路分页，HasMore 靠多取一条判断——不为了翻页再跑一次 count。
func TestWorkspaceGitHistory_Pages(t *testing.T) {
	repo := gitRepo(t)
	ctx := context.Background()
	for _, name := range []string{"a", "b", "c"} {
		commitFile(t, repo, name+".txt", name, "add "+name)
	}

	first, err := WorkspaceGitHistory(ctx, repo, "", 2, 0)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(first.Commits) != 2 || !first.HasMore {
		t.Fatalf("first page = %d commits hasMore=%v, want 2 + more", len(first.Commits), first.HasMore)
	}
	if first.Commits[0].Subject != "add c" {
		t.Fatalf("newest = %q, want 'add c'", first.Commits[0].Subject)
	}

	rest, err := WorkspaceGitHistory(ctx, repo, "", 50, 2)
	if err != nil {
		t.Fatalf("history offset: %v", err)
	}
	if rest.HasMore {
		t.Fatalf("last page hasMore = true, want false (got %d commits)", len(rest.Commits))
	}
	if rest.Commits[0].Subject != "add a" {
		t.Fatalf("offset page starts at %q, want 'add a'", rest.Commits[0].Subject)
	}
}

// 契约：非 git 目录返回空链路而不是错误——会话开在普通目录里是正常用法。
func TestWorkspaceGitHistory_NonRepo(t *testing.T) {
	history, err := WorkspaceGitHistory(context.Background(), t.TempDir(), "", 10, 0)
	if err != nil || len(history.Commits) != 0 {
		t.Fatalf("history = %+v, err = %v; want empty without error", history, err)
	}
}

// 契约：分支对比取 head 独有的提交与三点 diff 的文件变更——「这条分支做了
// 什么」不该把 base 后来的改动算进来。
func TestWorkspaceGitCompare(t *testing.T) {
	repo := gitRepo(t)
	ctx := context.Background()

	if _, err := WorkspaceGitCheckout(ctx, repo, CheckoutInput{Branch: "feature", Create: true}); err != nil {
		t.Fatalf("checkout -b: %v", err)
	}
	commitFile(t, repo, "feature.txt", "hello", "feature work")
	if _, err := WorkspaceGitCheckout(ctx, repo, CheckoutInput{Branch: "main"}); err != nil {
		t.Fatalf("checkout main: %v", err)
	}
	// base 上的后续提交不该出现在「feature 做了什么」里。
	commitFile(t, repo, "main.txt", "main", "main work")

	compare, err := WorkspaceGitCompare(ctx, repo, "main", "feature")
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if compare.Ahead != 1 || compare.Behind != 1 {
		t.Fatalf("ahead/behind = %d/%d, want 1/1", compare.Ahead, compare.Behind)
	}
	if len(compare.Commits) != 1 || compare.Commits[0].Subject != "feature work" {
		t.Fatalf("commits = %+v, want just the feature commit", compare.Commits)
	}
	if len(compare.Files) != 1 || compare.Files[0].Path != "feature.txt" {
		t.Fatalf("files = %+v, want only feature.txt (three-dot diff)", compare.Files)
	}
}

// 契约：ref 会被 git 自己解释成选项，`-` 开头与含 `..` 的一律挡在调用之前。
func TestWorkspaceGitCompare_RejectsBadRefs(t *testing.T) {
	repo := gitRepo(t)
	ctx := context.Background()

	for _, ref := range []string{"", "-upload-pack=x", "--output=/tmp/x", "a..b", "a b", "re*f"} {
		if _, err := WorkspaceGitCompare(ctx, repo, "main", ref); !errors.Is(err, ErrInvalid) {
			t.Errorf("compare(head=%q) err = %v, want ErrInvalid", ref, err)
		}
		if _, err := WorkspaceGitHistory(ctx, repo, ref, 10, 0); ref != "" && !errors.Is(err, ErrInvalid) {
			t.Errorf("history(ref=%q) err = %v, want ErrInvalid", ref, err)
		}
	}
}
