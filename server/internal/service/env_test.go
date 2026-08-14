package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"acpp/server/internal/config"
)

func envSvc() *SystemService {
	return NewSystemService(nil, config.Config{})
}

// 契约：PATH 里有的依赖报已安装并带版本与路径，没有的报未安装且
// manual 项带可复制的安装引导；清单覆盖固定的依赖链且顺序稳定。
func TestSystemService_EnvCheck_ReportsInstalledAndMissing(t *testing.T) {
	bin := t.TempDir()
	fakeNode := filepath.Join(bin, "node")
	if err := os.WriteFile(fakeNode, []byte("#!/bin/sh\necho v22.17.0\n"), 0o755); err != nil {
		t.Fatalf("write fake node: %v", err)
	}
	t.Setenv("PATH", bin)

	info := envSvc().EnvCheck(context.Background())

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
// 一律 ErrInvalid，不执行任何命令。
func TestSystemService_EnvInstall_RejectsOutsideAllowlist(t *testing.T) {
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
		if _, err := svc.EnvInstall(context.Background(), tc.key); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", tc.label, err)
		}
	}
}

// 契约：安装器存在时执行对应命令并回传输出；命令失败不是协议错误，
// Ok=false 且输出可供排查。
func TestSystemService_EnvInstall_RunsInstallerAndReportsFailure(t *testing.T) {
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
