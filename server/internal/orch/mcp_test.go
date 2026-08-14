package orch

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
	"acpp/server/internal/service"
	"acpp/server/internal/transcript"
)

func orchTestService(t *testing.T) (*Service, *model.OrchSession) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "orch.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.Agent{}, &model.Role{}, &model.OrchSession{}, &model.OrchTask{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := service.NewAgentService(gdb).EnsureDefaults(context.Background()); err != nil {
		t.Fatalf("seed agents: %v", err)
	}
	store, err := transcript.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("transcript store: %v", err)
	}
	svc := NewService(gdb, NewRoleService(gdb), acp.NewManager(2, 0, ""),
		store, nil, t.TempDir(), "", "127.0.0.1:48080")

	agents, _ := service.NewAgentService(gdb).List(context.Background())
	orch, err := svc.Create(context.Background(), SessionInput{AgentID: agents[0].ID})
	if err != nil {
		t.Fatalf("create orch session: %v", err)
	}
	return svc, orch
}

// 契约：MCP 端点的 JSON-RPC 面——initialize 回显协议版本并报 tools 能力，
// tools/list 只有 spawn_agent，通知无响应，未知 token 一律报错。
func TestService_HandleMCP_Protocol(t *testing.T) {
	svc, orch := orchTestService(t)
	ctx := context.Background()

	resp, ok := svc.HandleMCP(ctx, orch.MCPToken,
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`))
	if !ok {
		t.Fatal("initialize 必须有响应")
	}
	raw, _ := json.Marshal(resp)
	var init struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
			ServerInfo      struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &init); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if init.Result.ProtocolVersion != "2025-11-25" || init.Result.ServerInfo.Name != "acpp" {
		t.Fatalf("initialize result = %s", raw)
	}

	resp, ok = svc.HandleMCP(ctx, orch.MCPToken,
		[]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`))
	if !ok {
		t.Fatal("tools/list 必须有响应")
	}
	raw, _ = json.Marshal(resp)
	var list struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Result.Tools) != 1 || list.Result.Tools[0].Name != "hire_role" {
		t.Fatalf("tools = %s", raw)
	}

	if _, ok := svc.HandleMCP(ctx, orch.MCPToken,
		[]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); ok {
		t.Fatal("通知不能有响应")
	}

	resp, _ = svc.HandleMCP(ctx, "bad-token",
		[]byte(`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`))
	raw, _ = json.Marshal(resp)
	var errResp struct {
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &errResp); err != nil || errResp.Error == nil {
		t.Fatalf("未知 token 应报 JSON-RPC error: %s", raw)
	}
}

// 契约：spawn_agent 的入参失败（未知角色、空任务）走工具级错误
// （isError:true 的文本），不是 JSON-RPC error——模型读得到才能自行决策。
func TestService_HandleMCP_SpawnToolErrors(t *testing.T) {
	svc, orch := orchTestService(t)
	ctx := context.Background()

	resp, _ := svc.HandleMCP(ctx, orch.MCPToken,
		[]byte(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"hire_role","arguments":{"role":"不存在的角色","task":"做点事"}}}`))
	raw, _ := json.Marshal(resp)
	var call struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error any `json:"error"`
	}
	if err := json.Unmarshal(raw, &call); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if call.Error != nil {
		t.Fatalf("入参失败不该是 JSON-RPC error: %s", raw)
	}
	if !call.Result.IsError || len(call.Result.Content) == 0 {
		t.Fatalf("应是工具级错误: %s", raw)
	}

	resp, _ = svc.HandleMCP(ctx, orch.MCPToken,
		[]byte(`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"no_such_tool","arguments":{}}}`))
	raw, _ = json.Marshal(resp)
	var unknown struct {
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &unknown); err != nil || unknown.Error == nil {
		t.Fatalf("未知工具应是 JSON-RPC error: %s", raw)
	}
}
