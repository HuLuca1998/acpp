package report

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// callOpen 走完整的 JSON-RPC 往返调一次 report_open，返回工具回给模型的
// 文本与是否工具级错误。
//
// 刻意不直接调私有的路径校验函数：模型看到的是这条协议往返的结果，
// 校验放行与否、错误包成什么形状，都得从这一层才测得准。
func callOpen(t *testing.T, svc *Service, cwd, path string) (string, bool) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{
			"name":      toolOpen,
			"arguments": map[string]any{"path": path},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, ok := svc.InspectMCP(context.Background(), cwd, raw)
	if !ok {
		t.Fatalf("tools/call 没有响应，path=%q", path)
	}
	// 经 JSON 往返再读，断言的就是真正发给 agent 的那份字节。
	blob, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(blob, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Result.Content) == 0 {
		t.Fatalf("响应里没有 content：%s", blob)
	}
	return out.Result.Content[0].Text, out.Result.IsError
}

// 路径护栏是这个工具唯一的安全边界：acpp 有局域网分享与租户（adr-007），
// 越过它就意味着租户能把 owner 机器上任意一个 .html 渲染到自己屏幕上。
// 所以用真实的临时目录、真实的文件和真实的软链接来测，不用桩。
func TestReportOpenPathGuard(t *testing.T) {
	svc := NewService(fakeSessions{token: "tok-1"}, "127.0.0.1:48080")
	cwd := t.TempDir()
	outside := t.TempDir()

	write := func(dir, name string) string {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("<html></html>"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write(cwd, "报告.html")
	write(cwd, "docs/周报.html")
	write(cwd, "data.json")
	secret := write(outside, "secret.html")

	// 工作目录里放一条指向外面的软链是完全合法的操作——只比字符串前缀的
	// 实现会被它整个绕过去，所以这条必须单独钉住。
	escapeOK := os.Symlink(secret, filepath.Join(cwd, "escape.html")) == nil

	allowed := []struct{ name, path string }{
		{"相对路径", "报告.html"},
		{"带 ./ 前缀", "./报告.html"},
		{"子目录", "docs/周报.html"},
		{"目录内的绝对路径", filepath.Join(cwd, "报告.html")},
	}
	for _, tc := range allowed {
		t.Run("放行/"+tc.name, func(t *testing.T) {
			text, isErr := callOpen(t, svc, cwd, tc.path)
			if isErr {
				t.Fatalf("期望放行，却被拒：%s", text)
			}
			// 回执要让模型知道「用户已经看到了」，否则它会把报告内容再
			// 复述一遍，等于白做一份 HTML。
			if !strings.Contains(text, "已在用户的工作区打开") {
				t.Errorf("回执 = %q，期望说明报告已经打开", text)
			}
		})
	}

	denied := []struct{ name, path, why string }{
		{"目录外的绝对路径", secret, "租户会话不能读 owner 机器上的任意文件"},
		{"相对路径往上爬", "../" + filepath.Base(outside) + "/secret.html", "../ 必须被解析后再判定"},
		{"非 html", "data.json", "这个工具只打开报告"},
		{"目录", "docs", "目录不是报告文件"},
		{"不存在的文件", "没有这个.html", "不存在就该说不存在"},
		{"空路径", "", "缺参数不能当成默认值放行"},
	}
	if escapeOK {
		denied = append(denied, struct{ name, path, why string }{
			"软链逃逸", "escape.html", "名字在目录内、实体在目录外，只比前缀会被绕过",
		})
	}
	for _, tc := range denied {
		t.Run("拒绝/"+tc.name, func(t *testing.T) {
			text, isErr := callOpen(t, svc, cwd, tc.path)
			if !isErr {
				t.Fatalf("期望被拒（%s），却放行了：%s", tc.why, text)
			}
		})
	}
}

// 挂载形状是两端 runtime 的硬约定，写错的症状是「模型看不见这个工具」
// 这种静默失败，所以两条方言分支都要钉住。
func TestMountsFor(t *testing.T) {
	svc := NewService(fakeSessions{token: "tok-1"}, "127.0.0.1:48080")

	t.Run("claude 走 _meta 且预批工具", func(t *testing.T) {
		servers, meta, err := svc.MountsFor(context.Background(), 7, t.TempDir(), "claude")
		if err != nil {
			t.Fatal(err)
		}
		if servers != nil {
			t.Errorf("claude 侧不该走 mcpServers 列表，得到 %v", servers)
		}
		cc, ok := meta["claudeCode"].(map[string]any)
		if !ok {
			t.Fatalf("meta 形状不对：%v", meta)
		}
		opts, ok := cc["options"].(map[string]any)
		if !ok {
			t.Fatalf("meta.claudeCode 形状不对：%v", cc)
		}
		srv, ok := opts["mcpServers"].(map[string]any)[mcpServerName].(map[string]any)
		if !ok {
			t.Fatalf("没挂上 %s：%v", mcpServerName, opts)
		}
		if url, _ := srv["url"].(string); !strings.HasSuffix(url, "/api/mcp/report/tok-1") {
			t.Errorf("回连地址 = %v，期望以 /api/mcp/report/tok-1 结尾", srv["url"])
		}
		allowed, _ := opts["allowedTools"].([]string)
		want := "mcp__" + mcpServerName + "__" + toolOpen
		if len(allowed) != 1 || allowed[0] != want {
			t.Errorf("allowedTools = %v，期望预批 %s——出报告是产出成果的最后一步，"+
				"在这里弹权限卡的话用户什么都看不到", allowed, want)
		}
	})

	t.Run("codex 走 mcpServers 列表", func(t *testing.T) {
		servers, meta, err := svc.MountsFor(context.Background(), 7, t.TempDir(), "codex")
		if err != nil {
			t.Fatal(err)
		}
		if meta != nil {
			t.Errorf("codex 侧不该走 _meta，得到 %v", meta)
		}
		if len(servers) != 1 {
			t.Fatalf("期望挂一个 server，得到 %v", servers)
		}
		if name, _ := servers[0].(map[string]any)["name"].(string); name != mcpServerName {
			t.Errorf("server name = %v，期望 %s", name, mcpServerName)
		}
	})

	t.Run("没有工作目录就不挂", func(t *testing.T) {
		// 路径护栏以 cwd 为准；没有 cwd，挂上去也只是让模型看到一个
		// 必然失败的工具。
		servers, meta, err := svc.MountsFor(context.Background(), 7, "  ", "claude")
		if err != nil || servers != nil || meta != nil {
			t.Errorf("空 cwd 应该什么都不挂，得到 servers=%v meta=%v err=%v", servers, meta, err)
		}
	})
}

type fakeSessions struct{ token string }

func (f fakeSessions) SessionByMCPToken(context.Context, string) (uint, string, error) {
	return 0, "", nil
}

func (f fakeSessions) EnsureMCPToken(context.Context, uint) (string, error) {
	return f.token, nil
}
