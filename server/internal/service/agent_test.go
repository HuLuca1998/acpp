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
