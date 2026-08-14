package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"acpp/server/internal/model"
)

func roleDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "roles.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.Agent{}, &model.Role{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return gdb
}

// 契约：内置工具就位时，空角色表整批预置四个流水线角色，全带 builtin
// 标记且 persona/description 非空（分别喂子会话与主会话的雇佣目录）。
func TestRoleService_EnsureDefaults_SeedsPipelineRoles(t *testing.T) {
	gdb := roleDB(t)
	if _, err := NewAgentService(gdb).EnsureDefaults(context.Background()); err != nil {
		t.Fatalf("seed agents: %v", err)
	}
	svc := NewRoleService(gdb)

	if err := svc.EnsureDefaults(context.Background()); err != nil {
		t.Fatalf("EnsureDefaults: %v", err)
	}

	roles, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(roles) != 4 {
		t.Fatalf("roles = %d, want 4: %+v", len(roles), roles)
	}
	for _, r := range roles {
		if !r.Builtin {
			t.Errorf("role %s 缺 builtin 标记", r.Name)
		}
		if r.Persona == "" || r.Description == "" {
			t.Errorf("role %s persona/description 不能为空", r.Name)
		}
		if r.AgentID == 0 {
			t.Errorf("role %s 未绑定工具", r.Name)
		}
	}
}

// 契约：用户删除内置角色后不复活——只要 builtin 角色存在过或用户建过
// 自己的角色，就不再整批预置。
func TestRoleService_EnsureDefaults_DoesNotResurrect(t *testing.T) {
	gdb := roleDB(t)
	if _, err := NewAgentService(gdb).EnsureDefaults(context.Background()); err != nil {
		t.Fatalf("seed agents: %v", err)
	}
	svc := NewRoleService(gdb)
	if err := svc.EnsureDefaults(context.Background()); err != nil {
		t.Fatalf("EnsureDefaults: %v", err)
	}

	roles, _ := svc.List(context.Background())
	for _, r := range roles {
		if err := svc.Delete(context.Background(), r.ID); err != nil {
			t.Fatalf("Delete %s: %v", r.Name, err)
		}
	}
	// 用户自建一个，模拟「删光内置、只留自己的」库。
	agents, _ := NewAgentService(gdb).List(context.Background())
	if _, err := svc.Create(context.Background(), RoleInput{
		Name: "我的角色", AgentID: agents[0].ID,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.EnsureDefaults(context.Background()); err != nil {
		t.Fatalf("EnsureDefaults #2: %v", err)
	}
	roles, _ = svc.List(context.Background())
	if len(roles) != 1 {
		t.Fatalf("内置角色被复活: %+v", roles)
	}
}

// 契约：CRUD 基本行为——名字必填、agentId 必填、按名字查得到、删除后 404。
func TestRoleService_CRUD(t *testing.T) {
	gdb := roleDB(t)
	if _, err := NewAgentService(gdb).EnsureDefaults(context.Background()); err != nil {
		t.Fatalf("seed agents: %v", err)
	}
	agents, _ := NewAgentService(gdb).List(context.Background())
	svc := NewRoleService(gdb)
	ctx := context.Background()

	if _, err := svc.Create(ctx, RoleInput{AgentID: agents[0].ID}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("空名字应 ErrInvalid，got %v", err)
	}
	if _, err := svc.Create(ctx, RoleInput{Name: "r"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("缺 agentId 应 ErrInvalid，got %v", err)
	}

	role, err := svc.Create(ctx, RoleInput{
		Name: "架构师", Description: "d", Persona: "p",
		AgentID: agents[0].ID, Model: "m", Effort: "high", Level: "safe",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	byName, err := svc.GetByName(ctx, " 架构师 ")
	if err != nil || byName.ID != role.ID {
		t.Fatalf("GetByName（含空白）= %v, %v", byName, err)
	}

	role, err = svc.Update(ctx, role.ID, RoleInput{
		Name: "架构师2", AgentID: agents[0].ID, Level: "auto-edit",
	})
	if err != nil || role.Name != "架构师2" || role.Level != "auto-edit" {
		t.Fatalf("Update = %+v, %v", role, err)
	}

	if err := svc.Delete(ctx, role.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := svc.Get(ctx, role.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后应 ErrNotFound，got %v", err)
	}
}
