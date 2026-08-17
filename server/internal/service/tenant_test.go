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

func tenantService(t *testing.T) (*TenantService, string) {
	t.Helper()
	base := t.TempDir()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "tenants.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.Tenant{}, &model.Session{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewTenantService(gdb, base), base
}

// 契约：建租户即落 root 目录并发凭证——目录选择器一进来就得有个能站住
// 的位置，不能等首次使用才建。
func TestTenantService_Create_MakesRootAndToken(t *testing.T) {
	svc, base := tenantService(t)

	view, err := svc.Create(context.Background(), TenantInput{Name: "alice"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if view.InviteToken == "" || view.InviteToken != view.Token {
		t.Fatalf("invite token = %q, token = %q, want same non-empty", view.InviteToken, view.Token)
	}
	wantRoot := filepath.Join(base, "alice")
	if view.Root != wantRoot {
		t.Fatalf("root = %q, want %q", view.Root, wantRoot)
	}
	if _, err := svc.Get(context.Background(), view.ID); err != nil {
		t.Fatalf("Get after create: %v", err)
	}
	if got, err := TenantScope(view.ID, view.Root).GuardPath(""); err != nil || got == "" {
		t.Fatalf("guard root: got %q err %v", got, err)
	}
}

// 契约：租户名同时是目录名，因此路径分隔符、上跳、隐藏名一律拒绝——
// 放行任何一个都等于让建租户这一步就能写到 base 之外。
func TestTenantService_Create_RejectsUnsafeNames(t *testing.T) {
	svc, _ := tenantService(t)

	for _, name := range []string{"", "../escape", "a/b", ".hidden", "名字", "with space"} {
		if _, err := svc.Create(context.Background(), TenantInput{Name: name}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Create(%q) err = %v, want ErrInvalid", name, err)
		}
	}
}

// 契约：停用即时生效（每次鉴权现查），且与「凭证不认识」是两种不同的
// 失败——停用返回 ErrForbidden 并**仍带回租户**，界面才能说出「你的访问
// 已被关闭」而不是「请用邀请链接」。
func TestTenantService_Authenticate_DistinguishesDisabledFromUnknown(t *testing.T) {
	svc, _ := tenantService(t)
	ctx := context.Background()

	live, err := svc.Create(ctx, TenantInput{Name: "live"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Authenticate(ctx, live.Token); err != nil {
		t.Fatalf("Authenticate(live): %v", err)
	}

	disabled := true
	if _, err := svc.Update(ctx, live.ID, TenantPatch{Disabled: &disabled}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	tenant, err := svc.Authenticate(ctx, live.Token)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Authenticate(disabled) err = %v, want ErrForbidden", err)
	}
	if tenant == nil || tenant.Name != "live" {
		t.Fatalf("Authenticate(disabled) tenant = %+v, want the tenant for the UI message", tenant)
	}

	if _, err := svc.Authenticate(ctx, "not-a-real-token"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Authenticate(unknown) err = %v, want ErrUnauthorized", err)
	}
	if _, err := svc.Authenticate(ctx, ""); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Authenticate(empty) err = %v, want ErrUnauthorized", err)
	}

	// 重新打开就该立刻恢复：停用是开关，不是删除。
	enabled := false
	if _, err := svc.Update(ctx, live.ID, TenantPatch{Disabled: &enabled}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, err := svc.Authenticate(ctx, live.Token); err != nil {
		t.Fatalf("Authenticate(re-enabled): %v", err)
	}
}

// 契约：重新生成分享链接后旧链接立刻作废——链接外泄时这是止血手段，
// 作废不彻底等于没用。
func TestTenantService_Rotate_InvalidatesOldToken(t *testing.T) {
	svc, _ := tenantService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, TenantInput{Name: "bob"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	rotated, err := svc.Rotate(ctx, created.ID)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if rotated.InviteToken == created.InviteToken {
		t.Fatal("rotate returned the same token")
	}
	if _, err := svc.Authenticate(ctx, created.InviteToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("old token still works: %v", err)
	}
	if _, err := svc.Authenticate(ctx, rotated.InviteToken); err != nil {
		t.Fatalf("Authenticate(rotated): %v", err)
	}
}

