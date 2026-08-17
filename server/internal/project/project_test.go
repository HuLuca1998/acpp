package project

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

// repoAt 造一个「像 git 仓库」的目录：项目发现只看 .git 存不存在。
func repoAt(t *testing.T, path, remote, branch string) {
	t.Helper()
	gitDir := filepath.Join(path, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", gitDir, err)
	}
	config := "[core]\n\trepositoryformatversion = 0\n[remote \"origin\"]\n\turl = " + remote + "\n"
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/"+branch+"\n"), 0o644); err != nil {
		t.Fatalf("write HEAD: %v", err)
	}
}

func projectService(t *testing.T) (*Service, service.Scope, string) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "projects.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.Session{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	root := filepath.Join(t.TempDir(), "alice")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	return NewService(gdb), service.TenantScope(1, root), root
}

// 契约：项目 = 工作区根下的 git 仓库目录，名字是相对根的路径
// （`<组织>/<仓库>`）；node_modules 一类目录不进扫描，仓库内部的嵌套
// 仓库也不各自成项目——否则一个前端仓库能刷出几十个「项目」。
func TestService_List_DiscoversRepos(t *testing.T) {
	svc, scope, root := projectService(t)

	repoAt(t, filepath.Join(root, "BDBGAME2024", "pp-game"), "https://github.com/BDBGAME2024/pp-game.git", "main")
	repoAt(t, filepath.Join(root, "BDBGAME2024", "evo-game"), "git@github.com:BDBGAME2024/evo-game.git", "develop")
	// 仓库内部的嵌套 .git 不该被当成独立项目。
	repoAt(t, filepath.Join(root, "BDBGAME2024", "pp-game", "vendor", "dep"), "", "main")
	// 跳过目录里的仓库同样不算。
	repoAt(t, filepath.Join(root, "node_modules", "pkg"), "", "main")
	// 没有 .git 的普通目录不是项目。
	if err := os.MkdirAll(filepath.Join(root, "scratch"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	projects, err := svc.List(context.Background(), scope)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	names := map[string]Project{}
	for _, p := range projects {
		names[p.Name] = p
	}
	if len(names) != 2 {
		t.Fatalf("discovered %v, want exactly the two repos", names)
	}
	pp, ok := names["BDBGAME2024/pp-game"]
	if !ok {
		t.Fatalf("missing BDBGAME2024/pp-game in %v", names)
	}
	if pp.Remote != "https://github.com/BDBGAME2024/pp-game.git" || pp.Branch != "main" {
		t.Fatalf("pp-game = %+v, want remote/branch read from .git", pp)
	}
	if names["BDBGAME2024/evo-game"].Branch != "develop" {
		t.Fatalf("evo-game branch = %q, want develop", names["BDBGAME2024/evo-game"].Branch)
	}
}

// 契约：会话数按 cwd 前缀归属，worktree 子目录算进它的主仓库。
func TestService_List_CountsSessionsByCwd(t *testing.T) {
	svc, scope, root := projectService(t)
	repo := filepath.Join(root, "org", "app")
	repoAt(t, repo, "https://example.com/org/app.git", "main")

	for _, cwd := range []string{repo, filepath.Join(repo, "worktrees", "feature"), filepath.Join(root, "elsewhere")} {
		if err := svc.db.Create(&model.Session{AgentID: 1, TenantID: 1, Cwd: cwd}).Error; err != nil {
			t.Fatalf("seed session: %v", err)
		}
	}

	projects, err := svc.List(context.Background(), scope)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(projects) != 1 || projects[0].SessionCount != 2 {
		t.Fatalf("projects = %+v, want one project with 2 sessions", projects)
	}
}

// 契约：项目名会被拼进文件系统路径，逃逸与超深路径一律拒绝。
func TestCleanProjectName(t *testing.T) {
	for _, name := range []string{"", "..", "../evil", "a/b/c", "org/../../etc", ".hidden/repo"} {
		if _, err := cleanProjectName(name); !errors.Is(err, service.ErrInvalid) {
			t.Errorf("cleanProjectName(%q) err = %v, want ErrInvalid", name, err)
		}
	}
	// 首尾斜杠是规范化掉的，不算错误。
	for name, want := range map[string]string{
		"repo":                "repo",
		"BDBGAME2024/pp-game": filepath.Join("BDBGAME2024", "pp-game"),
		"org/repo.js":         filepath.Join("org", "repo.js"),
		"org/":                "org",
		"/org/repo":           filepath.Join("org", "repo"),
	} {
		got, err := cleanProjectName(name)
		if err != nil || got != want {
			t.Errorf("cleanProjectName(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
}

// 契约：克隆落点是 `<root>/<组织>/<仓库>`，两种 URL 形式都要还原出同样的
// 两层——用户要的就是 `<租户>/BDBGAME2024/pp-game` 这个形状。
func TestRepoNameFromURL(t *testing.T) {
	cases := map[string]string{
		"https://github.com/BDBGAME2024/pp-game.git": "BDBGAME2024/pp-game",
		"https://github.com/BDBGAME2024/pp-game":     "BDBGAME2024/pp-game",
		"git@github.com:BDBGAME2024/pp-game.git":     "BDBGAME2024/pp-game",
		"https://gitlab.com/group/sub/proj.git":      "sub/proj",
	}
	for url, want := range cases {
		if got := repoNameFromURL(url); got != want {
			t.Errorf("repoNameFromURL(%q) = %q, want %q", url, got, want)
		}
	}
}

// 契约：只放行 https 与 scp 形式的 git URL。file:// 与 ext:: 这类传输能
// 在本机乱指、甚至直接执行命令，克隆框不是让人干这个的地方。
func TestService_Clone_RejectsUnsafeURLs(t *testing.T) {
	svc, scope, _ := projectService(t)

	for _, url := range []string{
		"", "file:///etc", "ext::sh -c whoami", "/etc/passwd",
		"https://github.com/org/repo; rm -rf /", "--upload-pack=touch /tmp/pwned",
	} {
		if _, err := svc.Clone(scope, CloneInput{URL: url}); !errors.Is(err, service.ErrInvalid) {
			t.Errorf("Clone(%q) err = %v, want ErrInvalid", url, err)
		}
	}
}

// 契约：克隆目标已存在时拒绝，不覆盖也不悄悄合并。
func TestService_Clone_RejectsExistingTarget(t *testing.T) {
	svc, scope, root := projectService(t)
	repoAt(t, filepath.Join(root, "org", "app"), "https://example.com/org/app.git", "main")

	_, err := svc.Clone(scope, CloneInput{URL: "https://github.com/org/app.git"})
	if !errors.Is(err, service.ErrInvalid) {
		t.Fatalf("Clone into existing dir err = %v, want ErrInvalid", err)
	}
}

// 契约：删项目只删自己工作区里的目录，且绝不删到工作区根本身。
func TestService_Delete(t *testing.T) {
	svc, scope, root := projectService(t)
	repo := filepath.Join(root, "org", "app")
	repoAt(t, repo, "", "main")

	if err := svc.Delete(scope, "org/app"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(repo); !os.IsNotExist(err) {
		t.Fatalf("project dir still there: %v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("workspace root damaged: %v", err)
	}

	for _, name := range []string{"..", "../alice", "/etc"} {
		if err := svc.Delete(scope, name); err == nil {
			t.Fatalf("Delete(%q) = nil, want rejection", name)
		}
	}
}
