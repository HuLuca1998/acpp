package system

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"acpp/server/internal/service"
)

// EnvDependency 是环境体检的一项：依赖是否就位、版本、怎么装。
type EnvDependency struct {
	Key       string `json:"key"`
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Path      string `json:"path,omitempty"`
	// InstallKind："auto" 可由后端一键安装；"manual" 需要用户在终端执行
	//（如 brew 本体要交互式输密码）；"bundled" 随其他依赖一起就位（npm 随 node）。
	InstallKind string `json:"installKind"`
	// InstallHint 是 manual 时给用户复制执行的命令。
	InstallHint string `json:"installHint,omitempty"`
	// Requires 是一键安装的前置依赖 key，未就位时前端禁用安装按钮。
	Requires string `json:"requires,omitempty"`
	// Latest 是包管理器上的最新版本；离线、或该项本就不查新版（brew/node/npm）
	// 时为空。
	Latest string `json:"latest,omitempty"`
	// Outdated 为真表示 Version 落后于 Latest，前端据此把按钮改成「更新」。
	Outdated bool `json:"outdated,omitempty"`
}

// EnvInfo 是环境体检结果。Path 是后端进程实际用的 PATH——排查「明明装了
// 却说没装」时先看它（GUI 启动的 app 与终端的 PATH 天然不同）。
type EnvInfo struct {
	Deps []EnvDependency `json:"deps"`
	Path string          `json:"path"`
}

// EnvInstallResult 是一次安装的结果；Ok=false 时 Output 里有失败输出。
type EnvInstallResult struct {
	Key    string `json:"key"`
	Ok     bool   `json:"ok"`
	Output string `json:"output"`
}

// envSpec 定义体检清单与安装方式。安装命令只认这张表，绝不拼接用户输入。
type envSpec struct {
	key       string
	binary    string
	kind      string // auto / manual / bundled
	installer string // auto 时用哪个包管理器：brew / npm
	// pkg 是 npm 全局包名，一物两用：既拼安装命令，也做查最新版的 registry
	// 键。只有 installer=npm 的项有；brew 系走 brewArgs。
	pkg      string
	brewArgs []string
	hint     string
	requires string
}

// installArgs 拼安装命令的参数。**npm 全局安装天然就是升级**——已装的包会被
// 拉到最新版覆盖，所以「安装」与「更新」共用这一条命令。
func (s envSpec) installArgs() []string {
	if s.pkg != "" {
		return []string{"install", "-g", s.pkg}
	}
	return s.brewArgs
}

// envSpecs 的顺序就是安装依赖链：brew → node(npm) → 各 CLI 与 ACP 适配器。
var envSpecs = []envSpec{
	{key: "brew", binary: "brew", kind: "manual",
		hint: `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`},
	{key: "node", binary: "node", kind: "auto",
		installer: "brew", brewArgs: []string{"install", "node"}, requires: "brew"},
	{key: "npm", binary: "npm", kind: "bundled", requires: "node"},
	{key: "claude-agent-acp", binary: "claude-agent-acp", kind: "auto",
		installer: "npm", pkg: "@agentclientprotocol/claude-agent-acp", requires: "npm"},
	{key: "claude", binary: "claude", kind: "auto",
		installer: "npm", pkg: "@anthropic-ai/claude-code", requires: "npm"},
	{key: "codex-acp", binary: "codex-acp", kind: "auto",
		installer: "npm", pkg: "@agentclientprotocol/codex-acp", requires: "npm"},
	{key: "codex", binary: "codex", kind: "auto",
		installer: "npm", pkg: "@openai/codex", requires: "npm"},
}

// EnvCheck 逐项探测依赖：存在性看 PATH 解析，版本用 --version 短超时读取
// （ACP 适配器是 stdio 服务，不支持 --version 时会挂住，靠超时兜底）。
// npm 系的项同时查 registry 上的最新版，落后的标 Outdated——适配器捆着自己
// 那份 Claude Agent SDK，版本旧了模型清单就跟着旧，光看「已安装」发现不了。
// refresh 为真时绕过最新版缓存（「重新检测」按钮）。
func (s *Service) EnvCheck(ctx context.Context, refresh bool) EnvInfo {
	pkgs := make([]string, 0, len(envSpecs))
	for _, spec := range envSpecs {
		if spec.pkg != "" {
			pkgs = append(pkgs, spec.pkg)
		}
	}
	// 查 registry 要联网，与本地探测并行跑，页面等的是两者里慢的那个。
	latestCh := make(chan map[string]string, 1)
	go func() { latestCh <- s.latest.versions(ctx, pkgs, refresh) }()

	info := EnvInfo{Deps: make([]EnvDependency, 0, len(envSpecs)), Path: os.Getenv("PATH")}
	for _, spec := range envSpecs {
		dep := EnvDependency{
			Key:         spec.key,
			InstallKind: spec.kind,
			InstallHint: spec.hint,
			Requires:    spec.requires,
		}
		if path, err := exec.LookPath(spec.binary); err == nil {
			dep.Installed = true
			dep.Path = path
			dep.Version = probeVersion(ctx, path)
		}
		info.Deps = append(info.Deps, dep)
	}

	latest := <-latestCh
	for i := range info.Deps {
		v := latest[envSpecs[i].pkg]
		if v == "" {
			continue
		}
		info.Deps[i].Latest = v
		// 本地版本读不出来时不判落后，免得把「读不到版本」误报成「有新版」。
		cur := parseVersion(info.Deps[i].Version)
		info.Deps[i].Outdated = info.Deps[i].Installed && cur != "" && compareVersions(cur, v) < 0
	}
	return info
}

// EnvInstall 一键安装白名单里的依赖，对已装的 npm 包即升级到最新版
// （见 envSpec.installArgs）。只接受 kind=auto 的 key，安装器缺位时报清晰的
// 前置错误。安装失败不算协议错误：Ok=false + 输出尾巴。
func (s *Service) EnvInstall(ctx context.Context, key string) (*EnvInstallResult, error) {
	var spec *envSpec
	for i := range envSpecs {
		if envSpecs[i].key == key {
			spec = &envSpecs[i]
			break
		}
	}
	if spec == nil {
		return nil, fmt.Errorf("%w: unknown dependency %q", service.ErrInvalid, key)
	}
	if spec.kind != "auto" {
		return nil, fmt.Errorf("%w: %s is not one-click installable", service.ErrInvalid, key)
	}
	installer, err := exec.LookPath(spec.installer)
	if err != nil {
		return nil, fmt.Errorf("%w: install %s first", service.ErrInvalid, spec.requires)
	}

	// brew install node 这类要下载编译产物，给足时间；超时靠 ctx 杀进程。
	installCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(installCtx, installer, spec.installArgs()...)
	out, runErr := cmd.CombinedOutput()
	return &EnvInstallResult{Key: key, Ok: runErr == nil, Output: tailString(string(out), 8000)}, nil
}

// probeVersion 读 `--version` 的首行；不支持该参数（或挂住）时返回空，
// 存在性结论不受影响。
func probeVersion(ctx context.Context, path string) string {
	verCtx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(verCtx, path, "--version")
	cmd.Stdin = nil
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(line)
}

func tailString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return "…" + s[len(s)-max:]
}
