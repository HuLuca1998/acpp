package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"acpp/server/internal/model"
)

func agentDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "agents.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.Agent{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return gdb
}

// 契约：空库时补建全部内置工具（claude/codex），命令即 README 快速开始
// 里的两个 ACP runtime，返回的 id 正是新建的记录。
func TestAgentService_EnsureDefaults_SeedsEmptyDB(t *testing.T) {
	svc := NewAgentService(agentDB(t))

	created, err := svc.EnsureDefaults(context.Background())
	if err != nil {
		t.Fatalf("EnsureDefaults: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("created = %v, want 2 ids", created)
	}

	agents, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := map[string]string{"claude": "claude-agent-acp", "codex": "codex-acp"}
	if len(agents) != len(want) {
		t.Fatalf("agents = %d, want %d: %+v", len(agents), len(want), agents)
	}
	for _, a := range agents {
		if want[a.Name] != a.Command {
			t.Errorf("agent %s command = %q, want %q", a.Name, a.Command, want[a.Name])
		}
	}
}

// 契约：幂等且不覆盖——已存在的同名记录（含用户改过的命令）原样保留，
// 重复调用不新建。
func TestAgentService_EnsureDefaults_KeepsExistingConfig(t *testing.T) {
	svc := NewAgentService(agentDB(t))

	custom, err := svc.Create(context.Background(), AgentInput{
		Name:    "claude",
		Command: "/opt/homebrew/bin/claude-agent-acp",
		Args:    []string{"--verbose"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	for i := range 2 {
		if _, err := svc.EnsureDefaults(context.Background()); err != nil {
			t.Fatalf("EnsureDefaults #%d: %v", i+1, err)
		}
	}

	agents, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("agents = %d, want 2（claude 保留 + codex 补建）: %+v", len(agents), agents)
	}
	got, err := svc.Get(context.Background(), custom.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Command != "/opt/homebrew/bin/claude-agent-acp" || len(got.Args) != 1 {
		t.Errorf("用户配置被覆盖: command=%q args=%v", got.Command, got.Args)
	}
}

// 契约：AI 协作的模型与思考深度只能从探测清单里选（adr-022）——配置页的
// 下拉本就只给清单项，API 层再挡一次，免得手写请求把 agent 拨到一个不存在
// 的模型上、每次 ask 都失败。空串是「沿用默认」，永远合法。
func TestAgentService_UpdateCatalog_AskPreferences(t *testing.T) {
	svc := NewAgentService(agentDB(t))
	created, err := svc.Create(t.Context(), AgentInput{Name: "codex", Command: "codex-acp"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	created.Models = model.AgentModelSlice{{ID: "gpt-5", Name: "GPT-5"}}
	created.Skeleton = model.AgentSkeleton{Efforts: []string{"low", "high"}}
	if err := svc.db.Save(created).Error; err != nil {
		t.Fatalf("seed catalog: %v", err)
	}

	str := func(s string) *string { return &s }
	cases := []struct {
		name   string
		in     CatalogInput
		wantOK bool
	}{
		{"清单内的模型与深度", CatalogInput{AskModel: str("gpt-5"), AskEffort: str("high")}, true},
		{"空串=沿用默认", CatalogInput{AskModel: str(""), AskEffort: str("")}, true},
		{"清单外的模型", CatalogInput{AskModel: str("gpt-9")}, false},
		{"清单外的深度", CatalogInput{AskEffort: str("ultra")}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := svc.UpdateCatalog(t.Context(), created.ID, tc.in)
			if tc.wantOK != (err == nil) {
				t.Fatalf("err = %v, wantOK %v", err, tc.wantOK)
			}
			if err == nil && tc.in.AskModel != nil && got.AskModel != *tc.in.AskModel {
				t.Fatalf("askModel = %q, want %q", got.AskModel, *tc.in.AskModel)
			}
		})
	}
}

// 契约：配置页动了模型取舍（禁用/别名）就通知下游清单变了；只改 AI 协作
// 偏好这类不影响清单的项不吵下游。删 agent 同样通知。
func TestAgentService_CatalogChangeHook(t *testing.T) {
	svc := NewAgentService(agentDB(t))
	fired := 0
	svc.OnCatalogChanged = func() { fired++ }
	created, err := svc.Create(t.Context(), AgentInput{Name: "codex", Command: "codex-acp"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	created.Models = model.AgentModelSlice{{ID: "gpt-5", Name: "GPT-5"}}
	if err := svc.db.Save(created).Error; err != nil {
		t.Fatalf("seed catalog: %v", err)
	}

	str := func(s string) *string { return &s }
	if _, err := svc.UpdateCatalog(t.Context(), created.ID, CatalogInput{AskModel: str("gpt-5")}); err != nil {
		t.Fatalf("update ask prefs: %v", err)
	}
	if fired != 0 {
		t.Fatalf("只改 AI 协作偏好也触发了钩子 (%d 次)", fired)
	}
	if _, err := svc.UpdateCatalog(t.Context(), created.ID, CatalogInput{
		Models: []CatalogItem{{Key: "gpt-5", Disabled: true}},
	}); err != nil {
		t.Fatalf("update models: %v", err)
	}
	if fired != 1 {
		t.Fatalf("改模型取舍后钩子触发 %d 次, want 1", fired)
	}
	if err := svc.Delete(t.Context(), created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if fired != 2 {
		t.Fatalf("删 agent 后钩子触发 %d 次, want 2", fired)
	}
}
