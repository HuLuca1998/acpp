package acp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// 技能隔离：把机器级 skill 换成控制端自管的一套，工作目录项目级 skill 照常。
// 只作用于我们 spawn 的子进程与这条会话，不写 ~/.codex、~/.claude 一个字节。
// 两端机制不同——claude 走 session 参数 _meta，codex 走进程环境变量
// CODEX_CONFIG + additionalDirectories——差异全部收在各 adapter 的 Isolation。

// IsolationInput 是生成隔离注入的输入。
type IsolationInput struct {
	// SkillpackDir 是控制端技能包目录（<dataDir>/skillpack）。为空 = 不隔离。
	SkillpackDir string
	// Cwd 是会话工作目录，用于保留项目级 skill。
	Cwd string
	// Home 是用户家目录，用于枚举机器级 skill 逐个禁用（codex）。
	Home string
}

// Injection 是一条会话的隔离注入。Env 在 spawn 时追加到进程环境；
// Meta 与 AdditionalDirs 是 session/new 与 session/load 的协议参数。
type Injection struct {
	Env            map[string]string
	Meta           map[string]any
	AdditionalDirs []string
}

// Isolation（claude）：一切走 session/new 的 _meta.claudeCode.options。
// settingSources 只开 project 档——不开 user 档，机器级 ~/.claude 全不加载；
// plugins 以本地插件形式挂载控制端技能包；strictMcpConfig 封死项目 .mcp.json
// 的自动放行。外加 CLAUDE_CODE_DISABLE_AUTO_MEMORY 防会话读写机器级记忆。
func (claudeAdapter) Isolation(in IsolationInput) Injection {
	if in.SkillpackDir == "" {
		return Injection{}
	}
	return Injection{
		Env: map[string]string{"CLAUDE_CODE_DISABLE_AUTO_MEMORY": "1"},
		Meta: map[string]any{"claudeCode": map[string]any{"options": map[string]any{
			"settingSources":  []string{"project"},
			"plugins":         []any{map[string]any{"type": "local", "path": in.SkillpackDir}},
			"strictMcpConfig": true,
		}}},
	}
}

// Isolation（codex）：进程环境变量 CODEX_CONFIG 枚举 ~/.codex/skills 逐个
// 禁用（实测只有 frontmatter name 选择器有效，会话级覆盖不落地文件）；
// additionalDirectories 让 codex-acp 把技能包与工作目录的 .agents/skills
// 注册为 skill 根。技能隔离不碰 MCP，mcp_servers 给空对象。
func (codexAdapter) Isolation(in IsolationInput) Injection {
	if in.SkillpackDir == "" {
		return Injection{}
	}
	dirs := []string{in.SkillpackDir}
	if in.Cwd != "" {
		dirs = append(dirs, in.Cwd)
	}
	return Injection{
		Env:            map[string]string{"CODEX_CONFIG": codexDisableMachineSkills(in.Home)},
		AdditionalDirs: dirs,
	}
}

// Isolation（generic）：认不出方言的 runtime 没有可靠的隔离注入口，不猜。
func (genericAdapter) Isolation(IsolationInput) Injection { return Injection{} }

// codexDisableMachineSkills 生成 CODEX_CONFIG 的 JSON：枚举 <home>/.codex/skills
// 下每个技能的 frontmatter name，逐个 enabled=false。读不到目录时返回只含空
// 清单的配置（仍是合法覆盖，不报错）。
func codexDisableMachineSkills(home string) string {
	entries := []map[string]any{}
	root := filepath.Join(home, ".codex", "skills")
	if dirs, err := os.ReadDir(root); err == nil {
		for _, d := range dirs {
			if !d.IsDir() || strings.HasPrefix(d.Name(), ".") {
				continue
			}
			entries = append(entries, map[string]any{
				"name":    skillFrontmatterName(filepath.Join(root, d.Name())),
				"enabled": false,
			})
		}
	}
	cfg, _ := json.Marshal(map[string]any{
		"skills":      map[string]any{"config": entries},
		"mcp_servers": map[string]any{},
	})
	return string(cfg)
}

// skillFrontmatterName 读 SKILL.md frontmatter 的 name（禁用选择器按它匹配，
// 不是目录名）。读不到就退回目录名。
func skillFrontmatterName(dir string) string {
	raw, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return filepath.Base(dir)
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		if after, ok := strings.CutPrefix(strings.TrimSpace(line), "name:"); ok {
			return strings.Trim(strings.TrimSpace(after), `"'`)
		}
	}
	return filepath.Base(dir)
}
