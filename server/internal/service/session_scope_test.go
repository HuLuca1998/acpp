package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"acpp/server/internal/model"
)

// scopedSessions 建一套「两个租户各一条会话」的现场，用来验隔离。
func scopedSessions(t *testing.T) (*SessionService, Scope, Scope, uint, uint) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "sessions.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.Agent{}, &model.Session{}, &model.Tenant{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	agent := model.Agent{Name: "claude", Command: "claude-agent-acp"}
	if err := gdb.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	base := t.TempDir()
	rootA := filepath.Join(base, "alice")
	rootB := filepath.Join(base, "bob")
	for _, dir := range []string{rootA, rootB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	alice := TenantScope(1, rootA)
	bob := TenantScope(2, rootB)

	svc := NewSessionService(gdb)
	ctx := context.Background()
	sessionA, err := svc.Create(ctx, alice, SessionInput{AgentID: agent.ID, Title: "a"})
	if err != nil {
		t.Fatalf("create session for alice: %v", err)
	}
	sessionB, err := svc.Create(ctx, bob, SessionInput{AgentID: agent.ID, Title: "b"})
	if err != nil {
		t.Fatalf("create session for bob: %v", err)
	}
	return svc, alice, bob, sessionA.ID, sessionB.ID
}

// 契约：会话隔离是查询本身带的条件——列表看不到别人的，按 id 直取别人的
// 会话返回 404（不是 403：403 会泄露「这条会话存在」，凭 id 递增就能数出
// 别人有多少条）。owner 看得见全部。
func TestSessionService_ScopeIsolation(t *testing.T) {
	svc, alice, bob, idA, idB := scopedSessions(t)
	ctx := context.Background()

	list, total, err := svc.List(ctx, alice, 0, 1, 50)
	if err != nil {
		t.Fatalf("List(alice): %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].ID != idA {
		t.Fatalf("alice sees %d sessions %+v, want only her own", total, list)
	}

	if _, err := svc.Get(ctx, alice, idB); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(alice, bob's session) err = %v, want ErrNotFound", err)
	}
	if err := svc.Delete(ctx, alice, idB); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete(alice, bob's session) err = %v, want ErrNotFound", err)
	}
	// 删不掉才算数：确认 bob 的会话还在。
	if _, err := svc.Get(ctx, bob, idB); err != nil {
		t.Fatalf("bob's session gone after alice tried to delete: %v", err)
	}

	_, total, err = svc.List(ctx, OwnerScope(), 0, 1, 50)
	if err != nil {
		t.Fatalf("List(owner): %v", err)
	}
	if total != 2 {
		t.Fatalf("owner sees %d sessions, want 2", total)
	}
}

// 契约：租户不带 cwd 建会话时落在自己的 root，指定别人的目录一律拒绝——
// 否则一条建会话请求就能把 agent 开进别人的工作区。
func TestSessionService_CreateGuardsCwd(t *testing.T) {
	svc, alice, bob, _, _ := scopedSessions(t)
	ctx := context.Background()

	defaulted, err := svc.Get(ctx, alice, 1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// 存下来的 cwd 是 canonical 形式（符号链接已解析），比较时对齐 root。
	canonRoot, err := filepath.EvalSymlinks(alice.Root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if !within(canonRoot, defaulted.Cwd) {
		t.Fatalf("default cwd = %q, want inside %q", defaulted.Cwd, canonRoot)
	}

	if _, err := svc.Create(ctx, alice, SessionInput{AgentID: 1, Cwd: bob.Root}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("create into bob's root err = %v, want ErrForbidden", err)
	}
	if _, err := svc.Create(ctx, alice, SessionInput{AgentID: 1, Cwd: "/etc"}); err == nil {
		t.Fatal("create into /etc succeeded, want rejection")
	}

	// root 下的新子目录是允许的：它还不存在，但父目录在 root 内。
	nested := filepath.Join(alice.Root, "new-project")
	if _, err := svc.Create(ctx, alice, SessionInput{AgentID: 1, Cwd: nested}); err != nil {
		t.Fatalf("create into own new subdir: %v", err)
	}
}
