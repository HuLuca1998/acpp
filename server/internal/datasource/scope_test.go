package datasource

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"acpp/server/internal/model"
)

// 契约：工作目录 → 项目名候选。这是可见性边界的输入端，推错就等于
// 把别的项目的生产库摊给了当前会话。
func TestProjectCandidates(t *testing.T) {
	root := t.TempDir()

	tests := []struct {
		name string
		cwd  string
		want []string
	}{
		{
			name: "工作区根下的两层项目：全路径与仓库名都算",
			cwd:  filepath.Join(root, "BDBGAME2024", "pp-game"),
			want: []string{"BDBGAME2024/pp-game", "pp-game"},
		},
		{
			name: "项目子目录同样归属该项目",
			cwd:  filepath.Join(root, "BDBGAME2024", "pp-game", "server", "internal"),
			want: []string{"BDBGAME2024/pp-game/server/internal", "internal"},
		},
		{
			name: "worktree 归属主仓库",
			cwd:  filepath.Join(root, "acpp", "worktrees", "feat-x"),
			want: []string{"acpp"},
		},
		{
			name: "工作区根自身不属于任何项目",
			cwd:  root,
			want: nil,
		},
		{
			name: "空工作目录推不出项目",
			cwd:  "",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := projectCandidates(tt.cwd, root)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("projectCandidates(%q) = %#v, want %#v", tt.cwd, got, tt.want)
			}
		})
	}
}

// 契约：工作区根之外的目录靠最近的 git 仓库定位项目——owner 可以把会话
// 开在任意位置，那些会话同样该看得到自己项目的库。
func TestProjectCandidates_OutsideWorkspace(t *testing.T) {
	elsewhere := t.TempDir()
	repo := filepath.Join(elsewhere, "pp-game")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	sub := filepath.Join(repo, "server")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got := projectCandidates(sub, t.TempDir())
	if !reflect.DeepEqual(got, []string{"pp-game"}) {
		t.Fatalf("projectCandidates = %#v, want [pp-game]", got)
	}
}

func testService(t *testing.T, root string) *Service {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ds.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.DataSource{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	svc := NewService(gdb, nil, "127.0.0.1:48080")
	svc.workspaceRoot = func() string { return root }
	return svc
}

// 契约：会话只能取到自己项目的数据源。这条过滤是整个功能的安全底座——
// 它一松，一条开在 A 项目的会话就能连到 B 项目的生产库。
func TestService_ForCwd_ProjectIsolation(t *testing.T) {
	root := t.TempDir()
	svc := testService(t, root)
	ctx := context.Background()

	pw := "secret"
	for _, in := range []Input{
		{Project: "pp-game", Env: "local", Host: "127.0.0.1", User: "root", Password: &pw, Database: "pp_game"},
		{Project: "pp-game", Env: "dev", Host: "10.0.0.1", User: "root", Password: &pw, Database: "pp_game"},
		{Project: "other-app", Env: "prod", Host: "10.0.0.2", User: "root", Password: &pw, Database: "other"},
		{Project: "pp-game", Env: "pre", Host: "10.0.0.3", User: "root", Password: &pw,
			Database: "pp_game", Disabled: ptr(true)},
	} {
		if _, err := svc.Create(ctx, in); err != nil {
			t.Fatalf("create %s/%s: %v", in.Project, in.Env, err)
		}
	}

	cwd := filepath.Join(root, "pp-game")
	got, err := svc.ForCwd(ctx, cwd, false)
	if err != nil {
		t.Fatalf("ForCwd: %v", err)
	}
	if want := []string{"pp-game/dev", "pp-game/local", "pp-game/pre"}; !reflect.DeepEqual(refsOf(got), want) {
		t.Fatalf("ForCwd = %v, want %v", refsOf(got), want)
	}

	// 挂给 AI 的工具面只看启用的。
	enabled, err := svc.ForCwd(ctx, cwd, true)
	if err != nil {
		t.Fatalf("ForCwd enabled: %v", err)
	}
	if want := []string{"pp-game/dev", "pp-game/local"}; !reflect.DeepEqual(refsOf(enabled), want) {
		t.Fatalf("ForCwd(enabled) = %v, want %v", refsOf(enabled), want)
	}

	// 推不出项目就一个都拿不到——不是「拿到全部」。
	orphan, err := svc.ForCwd(ctx, root, true)
	if err != nil {
		t.Fatalf("ForCwd orphan: %v", err)
	}
	if len(orphan) != 0 {
		t.Fatalf("工作区根下不属于任何项目，应取不到数据源，实际 %v", refsOf(orphan))
	}
}

// 契约：ref 可以写全（项目/环境）、只写环境，或在唯一候选时省略；
// 认不出来时错误里要带上可用清单，模型才知道下一步填什么。
func TestResolve(t *testing.T) {
	sources := []model.DataSource{
		{Project: "pp-game", Env: "local", Ref: "pp-game/local"},
		{Project: "pp-game", Env: "dev", Ref: "pp-game/dev"},
	}

	for _, ref := range []string{"dev", "pp-game/dev", "DEV"} {
		got, err := Resolve(sources, ref)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", ref, err)
		}
		if got.Ref != "pp-game/dev" {
			t.Fatalf("Resolve(%q) = %s", ref, got.Ref)
		}
	}

	if _, err := Resolve(sources, ""); err == nil {
		t.Fatal("多个候选时省略 ref 应报错并列出候选")
	}
	if _, err := Resolve(sources[:1], ""); err != nil {
		t.Fatalf("唯一候选时可省略 ref: %v", err)
	}
	if _, err := Resolve(sources, "nope"); err == nil {
		t.Fatal("未知 ref 应报错")
	}
	if _, err := Resolve(nil, "dev"); err == nil {
		t.Fatal("没有数据源时应报错")
	}
}

// 契约：必填项与格式在写库前挡住——项目/环境是对外标识 `<项目>/<环境>`
// 的两半，含斜杠会让它没法解析。
func TestService_Create_Validation(t *testing.T) {
	svc := testService(t, t.TempDir())
	ctx := context.Background()

	bad := []Input{
		{Env: "local", Host: "h", User: "u", Database: "d"},                                      // 缺项目
		{Project: "p", Host: "h", User: "u", Database: "d"},                                      // 缺环境
		{Project: "p", Env: "local", User: "u", Database: "d"},                                   // 缺主机
		{Project: "p", Env: "local", Host: "h", Database: "d"},                                   // 缺用户
		{Project: "p", Env: "local", Host: "h", User: "u"},                                       // 缺库（一条连接必须绑一个库）
		{Project: "a/b", Env: "local", Host: "h", User: "u", Database: "d"},                      // 项目含斜杠
		{Project: "p", Env: "local", Host: "h", User: "u", Database: "d", SSHEnabled: ptr(true)}, // 开隧道但没跳板机
		{Project: "p", Env: "local", Host: "h", User: "u", Database: "d", SSHAuth: "magic"},      // 未知验证方式
	}
	for i, in := range bad {
		if _, err := svc.Create(ctx, in); err == nil {
			t.Errorf("第 %d 条非法入参应被拒绝: %+v", i, in)
		}
	}

	if _, err := svc.Create(ctx, Input{Project: "p", Env: "local", Host: "h", User: "u", Database: "d"}); err != nil {
		t.Fatalf("合法入参: %v", err)
	}
	// 同一项目同一环境只能有一条：重复配置只会让人分不清用的是哪个。
	if _, err := svc.Create(ctx, Input{Project: "p", Env: "local", Host: "h2", User: "u", Database: "d"}); err == nil {
		t.Fatal("项目+环境重复应被拒绝")
	}
}

func ptr[T any](v T) *T { return &v }
