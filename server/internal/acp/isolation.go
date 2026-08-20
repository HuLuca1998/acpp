package acp

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
)

// 技能隔离：把机器级 skill 换成控制端自管的一套，工作目录项目级 skill 照常。
// 只作用于我们 spawn 的子进程与这条会话，不写 ~/.codex、~/.claude 一个字节。
// 两端机制不同——claude 走 session 参数 _meta，codex 走进程环境变量
// CODEX_HOME 重定向——差异全部收在各 adapter 的 Isolation。

// IsolationInput 是生成隔离注入的输入。
type IsolationInput struct {
	// SkillpackDir 是控制端技能包目录（<dataDir>/skillpack）。为空 = 不隔离。
	SkillpackDir string
	// Cwd 是会话工作目录，用于保留项目级 skill。
	Cwd string
	// Home 是用户家目录，用于定位系统 codex 的 auth/config（codex 隔离）。
	Home string
}

// Injection 是一条会话的隔离注入。Env 在 spawn 时追加到进程环境；
// Meta 与 AdditionalDirs 是 session/new 与 session/load 的协议参数。
type Injection struct {
	Env            map[string]string
	Meta           map[string]any
	AdditionalDirs []string
}

// Isolation（claude）：一切走 session/new 的 _meta。技能隔离在
// claudeCode.options 里——settingSources 只开 project 档（不开 user 档，
// 机器级 ~/.claude 全不加载）；plugins 以本地插件形式挂载控制端技能包；
// strictMcpConfig 封死项目 .mcp.json 的自动放行；外加
// CLAUDE_CODE_DISABLE_AUTO_MEMORY 防会话读写机器级记忆。
// 基础提示词走同级的 systemPrompt——**传对象 {append} 而不是字符串**，
// 后者会整体替换 claude_code 的 preset（实测），那等于把 agent 本体换掉。
//
// 提示词不受技能包有无的影响：没配技能包也照样注入，两者只是恰好共用注入口。
func (claudeAdapter) Isolation(in IsolationInput) Injection {
	inj := Injection{
		Env:  map[string]string{"CLAUDE_CODE_DISABLE_AUTO_MEMORY": "1"},
		Meta: map[string]any{"systemPrompt": map[string]any{"append": ClaudeInstructions()}},
	}
	if in.SkillpackDir == "" {
		return inj
	}
	inj.Meta["claudeCode"] = map[string]any{"options": map[string]any{
		"settingSources":  []string{"project"},
		"plugins":         []any{map[string]any{"type": "local", "path": in.SkillpackDir}},
		"strictMcpConfig": true,
	}}
	return inj
}

// Isolation（codex）：进程环境变量 CODEX_HOME 把 codex 的家目录整体重定向到
// <dataDir>/codex-home。机器级技能住在系统 ~/.codex/skills，换掉 home 后它们
// 彻底不在 codex 视野——连 /skills 命令都不再列出（会话级禁用做不到这点，
// CODEX_CONFIG 的 enabled=false 只挡使用不挡显示）。控制端技能包软链进
// codex-home/skills，项目级仍由会话 cwd 的 additionalDirectories 保留。
// 认证与 provider 配置软链/复制自系统 ~/.codex，基础提示词写 codex-home 的
// AGENTS.md（见 ensureCodexHome）。
func (codexAdapter) Isolation(in IsolationInput) Injection {
	if in.SkillpackDir == "" {
		return Injection{}
	}
	codexHome := filepath.Join(filepath.Dir(in.SkillpackDir), "codex-home")
	if err := ensureCodexHome(codexHome, in.SkillpackDir, in.Home); err != nil {
		// 搭 home 失败宁可不隔离，也不让会话起不来。
		return Injection{}
	}
	var dirs []string
	if in.Cwd != "" {
		dirs = []string{in.Cwd}
	}
	return Injection{
		Env:            map[string]string{"CODEX_HOME": codexHome},
		AdditionalDirs: dirs,
	}
}

// Isolation（generic）：认不出方言的 runtime 没有可靠的隔离注入口，不猜。
func (genericAdapter) Isolation(IsolationInput) Injection { return Injection{} }

// mergeMeta 把 extra 深合并进 base（map 递归合并，其余类型 extra 覆盖），
// 返回新 map，不改动入参——base 是会话间共享的隔离注入，改它会串会话。
func mergeMeta(base, extra map[string]any) map[string]any {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	out := make(map[string]any, len(base)+len(extra))
	maps.Copy(out, base)
	for k, v := range extra {
		bm, bok := out[k].(map[string]any)
		em, eok := v.(map[string]any)
		if bok && eok {
			out[k] = mergeMeta(bm, em)
			continue
		}
		out[k] = v
	}
	return out
}

// ensureCodexHome 幂等搭好隔离用的 codex 家目录：
//   - auth.json 软链系统的（codex 用静态 OPENAI_API_KEY，不改写它，软链跟随
//     系统登录态、不复制密钥）；
//   - config.toml 复制系统副本（codex 会往 config 写 trust_level 等，复制而非
//     软链，避免污染系统 config；已存在就不覆盖，保留 codex 写入的会话态）；
//   - skills 软链到 skillpack/skills（codex 只从这里发现技能，机器级不在视野）；
//   - AGENTS.md 写基础提示词（CodexInstructions）——codex 没有协议注入口，
//     session/new 的 _meta 只认 additionalRoots，家目录的 AGENTS.md 是唯一
//     稳定生效的口子（实测）。内容随版本变，每次比对覆盖，不做「已存在就跳过」。
//
// 系统 ~/.codex 缺 auth/config 时对应步骤跳过——未登录不是搭建失败。
func ensureCodexHome(codexHome, skillpackDir, sysHome string) error {
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		return err
	}

	// 家目录级 AGENTS.md：与 claude 的 systemPrompt.append 等价的注入口。
	// 用户项目里的 AGENTS.md 照常叠加，这里只补通用约定。
	instructions := filepath.Join(codexHome, "AGENTS.md")
	if cur, err := os.ReadFile(instructions); err != nil || string(cur) != CodexInstructions() {
		if err := os.WriteFile(instructions, []byte(CodexInstructions()), 0o644); err != nil {
			return err
		}
	}

	authLink := filepath.Join(codexHome, "auth.json")
	sysAuth := filepath.Join(sysHome, ".codex", "auth.json")
	if _, err := os.Lstat(sysAuth); err == nil {
		_ = os.Remove(authLink)
		if err := os.Symlink(sysAuth, authLink); err != nil {
			return err
		}
	}

	cfgCopy := filepath.Join(codexHome, "config.toml")
	if _, err := os.Stat(cfgCopy); errors.Is(err, os.ErrNotExist) {
		if raw, err := os.ReadFile(filepath.Join(sysHome, ".codex", "config.toml")); err == nil {
			if err := os.WriteFile(cfgCopy, raw, 0o600); err != nil {
				return err
			}
		}
	}

	skillsLink := filepath.Join(codexHome, "skills")
	target := filepath.Join(skillpackDir, "skills")
	if cur, err := os.Readlink(skillsLink); err != nil || cur != target {
		_ = os.Remove(skillsLink)
		if err := os.Symlink(target, skillsLink); err != nil {
			return err
		}
	}
	return nil
}
