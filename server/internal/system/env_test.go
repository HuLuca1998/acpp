package system

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"acpp/server/internal/config"

	"acpp/server/internal/service"
)

func envSvc() *Service {
	return NewService(nil, config.Config{})
}

// 契约：PATH 里有的依赖报已安装并带版本与路径，没有的报未安装且
// manual 项带可复制的安装引导；清单覆盖固定的依赖链且顺序稳定。
func TestService_EnvCheck_ReportsInstalledAndMissing(t *testing.T) {
	bin := t.TempDir()
	fakeNode := filepath.Join(bin, "node")
	if err := os.WriteFile(fakeNode, []byte("#!/bin/sh\necho v22.17.0\n"), 0o755); err != nil {
		t.Fatalf("write fake node: %v", err)
	}
	t.Setenv("PATH", bin)

	info := envSvc().EnvCheck(context.Background(), false)

	wantOrder := []string{"brew", "node", "npm", "claude-agent-acp", "claude", "codex-acp", "codex"}
	if len(info.Deps) != len(wantOrder) {
		t.Fatalf("deps = %d, want %d: %+v", len(info.Deps), len(wantOrder), info.Deps)
	}
	byKey := map[string]EnvDependency{}
	for i, dep := range info.Deps {
		if dep.Key != wantOrder[i] {
			t.Errorf("deps[%d] = %s, want %s", i, dep.Key, wantOrder[i])
		}
		byKey[dep.Key] = dep
	}

	node := byKey["node"]
	if !node.Installed || node.Version != "v22.17.0" || node.Path != fakeNode {
		t.Errorf("node = %+v, want installed v22.17.0 at %s", node, fakeNode)
	}
	brew := byKey["brew"]
	if brew.Installed || brew.InstallKind != "manual" || brew.InstallHint == "" {
		t.Errorf("brew = %+v, want missing + manual 引导命令", brew)
	}
	if info.Path != bin {
		t.Errorf("Path = %q, want %q", info.Path, bin)
	}
}

// 契约：安装只认白名单——未知 key、manual/bundled 项、前置安装器缺位
// 一律 service.ErrInvalid，不执行任何命令。
func TestService_EnvInstall_RejectsOutsideAllowlist(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // 空 PATH：连 brew/npm 都不存在

	svc := envSvc()
	cases := []struct {
		label string
		key   string
	}{
		{"未知 key", "rm -rf /"},
		{"manual 项", "brew"},
		{"bundled 项", "npm"},
		{"前置缺位（npm 不在）", "codex-acp"},
		{"前置缺位（brew 不在）", "node"},
	}
	for _, tc := range cases {
		if _, err := svc.EnvInstall(context.Background(), tc.key); !errors.Is(err, service.ErrInvalid) {
			t.Errorf("%s: err = %v, want service.ErrInvalid", tc.label, err)
		}
	}
}

// 契约：安装器存在时执行对应命令并回传输出；命令失败不是协议错误，
// Ok=false 且输出可供排查。
func TestService_EnvInstall_RunsInstallerAndReportsFailure(t *testing.T) {
	bin := t.TempDir()
	// 伪 npm：把收到的参数回显后失败，验证「命令来自白名单表」与失败通路。
	fakeNpm := filepath.Join(bin, "npm")
	script := "#!/bin/sh\necho \"npm called: $@\"\nexit 1\n"
	if err := os.WriteFile(fakeNpm, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake npm: %v", err)
	}
	t.Setenv("PATH", bin)

	res, err := envSvc().EnvInstall(context.Background(), "codex-acp")
	if err != nil {
		t.Fatalf("EnvInstall: %v", err)
	}
	if res.Ok {
		t.Fatalf("Ok = true, want false（伪 npm exit 1）")
	}
	want := "npm called: install -g @agentclientprotocol/codex-acp"
	if !strings.Contains(res.Output, want) {
		t.Errorf("output = %q, want contains %q", res.Output, want)
	}
}

// writeScript 在 dir 下放一个可执行的假二进制，供体检解析 PATH 与读版本。
func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
	return path
}

// 契约：npm 系依赖查 registry 标出最新版，本地版本确实落后才标 Outdated。
// 版本读不出来的、没装的都不判落后——把「读不到版本」误报成「有新版」会
// 让用户对着已是最新的依赖反复点更新。
func TestService_EnvCheck_FlagsOutdated(t *testing.T) {
	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// scoped 包名里的 %2F 到这里已被解码回斜杠，按包名片段分派即可。
		var version string
		switch {
		case strings.Contains(r.URL.Path, "claude-agent-acp"):
			version = "0.73.0"
		case strings.Contains(r.URL.Path, "claude-code"):
			version = "2.1.258"
		case strings.Contains(r.URL.Path, "codex-acp"):
			version = "1.4.0"
		case strings.Contains(r.URL.Path, "codex"):
			version = "0.152.1"
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"version":"` + version + `"}`))
	}))
	defer reg.Close()

	bin := t.TempDir()
	// 伪 npm：任何参数都回显测试 registry 地址，让 config get registry 指向它。
	writeScript(t, bin, "npm", "#!/bin/sh\necho "+reg.URL+"\n")
	writeScript(t, bin, "claude-agent-acp", "#!/bin/sh\necho 0.70.0\n")
	// 带后缀的版本串是 claude CLI 的真实格式，必须能抠出来比较。
	writeScript(t, bin, "claude", "#!/bin/sh\necho '2.1.235 (Claude Code)'\n")
	// 版本读不出来（有的适配器不认 --version）：可以报最新版，但不能判落后。
	writeScript(t, bin, "codex-acp", "#!/bin/sh\nexit 1\n")
	t.Setenv("PATH", bin)

	info := envSvc().EnvCheck(context.Background(), true)

	byKey := map[string]EnvDependency{}
	for _, dep := range info.Deps {
		byKey[dep.Key] = dep
	}
	cases := []struct {
		key          string
		wantLatest   string
		wantOutdated bool
		why          string
	}{
		{"claude-agent-acp", "0.73.0", true, "0.70.0 落后于 0.73.0"},
		{"claude", "2.1.258", true, "带后缀的 2.1.235 也要能比出落后"},
		{"codex-acp", "1.4.0", false, "版本读不出来，不判落后"},
		{"codex", "0.152.1", false, "没装，不判落后"},
		{"node", "", false, "brew 系不查 npm registry"},
	}
	for _, tc := range cases {
		dep := byKey[tc.key]
		if dep.Latest != tc.wantLatest || dep.Outdated != tc.wantOutdated {
			t.Errorf("%s: Latest=%q Outdated=%v, want %q/%v（%s）",
				tc.key, dep.Latest, dep.Outdated, tc.wantLatest, tc.wantOutdated, tc.why)
		}
	}
}
