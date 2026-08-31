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

// 契约：一个仓库只克隆一份 bare git 数据（.repo），频道工作在**自己**的
// 分支上（从 base 切出、自动命名且不重名），每条分支一棵 .worktree 树。
// 直接用 base 不行——pre/prod 那类分支有保护规则，agent 提交推不上去。
func TestEnsureWorktree(t *testing.T) {
	src := seedRepo(t)
	home := filepath.Join(t.TempDir(), "org", "app")
	ctx := context.Background()

	first, err := ensureWorktree(ctx, worktreeSpec{CloneURL: src, Home: home, NameHint: "pp-prod", Base: "dev"})
	if err != nil {
		t.Fatalf("建树: %v", err)
	}
	if first.Branch != "discord/pp-prod" {
		t.Errorf("自动分支名 = %q, want discord/pp-prod", first.Branch)
	}
	if first.Base != "dev" {
		t.Errorf("base = %q, want dev", first.Base)
	}
	if first.Reused {
		t.Error("首次建树不该报复用")
	}
	if _, err := os.Stat(filepath.Join(home, gitDirName, "HEAD")); err != nil {
		t.Errorf("bare git 数据应在 %s: %v", gitDirName, err)
	}
	// 新分支的内容来自 base（dev 分支的 who.txt 写着 dev）。
	if body, err := os.ReadFile(filepath.Join(first.Dir, "who.txt")); err != nil || strings.TrimSpace(string(body)) != "dev" {
		t.Errorf("工作树内容 = %q, err=%v（应等于 base 的内容）", body, err)
	}
	if cur, err := currentBranch(ctx, first.Dir); err != nil || cur != "discord/pp-prod" {
		t.Errorf("检出的分支 = %q, err=%v", cur, err)
	}

	// 另一个频道：分支名与目录都不能撞上。
	second, err := ensureWorktree(ctx, worktreeSpec{CloneURL: src, Home: home, NameHint: "pp-pre", Base: "dev"})
	if err != nil {
		t.Fatalf("第二个频道建树: %v", err)
	}
	if second.Branch == first.Branch || second.Dir == first.Dir {
		t.Fatalf("两个频道撞了: %+v / %+v", first, second)
	}

	// 重绑：把上次那条分支传回来就该复用同一棵树，不新建。
	again, err := ensureWorktree(ctx, worktreeSpec{
		CloneURL: src, Home: home, Branch: first.Branch, NameHint: "pp-prod", Base: "dev",
	})
	if err != nil || !again.Reused || again.Dir != first.Dir {
		t.Errorf("重绑应复用同一棵树: %+v err=%v", again, err)
	}
}

// 契约：自动生成的分支名不能撞上远端已有的分支——撞上就可能落到受保护
// 分支或别人的分支上。
func TestUniqueBranchNameAvoidsRemote(t *testing.T) {
	src := seedRepo(t)
	gitRun(t, src, "branch", "discord/pp-prod")
	home := filepath.Join(t.TempDir(), "org", "app")

	res, err := ensureWorktree(context.Background(), worktreeSpec{
		CloneURL: src, Home: home, NameHint: "pp-prod", Base: "dev",
	})
	if err != nil {
		t.Fatalf("建树: %v", err)
	}
	if res.Branch != "discord/pp-prod-2" {
		t.Errorf("分支名 = %q, want discord/pp-prod-2（远端已占用原名）", res.Branch)
	}
}

// 契约：频道名里的空格、大小写、斜杠都要压成合法好用的分支名段。
func TestSafeBranchSegment(t *testing.T) {
	cases := map[string]string{
		"pp-prod": "pp-prod",
		"PP Prod": "pp-prod",
		"生产环境":    "channel",
		"":        "channel",
		"a/b:c":   "a-b-c",
		"...":     "channel",
	}
	for in, want := range cases {
		if got := safeBranchSegment(in); got != want {
			t.Errorf("safeBranchSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

// 契约：解绑清理从严。有未提交的改动、或有还没合回 base 的提交，工作树就
// 必须留着——误删几小时的活远比多占几百兆磁盘严重；两样都没有才连树带分支
// 删掉（那时它就是一份能从 base 随时重建的副本）。
func TestWorktreeSalvageAndRemove(t *testing.T) {
	src := seedRepo(t)
	home := filepath.Join(t.TempDir(), "org", "app")
	ctx := context.Background()

	res, err := ensureWorktree(ctx, worktreeSpec{CloneURL: src, Home: home, NameHint: "ch", Base: "dev"})
	if err != nil {
		t.Fatalf("建树: %v", err)
	}

	// 刚建出来：干净、与 base 一致 → 可以删。
	if keep, why := worktreeSalvage(ctx, res.Dir, res.Base); keep {
		t.Fatalf("刚建的树应可清理，却说要留：%s", why)
	}

	// 有未提交的改动 → 必须留。
	if err := os.WriteFile(filepath.Join(res.Dir, "wip.txt"), []byte("half done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if keep, why := worktreeSalvage(ctx, res.Dir, res.Base); !keep || why == "" {
		t.Errorf("有未提交改动应保留，got keep=%v why=%q", keep, why)
	}

	// 提交之后（领先 base）→ 仍要留。
	gitRun(t, res.Dir, "add", ".")
	gitRun(t, res.Dir, "commit", "--quiet", "-m", "wip")
	keep, why := worktreeSalvage(ctx, res.Dir, res.Base)
	if !keep || !strings.Contains(why, "提交") {
		t.Errorf("有没合回 base 的提交应保留，got keep=%v why=%q", keep, why)
	}

	// 算不出与 base 的差距（base 名字不对）也保留——宁可留着。
	if keep, _ := worktreeSalvage(ctx, res.Dir, "no-such-base"); !keep {
		t.Error("算不出差距时应保留")
	}

	// 退回与 base 一致 → 可以删，且真的删干净。
	gitRun(t, res.Dir, "reset", "--hard", "--quiet", "origin/dev")
	if keep, why := worktreeSalvage(ctx, res.Dir, res.Base); keep {
		t.Fatalf("回到 base 后应可清理，却说要留：%s", why)
	}
	if err := removeWorktree(ctx, home, res.Dir, res.Branch); err != nil {
		t.Fatalf("removeWorktree: %v", err)
	}
	if _, err := os.Stat(res.Dir); !os.IsNotExist(err) {
		t.Errorf("工作树目录应已删除: %v", err)
	}
	if hasLocalBranch(ctx, gitHome(home), res.Branch) {
		t.Errorf("工作分支 %s 应已删除", res.Branch)
	}
}

// 契约：老布局的克隆占着新布局的项目目录时，要**整体挪开**再建新工作树
// ——里面可能有没推送的活，还有 git status 看不见的 .gitignore 文件
// （上传件、.env、构建产物），删掉就找不回来了。
func TestEnsureWorktreeRetiresLegacyClone(t *testing.T) {
	src := seedRepo(t)
	home := filepath.Join(t.TempDir(), "org", "app")
	ctx := context.Background()

	// 造一个老布局的克隆：项目目录本身就是一棵工作树，里面躺着一个
	// 未跟踪文件与一个被忽略的文件。
	if err := os.MkdirAll(filepath.Dir(home), 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, filepath.Dir(home), "clone", "--quiet", src, home)
	for _, name := range []string{"untracked.txt", "secret.env"} {
		if err := os.WriteFile(filepath.Join(home, name), []byte("keep me\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	res, err := ensureWorktree(ctx, worktreeSpec{CloneURL: src, Home: home, NameHint: "ch", Base: "dev"})
	if err != nil {
		t.Fatalf("ensureWorktree: %v", err)
	}
	if res.RetiredLegacy == "" {
		t.Fatal("老克隆应被挪开并报出新位置")
	}
	for _, name := range []string{"untracked.txt", "secret.env"} {
		if _, err := os.Stat(filepath.Join(res.RetiredLegacy, name)); err != nil {
			t.Errorf("老克隆里的 %s 不该丢: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, gitDirName, "HEAD")); err != nil {
		t.Errorf("新布局应就位: %v", err)
	}
	if body, err := os.ReadFile(filepath.Join(res.Dir, "who.txt")); err != nil || strings.TrimSpace(string(body)) != "dev" {
		t.Errorf("新工作树内容 = %q, err=%v", body, err)
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

	clean, err := readGitStatus(ctx, dir, "")
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

	st, err := readGitStatus(ctx, dir, "")
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
