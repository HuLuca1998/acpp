package service

import (
	"context"
	"errors"
	"testing"
)

// stubMounter 是一个工具面，按 flavor 返回对应方言的挂载片段。
type stubMounter struct {
	name string
	err  error
}

func (m stubMounter) MountsFor(_ context.Context, _ uint, _, flavor string) ([]any, map[string]any, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	if flavor == "claude" {
		return nil, map[string]any{
			"claudeCode": map[string]any{"options": map[string]any{
				"mcpServers":   map[string]any{m.name: map[string]any{"url": "http://x/" + m.name}},
				"allowedTools": []string{"mcp__" + m.name + "__t"},
			}},
		}, nil
	}
	return []any{map[string]any{"name": m.name}}, nil, nil
}

// 多个工具面挂进的是**同一个** _meta，谁覆盖谁都是 bug：症状是模型看不见
// 其中一组工具，而它不会报错，只会在某次调用时「装作没有数据库能力」。
func TestCollectMountsKeepsEveryFace(t *testing.T) {
	s := &ChatService{}
	s.AddMounter(stubMounter{name: "acpp-db"})
	s.AddMounter(stubMounter{name: "acpp-report"})

	t.Run("claude 侧两个 server 与两组预批都在", func(t *testing.T) {
		_, meta := s.collectMounts(context.Background(), 1, "/w", "claude")
		opts, ok := claudeOptions(meta)
		if !ok {
			t.Fatalf("meta 形状不对：%v", meta)
		}
		servers, _ := opts["mcpServers"].(map[string]any)
		if len(servers) != 2 || servers["acpp-db"] == nil || servers["acpp-report"] == nil {
			t.Errorf("mcpServers = %v，期望两个工具面都在（后挂的不该顶掉先挂的）", servers)
		}
		allowed, _ := opts["allowedTools"].([]string)
		if len(allowed) != 2 {
			t.Errorf("allowedTools = %v，期望两组预批都保留", allowed)
		}
	})

	t.Run("codex 侧两个 server 都在", func(t *testing.T) {
		servers, meta := s.collectMounts(context.Background(), 1, "/w", "codex")
		if meta != nil {
			t.Errorf("codex 侧不该产出 _meta，得到 %v", meta)
		}
		if len(servers) != 2 {
			t.Errorf("servers = %v，期望两个都在", servers)
		}
	})
}

// 一个工具面算不出来不算失败：没有数据库工具的会话照样是一条正常会话。
// 所以一个源出错，别的源必须照常挂上，而不是一起被放弃。
func TestCollectMountsSurvivesOneFailure(t *testing.T) {
	s := &ChatService{}
	s.AddMounter(stubMounter{name: "acpp-db", err: errors.New("数据源连不上")})
	s.AddMounter(stubMounter{name: "acpp-report"})

	_, meta := s.collectMounts(context.Background(), 1, "/w", "claude")
	opts, ok := claudeOptions(meta)
	if !ok {
		t.Fatalf("一个源失败就不该拖垮另一个，meta = %v", meta)
	}
	servers, _ := opts["mcpServers"].(map[string]any)
	if servers["acpp-report"] == nil {
		t.Errorf("mcpServers = %v，期望报告工具面照常挂上", servers)
	}
	if servers["acpp-db"] != nil {
		t.Errorf("mcpServers = %v，失败的源不该留下半份配置", servers)
	}
}

// 没有任何工具面时不该凭空造出一个空壳 _meta——agent 那边收到空 options
// 与收不到 _meta 是两回事。
func TestCollectMountsEmpty(t *testing.T) {
	s := &ChatService{}
	servers, meta := s.collectMounts(context.Background(), 1, "/w", "claude")
	if servers != nil || meta != nil {
		t.Errorf("没有工具面时应该两个都是 nil，得到 servers=%v meta=%v", servers, meta)
	}
}
