package discord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// 契约：RefreshCommands 对已连接的每个 guild 重新 PUT 斜杠命令，/model 的
// choices 取**当下**的 catalog 而不是连接时的快照；没连上（无 appId）时
// 一个请求都不发。
func TestRefreshCommands_ReregistersWithCurrentCatalog(t *testing.T) {
	type put struct {
		path string
		body []map[string]any
	}
	puts := make(chan put, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var cmds []map[string]any
		if err := json.Unmarshal(raw, &cmds); err != nil {
			t.Errorf("注册载荷不是命令数组: %v", err)
		}
		puts <- put{path: r.Method + " " + r.URL.Path, body: cmds}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	oldBase := apiBase
	apiBase = srv.URL
	defer func() { apiBase = oldBase }()

	var catalog atomic.Pointer[[]AgentOption]
	first := []AgentOption{{Agent: "codex", Models: []ModelOption{{ID: "gpt-5.5", Label: "GPT-5.5"}}}}
	catalog.Store(&first)
	svc, err := New(filepath.Join(t.TempDir(), "discord.json"), Deps{
		Catalog: func(context.Context) ([]AgentOption, error) { return *catalog.Load(), nil },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	modelValues := func(cmds []map[string]any) []string {
		for _, c := range cmds {
			if c["name"] != "model" {
				continue
			}
			opts := c["options"].([]any)
			choices, _ := opts[0].(map[string]any)["choices"].([]any)
			var out []string
			for _, ch := range choices {
				out = append(out, ch.(map[string]any)["value"].(string))
			}
			return out
		}
		t.Fatalf("载荷里没有 /model: %v", cmds)
		return nil
	}
	wait := func() put {
		select {
		case p := <-puts:
			return p
		case <-time.After(3 * time.Second):
			t.Fatal("等不到重注册请求")
			return put{}
		}
	}

	// 没连上：什么都不发。
	svc.RefreshCommands()
	select {
	case p := <-puts:
		t.Fatalf("未连接也发了注册: %s", p.path)
	case <-time.After(150 * time.Millisecond):
	}

	svc.mu.Lock()
	svc.gwCtx, svc.gwToken = context.Background(), "tok"
	svc.st.AppID = "app1"
	svc.st.Guilds = []Guild{{ID: "g1"}, {ID: "g2"}}
	svc.mu.Unlock()

	svc.RefreshCommands()
	got := map[string][]string{}
	for range 2 {
		p := wait()
		got[p.path] = modelValues(p.body)
	}
	for _, g := range []string{"g1", "g2"} {
		vals, ok := got["PUT /applications/app1/guilds/"+g+"/commands"]
		if !ok || len(vals) != 1 || vals[0] != "codex|gpt-5.5" {
			t.Fatalf("guild %s 的 /model choices = %v (present %v), want [codex|gpt-5.5]", g, vals, ok)
		}
	}

	// 清单变了再刷：新模型立刻进下拉，不必重连。
	second := []AgentOption{{Agent: "codex", Models: []ModelOption{
		{ID: "gpt-5.5", Label: "GPT-5.5"}, {ID: "glm-5.3-flash", Label: "GLM"},
	}}}
	catalog.Store(&second)
	svc.RefreshCommands()
	for range 2 {
		vals := modelValues(wait().body)
		if len(vals) != 2 || vals[1] != "codex|glm-5.3-flash" {
			t.Fatalf("重刷后 /model choices = %v, want 含 codex|glm-5.3-flash", vals)
		}
	}
}
