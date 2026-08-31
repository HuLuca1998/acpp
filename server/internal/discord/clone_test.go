package discord

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// 克隆与工作树、git 状态解析的测试（对应 clone.go）。

func TestResolveRepo(t *testing.T) {
	cases := []struct {
		in       string
		name     string
		cloneURL string
		wantErr  bool
	}{
		{in: "BDBGAME2024/pp-game", name: "BDBGAME2024/pp-game", cloneURL: "https://github.com/BDBGAME2024/pp-game.git"},
		{in: "  owner/repo.git ", name: "owner/repo", cloneURL: "https://github.com/owner/repo.git"},
		{in: "https://github.com/org/app.git", name: "org/app", cloneURL: "https://github.com/org/app.git"},
		{in: "git@github.com:org/app.git", name: "org/app", cloneURL: "git@github.com:org/app.git"},
		{in: "", wantErr: true},
		{in: "justaname", wantErr: true},
		{in: "file:///etc/passwd", wantErr: true},
		{in: "../escape/repo", wantErr: true},
		{in: "https://host/../..", wantErr: true},
	}
	for _, c := range cases {
		name, url, err := resolveRepo(c.in)
		if c.wantErr {
			if !errors.Is(err, ErrInvalid) {
				t.Errorf("resolveRepo(%q) err = %v, want ErrInvalid", c.in, err)
			}
			continue
		}
		if err != nil || name != c.name || url != c.cloneURL {
			t.Errorf("resolveRepo(%q) = (%q, %q, %v), want (%q, %q)", c.in, name, url, err, c.name, c.cloneURL)
		}
	}
}

func TestEnsureWorktree(t *testing.T) {
	src := seedRepo(t)
	home := filepath.Join(t.TempDir(), "org", "app")
	ctx := context.Background()

	dir, branch, reused, err := ensureWorktree(ctx, src, home, "")
	if err != nil {
		t.Fatalf("默认分支建树: %v", err)
	}
	if reused {
		t.Error("首次建树不该报复用")
	}
	if branch == "" {
		t.Error("默认分支名应被解析出来回填")
	}
	if _, err := os.Stat(filepath.Join(home, gitDirName, "HEAD")); err != nil {
		t.Errorf("bare git 数据应在 %s: %v", gitDirName, err)
	}
	if dir != filepath.Join(home, worktreeDirName, branch) {
		t.Errorf("工作树落点 = %q", dir)
	}
	if body, err := os.ReadFile(filepath.Join(dir, "who.txt")); err != nil || strings.TrimSpace(string(body)) != branch {
		t.Errorf("默认分支的树内容 = %q, err=%v", body, err)
	}

	devDir, devBranch, _, err := ensureWorktree(ctx, src, home, "dev")
	if err != nil {
		t.Fatalf("dev 建树: %v", err)
	}
	if devBranch != "dev" || devDir == dir {
		t.Fatalf("dev 应是独立的树: branch=%q dir=%q", devBranch, devDir)
	}
	if body, err := os.ReadFile(filepath.Join(devDir, "who.txt")); err != nil || strings.TrimSpace(string(body)) != "dev" {
		t.Errorf("dev 树内容 = %q, err=%v", body, err)
	}

	again, _, reused, err := ensureWorktree(ctx, src, home, "dev")
	if err != nil || !reused || again != devDir {
		t.Errorf("同分支再绑应复用同一棵树: dir=%q reused=%v err=%v", again, reused, err)
	}
}

func seedRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) { gitRun(t, dir, args...) }
	run("init", "--quiet")
	head, err := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		t.Fatalf("读默认分支: %v", err)
	}
	def := strings.TrimSpace(string(head))
	if err := os.WriteFile(filepath.Join(dir, "who.txt"), []byte(def+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "--quiet", "-m", "init")
	run("checkout", "--quiet", "-b", "dev")
	if err := os.WriteFile(filepath.Join(dir, "who.txt"), []byte("dev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("commit", "--quiet", "-am", "dev")
	run("checkout", "--quiet", def)
	return dir
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestReadGitStatus(t *testing.T) {
	dir := seedRepo(t)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(dir, "gone.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "--quiet", "-m", "add gone")

	clean, err := readGitStatus(ctx, dir)
	if err != nil {
		t.Fatalf("readGitStatus: %v", err)
	}
	if !clean.clean() {
		t.Fatalf("刚提交完应是干净的，实际 %+v", clean)
	}
	if clean.Branch == "" {
		t.Error("应报出当前分支")
	}

	if err := os.WriteFile(filepath.Join(dir, "who.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fresh.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "gone.txt")); err != nil {
		t.Fatal(err)
	}

	st, err := readGitStatus(ctx, dir)
	if err != nil {
		t.Fatalf("readGitStatus: %v", err)
	}
	if st.clean() {
		t.Fatal("有改动却报干净")
	}
	if !reflect.DeepEqual(st.Modified, []string{"who.txt"}) {
		t.Errorf("修改 = %v, want [who.txt]", st.Modified)
	}
	if !reflect.DeepEqual(st.Added, []string{"fresh.txt"}) {
		t.Errorf("新增 = %v, want [fresh.txt]", st.Added)
	}
	if !reflect.DeepEqual(st.Deleted, []string{"gone.txt"}) {
		t.Errorf("删除 = %v, want [gone.txt]", st.Deleted)
	}
}

func TestParseGitStatusBranchLine(t *testing.T) {
	g := parseGitStatus("## live...origin/live [ahead 2, behind 5]\nR  old.go -> new.go\nUU conflict.go\n")
	if g.Branch != "live" || g.Upstream != "origin/live" {
		t.Errorf("分支行 = %q / %q", g.Branch, g.Upstream)
	}
	if g.Ahead != 2 || g.Behind != 5 {
		t.Errorf("领先/落后 = %d/%d, want 2/5", g.Ahead, g.Behind)
	}
	if len(g.Renamed) != 1 || len(g.Conflicted) != 1 {
		t.Errorf("重命名/冲突分类错: %+v", g)
	}
	if d := parseGitStatus("## HEAD (no branch)\n"); d.Branch != "" {
		t.Errorf("游离 HEAD 不该报分支名，got %q", d.Branch)
	}
}

func TestSafeBranchDir(t *testing.T) {
	cases := map[string]string{
		"main":       "main",
		"feat/login": "feat-login",
		"..":         "branch",
		"../../etc":  "-..-etc",
		".hidden":    "hidden",
		"":           "branch",
	}
	for in, want := range cases {
		if got := safeBranchDir(in); got != want {
			t.Errorf("safeBranchDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseLsRemote(t *testing.T) {
	out := "ref: refs/heads/main\tHEAD\n" +
		"aaaa\tHEAD\n" +
		"aaaa\trefs/heads/main\n" +
		"bbbb\trefs/heads/feat/login\n" +
		"cccc\trefs/tags/v1.0\n"
	def, branches := parseLsRemote(out)
	if def != "main" {
		t.Errorf("默认分支 = %q, want main", def)
	}
	if len(branches) != 2 || branches[0] != "main" || branches[1] != "feat/login" {
		t.Errorf("分支清单 = %v", branches)
	}
}
