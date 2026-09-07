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

// envSvc 造一个最新版查询指向本地假服务器的 Service。**测试不许真去打
// formulae.brew.sh / npm registry**：体检的本地结论跟网络无关，让它联网
// 只会换来一堆 6 秒超时和看天吃饭的结果。
func envSvc(t *testing.T) *Service {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(ts.Close)
	svc := NewService(nil, config.Config{})
	svc.latest.brewAPI = ts.URL
	return svc
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

	info := envSvc(t).EnvCheck(context.Background(), false)

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
	// 依赖链的前置声明是前端禁用安装按钮的依据：走 brew 的项前置是 brew，
	// 只能走 npm 的那项前置才是 npm。
	if got := byKey["codex"].Requires; got != "brew" {
		t.Errorf("codex.Requires = %q, want brew", got)
	}
	if got := byKey["claude-agent-acp"].Requires; got != "npm" {
		t.Errorf("claude-agent-acp.Requires = %q, want npm", got)
	}
	if info.Path != bin {
		t.Errorf("Path = %q, want %q", info.Path, bin)
	}
}

// 契约：安装只认白名单——未知 key、manual/bundled 项、前置安装器缺位
// 一律 service.ErrInvalid，不执行任何命令。
func TestService_EnvInstall_RejectsOutsideAllowlist(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // 空 PATH：连 brew/npm 都不存在

	svc := envSvc(t)
	cases := []struct {
		label string
		key   string
	}{
		{"未知 key", "rm -rf /"},
		{"manual 项", "brew"},
		{"bundled 项", "npm"},
		{"前置缺位（npm 不在）", "claude-agent-acp"},
		{"前置缺位（brew 不在）", "codex-acp"},
		{"前置缺位（brew 不在）", "node"},
	}
	for _, tc := range cases {
		if _, err := svc.EnvInstall(context.Background(), tc.key); !errors.Is(err, service.ErrInvalid) {
			t.Errorf("%s: err = %v, want service.ErrInvalid", tc.label, err)
		}
	}
}

// 契约：只能走 npm 的项（brew 里没有对应包）用 npm 全局安装；命令失败不是
// 协议错误，Ok=false 且输出可供排查。
func TestService_EnvInstall_RunsNpmAndReportsFailure(t *testing.T) {
	bin := t.TempDir()
	// 伪 npm：把收到的参数回显后失败，验证「命令来自白名单表」与失败通路。
	writeScript(t, bin, "npm", "#!/bin/sh\necho \"npm called: $@\"\nexit 1\n")
	t.Setenv("PATH", bin)

	res, err := envSvc(t).EnvInstall(context.Background(), "claude-agent-acp")
	if err != nil {
		t.Fatalf("EnvInstall: %v", err)
	}
	if res.Ok {
		t.Fatalf("Ok = true, want false（伪 npm exit 1）")
	}
	want := "npm called: install -g @agentclientprotocol/claude-agent-acp"
	if !strings.Contains(res.Output, want) {
		t.Errorf("output = %q, want contains %q", res.Output, want)
	}
}

// 契约：brew 系的项按「装没装」选动词——没装 install、已装 upgrade，cask
// 带 --cask。brew 对已装的包 install 只会回一句 already installed 就退出，
// 用错动词等于点了更新却什么都没发生。
func TestService_EnvInstall_UsesBrewInstallOrUpgrade(t *testing.T) {
	bin := t.TempDir()
	writeScript(t, bin, "brew", "#!/bin/sh\necho \"brew called: $@\"\n")
	// codex 已装（普通文件，不是 npm 那种指向 node_modules 的软链）；
	// codex-acp 没装。
	writeScript(t, bin, "codex", "#!/bin/sh\necho codex-cli 0.153.4\n")
	t.Setenv("PATH", bin)

	svc := envSvc(t)
	cases := []struct {
		key  string
		want string
	}{
		{"codex", "brew called: upgrade --cask codex"},
		{"codex-acp", "brew called: install codex-acp"},
	}
	for _, tc := range cases {
		res, err := svc.EnvInstall(context.Background(), tc.key)
		if err != nil {
			t.Fatalf("EnvInstall(%s): %v", tc.key, err)
		}
		if !res.Ok {
			t.Errorf("%s: Ok = false, output = %q", tc.key, res.Output)
		}
		if !strings.Contains(res.Output, tc.want) {
			t.Errorf("%s: output = %q, want contains %q", tc.key, res.Output, tc.want)
		}
	}
}

// 契约：已改由 brew 管的项，如果现场那个命令还是早年 npm 全局装的（软链
// 指向 node_modules），体检要给出腾位置的命令，一键安装则直接拒绝——两个
// 包管理器争同一个 bin 名，硬装下去只会撞一脸 EEXIST。
func TestService_EnvCheck_FlagsStaleNpmInstall(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	mod := filepath.Join(root, "lib", "node_modules", "@anthropic-ai", "claude-code", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.MkdirAll(mod, 0o755); err != nil {
		t.Fatalf("mkdir node_modules: %v", err)
	}
	// npm 全局安装的真实形状：<prefix>/bin/claude 是指向
	// <prefix>/lib/node_modules/<pkg>/... 的软链。
	real := writeScript(t, mod, "claude.js", "#!/bin/sh\necho '2.1.235 (Claude Code)'\n")
	if err := os.Symlink(real, filepath.Join(bin, "claude")); err != nil {
		t.Fatalf("symlink claude: %v", err)
	}
	// brew 本体得在，否则 EnvInstall 先卡在前置依赖上，测不到迁移拦截。
	writeScript(t, bin, "brew", "#!/bin/sh\necho \"brew called: $@\"\n")
	// codex 是 brew 装的形状（普通文件），不该被误判成 npm 遗留。
	writeScript(t, bin, "codex", "#!/bin/sh\necho codex-cli 0.153.4\n")
	t.Setenv("PATH", bin)

	svc := envSvc(t)
	info := svc.EnvCheck(context.Background(), true)
	byKey := map[string]EnvDependency{}
	for _, dep := range info.Deps {
		byKey[dep.Key] = dep
	}
	if got := byKey["claude"].MigrateHint; got != "npm uninstall -g @anthropic-ai/claude-code" {
		t.Errorf("claude.MigrateHint = %q, want 清理旧 npm 安装的命令", got)
	}
	if got := byKey["codex"].MigrateHint; got != "" {
		t.Errorf("codex.MigrateHint = %q, want 空（brew 装的不算遗留）", got)
	}

	if _, err := svc.EnvInstall(context.Background(), "claude"); !errors.Is(err, service.ErrInvalid) {
		t.Errorf("EnvInstall(claude) err = %v, want service.ErrInvalid（先清 npm 再装）", err)
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

// 契约：npm 系查 registry、brew 系查 Homebrew API，本地版本确实落后才标
// Outdated。版本读不出来的、没装的都不判落后——把「读不到版本」误报成
// 「有新版」会让用户对着已是最新的依赖反复点更新。node 不跟新版。
func TestService_EnvCheck_FlagsOutdated(t *testing.T) {
	// 一个服务器同时扮 npm registry 与 Homebrew API，按路径分派。
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/cask/claude-code.json"):
			_, _ = w.Write([]byte(`{"token":"claude-code","version":"2.1.236"}`))
		case strings.Contains(r.URL.Path, "/cask/codex.json"):
			// cask 版本常带 ",build" 后缀，比较时只取前半段。
			_, _ = w.Write([]byte(`{"token":"codex","version":"0.153.4,4210"}`))
		case strings.Contains(r.URL.Path, "/formula/codex-acp.json"):
			_, _ = w.Write([]byte(`{"name":"codex-acp","versions":{"stable":"1.10.0"}}`))
		// scoped 包名里的 %2F 到这里已被解码回斜杠，按包名片段分派即可。
		case strings.Contains(r.URL.Path, "claude-agent-acp"):
			_, _ = w.Write([]byte(`{"version":"0.73.0"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer api.Close()

	bin := t.TempDir()
	// 伪 npm：任何参数都回显测试地址，让 config get registry 指向它。
	writeScript(t, bin, "npm", "#!/bin/sh\necho "+api.URL+"\n")
	writeScript(t, bin, "claude-agent-acp", "#!/bin/sh\necho 0.70.0\n")
	// 带后缀的版本串是 claude CLI 的真实格式，必须能抠出来比较。
	writeScript(t, bin, "claude", "#!/bin/sh\necho '2.1.235 (Claude Code)'\n")
	// 版本读不出来（有的适配器不认 --version）：可以报最新版，但不能判落后。
	writeScript(t, bin, "codex-acp", "#!/bin/sh\nexit 1\n")
	writeScript(t, bin, "node", "#!/bin/sh\necho v22.17.0\n")
	t.Setenv("PATH", bin)

	svc := NewService(nil, config.Config{})
	svc.latest.brewAPI = api.URL
	info := svc.EnvCheck(context.Background(), true)

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
		{"claude-agent-acp", "0.73.0", true, "npm 系：0.70.0 落后于 0.73.0"},
		{"claude", "2.1.236", true, "cask：带后缀的 2.1.235 也要能比出落后"},
		{"codex-acp", "1.10.0", false, "formula：版本读不出来，不判落后"},
		{"codex", "0.153.4", false, "没装，不判落后；cask 的 build 后缀已剥掉"},
		{"node", "", false, "node 不跟新版"},
	}
	for _, tc := range cases {
		dep := byKey[tc.key]
		if dep.Latest != tc.wantLatest || dep.Outdated != tc.wantOutdated {
			t.Errorf("%s: Latest=%q Outdated=%v, want %q/%v（%s）",
				tc.key, dep.Latest, dep.Outdated, tc.wantLatest, tc.wantOutdated, tc.why)
		}
	}
}
