package system

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	// MigrateHint 非空表示现场这个命令还是早年 npm 全局安装留下的，而本项
	// 已改由 Homebrew 管：两边争同一个 bin 名，谁也装不进谁，必须先跑这条
	// 命令腾位置。此时一键安装/更新禁用。
	MigrateHint string `json:"migrateHint,omitempty"`
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
	key    string
	binary string
	kind   string // auto / manual / bundled
	// brewPkg 是 Homebrew 包名，brewCask 区分 cask 与 formula。**它优先于
	// npmPkg**：能从 brew 装的一律走 brew，版本来源单一，不会两个包管理器
	// 抢同一个命令名。
	brewPkg  string
	brewCask bool
	// npmPkg 是 npm 全局包名。brewPkg 为空时它是安装源（brew 里没有对应包，
	// 只能走 npm）；两者都有时它只用来生成清理旧 npm 安装的迁移命令。
	npmPkg string
	// noLatest 关掉最新版跟踪。node 只要能跑起 npm 就够，追新只会让体检页
	// 常年挂一条「有新版」的噪音——本地装的多半是 node@22 这类固定大版本，
	// 跟 brew 一路追到 26 的 node 压根不是同一个 formula。
	noLatest bool
	hint     string
	requires string
}

// installer 是这一项该用哪个包管理器；两者都没有说明不可一键安装。
func (s envSpec) installer() string {
	switch {
	case s.brewPkg != "":
		return "brew"
	case s.npmPkg != "":
		return "npm"
	default:
		return ""
	}
}

// pkgArgs 拼安装命令的参数。**npm 全局安装天然就是升级**——已装的包会被
// 拉到最新版覆盖，所以一条命令通吃；brew 则分家：对已装的包 install 只会
// 回一句「already installed」就退出，升级必须走 upgrade。
func (s envSpec) pkgArgs(installed bool) []string {
	if s.installer() == "npm" {
		return []string{"install", "-g", s.npmPkg}
	}
	verb := "install"
	if installed {
		verb = "upgrade"
	}
	if s.brewCask {
		return []string{verb, "--cask", s.brewPkg}
	}
	return []string{verb, s.brewPkg}
}

// envSpecs 的顺序就是安装依赖链：brew → node(npm) → 各 CLI 与 ACP 适配器。
// 除 claude-agent-acp 外全部走 Homebrew——它在 brew 里没有对应包，只能留在
// npm 上，node/npm 这两级依赖也就是为它保留的。
var envSpecs = []envSpec{
	{key: "brew", binary: "brew", kind: "manual",
		hint: `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`},
	{key: "node", binary: "node", kind: "auto",
		brewPkg: "node", noLatest: true, requires: "brew"},
	{key: "npm", binary: "npm", kind: "bundled", requires: "node"},
	{key: "claude-agent-acp", binary: "claude-agent-acp", kind: "auto",
		npmPkg: "@agentclientprotocol/claude-agent-acp", requires: "npm"},
	{key: "claude", binary: "claude", kind: "auto",
		brewPkg: "claude-code", brewCask: true,
		npmPkg: "@anthropic-ai/claude-code", requires: "brew"},
	{key: "codex-acp", binary: "codex-acp", kind: "auto",
		brewPkg: "codex-acp",
		npmPkg:  "@agentclientprotocol/codex-acp", requires: "brew"},
	{key: "codex", binary: "codex", kind: "auto",
		brewPkg: "codex", brewCask: true,
		npmPkg: "@openai/codex", requires: "brew"},
}

// EnvCheck 逐项探测依赖：存在性看 PATH 解析，版本用 --version 短超时读取
// （ACP 适配器是 stdio 服务，不支持 --version 时会挂住，靠超时兜底）。
// 同时查包管理器上的最新版，落后的标 Outdated——适配器捆着自己那份 Claude
// Agent SDK，版本旧了模型清单就跟着旧，光看「已安装」发现不了。
// refresh 为真时绕过最新版缓存（「重新检测」按钮）。
func (s *Service) EnvCheck(ctx context.Context, refresh bool) EnvInfo {
	// 查包管理器要联网，与本地探测并行跑，页面等的是两者里慢的那个。
	latestCh := make(chan map[string]string, 1)
	go func() { latestCh <- s.latest.versions(ctx, envSpecs, refresh) }()

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
			if spec.staleNpm(path) {
				dep.MigrateHint = "npm uninstall -g " + spec.npmPkg
			}
		}
		info.Deps = append(info.Deps, dep)
	}

	latest := <-latestCh
	for i := range info.Deps {
		v := latest[info.Deps[i].Key]
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

// staleNpm 报告 path 这个命令是不是本项遗留的 npm 全局安装——仅对已改由
// brew 管的项有意义（两边都有包名）。
func (s envSpec) staleNpm(path string) bool {
	return s.brewPkg != "" && s.npmPkg != "" && npmOwned(path)
}

// npmOwned 判断可执行文件是否由 npm 全局安装管理：npm 在 <prefix>/bin 下建的
// 是指向 <prefix>/lib/node_modules/... 的软链，解析到底必然经过 node_modules；
// Homebrew 装的则落在 Cellar/ 或 Caskroom/ 里，一眼可分。解析不了（断链、
// 或压根不是软链）一律当作不是 npm 的——宁可漏报也不误伤。
func npmOwned(path string) bool {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	return strings.Contains(real, "/node_modules/")
}

// EnvInstall 一键安装白名单里的依赖，对已装的即升级到最新版（见 pkgArgs）。
// 只接受 kind=auto 的 key，安装器缺位时报清晰的前置错误。安装失败不算协议
// 错误：Ok=false + 输出尾巴。
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
	installer, err := exec.LookPath(spec.installer())
	if err != nil {
		return nil, fmt.Errorf("%w: install %s first", service.ErrInvalid, spec.requires)
	}
	// 装没装决定 brew 用 install 还是 upgrade；顺带拦下还被 npm 占着命令名
	// 的项——直接装下去只会撞一脸 EEXIST，不如把腾位置的命令告诉用户。
	path, lookErr := exec.LookPath(spec.binary)
	installed := lookErr == nil
	if installed && spec.staleNpm(path) {
		return nil, fmt.Errorf("%w: %s 现在是 npm 全局安装的（%s），与 Homebrew 争同一个命令名；请先在终端执行 npm uninstall -g %s，再回来安装",
			service.ErrInvalid, key, path, spec.npmPkg)
	}

	// brew install node 这类要下载编译产物，给足时间；超时靠 ctx 杀进程。
	installCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(installCtx, installer, spec.pkgArgs(installed)...)
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
