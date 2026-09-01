package remote

import (
	"context"
	"strings"
	"testing"
)

// 契约：一台服务器都没配时**什么都不挂**。工具清单里凭空多出十几个用不了
// 的条目，只会让模型乱试，也白占它的注意力。
func TestMountsForPeer_NothingWhenNoServers(t *testing.T) {
	svc, _ := testService(t)
	servers, meta, err := svc.MountsForPeer(context.Background(), "k", "/tmp", "claude", 0)
	if err != nil {
		t.Fatalf("mounts: %v", err)
	}
	if servers != nil || meta != nil {
		t.Fatalf("没有服务器时不该挂任何东西: %+v / %+v", servers, meta)
	}
}

// 契约：两端注入口的形状不同（实测见 team-mode-protocol-findings）——
// claude 走 _meta 里的 claudeCode.options，codex 走 session/new 的 mcpServers。
// 写错任何一边的症状都是「模型看不见这些工具」，很难追。
func TestMountsForPeer_FlavorShapes(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()
	if _, err := svc.Create(ctx, Input{Name: "box", Host: "h", User: "u", Auth: "key"}); err != nil {
		t.Fatal(err)
	}

	t.Run("claude 走 _meta 且预批工具名", func(t *testing.T) {
		servers, meta, err := svc.MountsForPeer(ctx, "k1", "/tmp", "claude", 0)
		if err != nil {
			t.Fatalf("mounts: %v", err)
		}
		if servers != nil {
			t.Errorf("claude 侧不走 mcpServers 数组: %+v", servers)
		}
		opts := claudeOptions(t, meta)
		mcpServers, ok := opts["mcpServers"].(map[string]any)
		if !ok || mcpServers[mcpServerName] == nil {
			t.Fatalf("缺 mcpServers.%s: %+v", mcpServerName, opts)
		}
		conf, _ := mcpServers[mcpServerName].(map[string]any)
		if conf["type"] != "http" {
			t.Errorf("回连方式应是 http: %+v", conf)
		}
		url, _ := conf["url"].(string)
		if !strings.Contains(url, "/api/mcp/server/") {
			t.Errorf("回连端点不对: %q", url)
		}
		// 预批：观察类工具一轮排障要调十几次，每次弹卡没有意义。
		allowed, _ := opts["allowedTools"].([]string)
		if len(allowed) != len(svc.tools(Scope{})) {
			t.Errorf("预批清单 %d 条与工具数 %d 对不上", len(allowed), len(svc.tools(Scope{})))
		}
	})

	t.Run("codex 走 mcpServers 数组", func(t *testing.T) {
		servers, meta, err := svc.MountsForPeer(ctx, "k2", "/tmp", "codex", 0)
		if err != nil {
			t.Fatalf("mounts: %v", err)
		}
		if meta != nil {
			t.Errorf("codex 侧不用 _meta: %+v", meta)
		}
		if len(servers) != 1 {
			t.Fatalf("期望一个 server 条目，得到 %+v", servers)
		}
		entry, _ := servers[0].(map[string]any)
		if entry["name"] != mcpServerName || entry["type"] != "http" {
			t.Errorf("条目形状不对: %+v", entry)
		}
	})
}

// 契约：频道锁定了机器时，凭证也带着那个作用域——工具面因此连别的机器
// 都列不出来。锁定的机器没了则整个不挂（降级方向只能是「更少」）。
func TestMountsForPeer_ScopeLock(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()
	one, err := svc.Create(ctx, Input{Name: "a", Host: "h", User: "u", Auth: "key"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, Input{Name: "b", Host: "h", User: "u", Auth: "key"}); err != nil {
		t.Fatal(err)
	}

	if _, meta, err := svc.MountsForPeer(ctx, "k", "/tmp", "claude", one.ID); err != nil || meta == nil {
		t.Fatalf("锁定到存在的机器应正常挂载: %v", err)
	}
	servers, meta, err := svc.MountsForPeer(ctx, "k", "/tmp", "claude", 99999)
	if err != nil {
		t.Fatalf("mounts: %v", err)
	}
	if servers != nil || meta != nil {
		t.Fatal("锁定的机器不存在时应整个不挂，而不是回退成全部可见")
	}
}

func claudeOptions(t *testing.T, meta map[string]any) map[string]any {
	t.Helper()
	cc, ok := meta["claudeCode"].(map[string]any)
	if !ok {
		t.Fatalf("meta 里没有 claudeCode: %+v", meta)
	}
	opts, ok := cc["options"].(map[string]any)
	if !ok {
		t.Fatalf("claudeCode 里没有 options: %+v", cc)
	}
	return opts
}
