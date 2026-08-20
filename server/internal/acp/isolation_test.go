package acp

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// 契约：claude 的隔离全部走 session 参数 _meta.claudeCode.options——只开
// project 档（屏蔽机器级、保留项目级）、以本地插件挂载技能包、strictMcpConfig；
// 外加禁自动记忆的进程环境变量。不产生 additionalDirectories。
func TestClaudeAdapter_Isolation_MetaShape(t *testing.T) {
	inj := claudeAdapter{}.Isolation(IsolationInput{
		SkillpackDir: "/data/skillpack",
		Cwd:          "/work/proj",
		Home:         "/home/u",
	})

	if inj.AdditionalDirs != nil {
		t.Fatalf("claude must not use additionalDirectories, got %v", inj.AdditionalDirs)
	}
	if inj.Env["CLAUDE_CODE_DISABLE_AUTO_MEMORY"] != "1" {
		t.Fatalf("env = %v, want CLAUDE_CODE_DISABLE_AUTO_MEMORY=1", inj.Env)
	}

	opts := inj.Meta["claudeCode"].(map[string]any)["options"].(map[string]any)
	if got := opts["settingSources"]; !reflect.DeepEqual(got, []string{"project"}) {
		t.Fatalf("settingSources = %v, want [project] (user 档不开=屏蔽机器级)", got)
	}
	if opts["strictMcpConfig"] != true {
		t.Fatalf("strictMcpConfig = %v, want true", opts["strictMcpConfig"])
	}
	plugin := opts["plugins"].([]any)[0].(map[string]any)
	if plugin["type"] != "local" || plugin["path"] != "/data/skillpack" {
		t.Fatalf("plugin = %v, want local plugin at skillpack dir", plugin)
	}

	// 基础提示词是**追加**：传字符串会整体替换 claude_code preset。
	sp, ok := inj.Meta["systemPrompt"].(map[string]any)
	if !ok {
		t.Fatalf("systemPrompt = %v, want object form {append: ...}", inj.Meta["systemPrompt"])
	}
	if sp["append"] != ClaudeInstructions() {
		t.Fatalf("systemPrompt.append = %v, want ClaudeInstructions()", sp["append"])
	}
}

// 契约：没配技能包也要注入基础提示词——两者只是共用注入口，提示词不是隔离
// 的附属品。此时 claudeCode.options 整块不出现（没有技能包可挂）。
func TestClaudeAdapter_Isolation_InstructionsWithoutSkillpack(t *testing.T) {
	inj := claudeAdapter{}.Isolation(IsolationInput{Cwd: "/work/proj", Home: "/home/u"})

	sp, ok := inj.Meta["systemPrompt"].(map[string]any)
	if !ok || sp["append"] != ClaudeInstructions() {
		t.Fatalf("systemPrompt = %v, want {append: ClaudeInstructions()}", inj.Meta["systemPrompt"])
	}
	if _, ok := inj.Meta["claudeCode"]; ok {
		t.Fatalf("claudeCode options must be absent without a skillpack, got %v", inj.Meta["claudeCode"])
	}
}

// 契约：codex 的隔离用 CODEX_HOME 把家目录重定向到 <dataDir>/codex-home，
// 让机器级技能彻底不在视野（连 /skills 都不列）；家目录里 auth.json 软链
// 系统的、config.toml 复制系统副本、skills 软链到 skillpack/skills。
// additionalDirectories 只保留工作目录（项目级），控制端技能走 codex-home。
// 不产生 _meta。
func TestCodexAdapter_Isolation_RedirectsCodexHome(t *testing.T) {
	dataDir := t.TempDir()
	skillpack := filepath.Join(dataDir, "skillpack")
	if err := os.MkdirAll(filepath.Join(skillpack, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 系统 ~/.codex 的 auth/config，供软链与复制。
	sysHome := t.TempDir()
	sysCodex := filepath.Join(sysHome, ".codex")
	if err := os.MkdirAll(sysCodex, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sysCodex, "auth.json"), []byte(`{"OPENAI_API_KEY":"sk-x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sysCodex, "config.toml"), []byte("model = \"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	inj := codexAdapter{}.Isolation(IsolationInput{
		SkillpackDir: skillpack,
		Cwd:          "/work/proj",
		Home:         sysHome,
	})

	if inj.Meta != nil {
		t.Fatalf("codex must not use _meta, got %v", inj.Meta)
	}
	// 只保留项目级 cwd；控制端技能不再走 additionalDirectories。
	if !reflect.DeepEqual(inj.AdditionalDirs, []string{"/work/proj"}) {
		t.Fatalf("additionalDirs = %v, want [cwd] only", inj.AdditionalDirs)
	}

	codexHome := filepath.Join(dataDir, "codex-home")
	if inj.Env["CODEX_HOME"] != codexHome {
		t.Fatalf("CODEX_HOME = %q, want %q", inj.Env["CODEX_HOME"], codexHome)
	}

	// auth.json 软链系统的（不复制密钥）。
	authTarget, err := os.Readlink(filepath.Join(codexHome, "auth.json"))
	if err != nil || authTarget != filepath.Join(sysCodex, "auth.json") {
		t.Fatalf("auth.json link = %q (err %v), want symlink to system auth", authTarget, err)
	}
	// config.toml 复制成独立副本（不是软链——避免 codex 写回污染系统）。
	if fi, err := os.Lstat(filepath.Join(codexHome, "config.toml")); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("config.toml must be a real copy, not a symlink (err %v)", err)
	}
	// skills 软链到 skillpack/skills（codex 只从这里发现技能）。
	skillsTarget, err := os.Readlink(filepath.Join(codexHome, "skills"))
	if err != nil || skillsTarget != filepath.Join(skillpack, "skills") {
		t.Fatalf("skills link = %q (err %v), want symlink to skillpack/skills", skillsTarget, err)
	}
	// AGENTS.md 是 codex 唯一的提示词注入口（它没有协议注入口）。
	got, err := os.ReadFile(filepath.Join(codexHome, "AGENTS.md"))
	if err != nil || string(got) != CodexInstructions() {
		t.Fatalf("AGENTS.md = %q (err %v), want CodexInstructions()", got, err)
	}
}

// 契约：AGENTS.md 的内容随版本走——家目录里躺着旧文案时必须被覆盖，
// 否则升级后老用户永远拿的是旧约定。
func TestCodexAdapter_Isolation_RewritesStaleInstructions(t *testing.T) {
	dataDir := t.TempDir()
	skillpack := filepath.Join(dataDir, "skillpack")
	if err := os.MkdirAll(filepath.Join(skillpack, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	codexHome := filepath.Join(dataDir, "codex-home")
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(codexHome, "AGENTS.md")
	if err := os.WriteFile(stale, []byte("# 上个版本的约定\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	codexAdapter{}.Isolation(IsolationInput{SkillpackDir: skillpack, Home: t.TempDir()})

	got, err := os.ReadFile(stale)
	if err != nil || string(got) != CodexInstructions() {
		t.Fatalf("AGENTS.md = %q (err %v), want overwritten with CodexInstructions()", got, err)
	}
}

// 契约：codex 缺 cwd 时 additionalDirectories 为空（不塞空串），隔离仍靠 CODEX_HOME。
func TestCodexAdapter_Isolation_OmitsEmptyCwd(t *testing.T) {
	dataDir := t.TempDir()
	skillpack := filepath.Join(dataDir, "skillpack")
	if err := os.MkdirAll(filepath.Join(skillpack, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	inj := codexAdapter{}.Isolation(IsolationInput{
		SkillpackDir: skillpack,
		Home:         t.TempDir(),
	})
	if len(inj.AdditionalDirs) != 0 {
		t.Fatalf("additionalDirs = %v, want empty", inj.AdditionalDirs)
	}
	if inj.Env["CODEX_HOME"] == "" {
		t.Fatal("CODEX_HOME must be set even without cwd")
	}
}

// 契约：SkillpackDir 为空 = 不做技能隔离，codex 与 generic 返回零值注入
// （会话沿用机器级）。claude 例外：它的提示词注入口与隔离同在 _meta，
// 提示词照给，只是不挂技能包——见上面的 InstructionsWithoutSkillpack。
func TestAdapters_Isolation_EmptySkillpackIsNoop(t *testing.T) {
	adapters := map[string]Adapter{
		"codex":   codexAdapter{},
		"generic": genericAdapter{},
	}
	for name, a := range adapters {
		t.Run(name, func(t *testing.T) {
			inj := a.Isolation(IsolationInput{Cwd: "/work", Home: "/home/u"})
			if inj.Env != nil || inj.Meta != nil || inj.AdditionalDirs != nil {
				t.Fatalf("%s: empty skillpack must yield zero injection, got %+v", name, inj)
			}
		})
	}
}

// 契约：generic runtime 没有可靠注入口，即便给了技能包也不注入（不猜语义）。
func TestGenericAdapter_Isolation_AlwaysEmpty(t *testing.T) {
	inj := genericAdapter{}.Isolation(IsolationInput{
		SkillpackDir: "/data/skillpack",
		Cwd:          "/work",
		Home:         "/home/u",
	})
	if inj.Env != nil || inj.Meta != nil || inj.AdditionalDirs != nil {
		t.Fatalf("generic must never inject, got %+v", inj)
	}
}

// 契约：mergeMeta 递归合并 map、其余类型上层覆盖、不改动入参——
// 编排会话把 systemPrompt/disallowedTools 合并进技能隔离 Meta 全靠它。
func TestMergeMeta(t *testing.T) {
	cases := []struct {
		name        string
		base, extra map[string]any
		want        map[string]any
	}{
		{
			name: "都为空返回 nil",
			want: nil,
		},
		{
			name:  "base 为空取 extra",
			extra: map[string]any{"systemPrompt": map[string]any{"append": "p"}},
			want:  map[string]any{"systemPrompt": map[string]any{"append": "p"}},
		},
		{
			name: "不同键并存",
			base: map[string]any{"claudeCode": map[string]any{"options": map[string]any{"strictMcpConfig": true}}},
			extra: map[string]any{
				"systemPrompt": map[string]any{"append": "p"},
			},
			want: map[string]any{
				"claudeCode":   map[string]any{"options": map[string]any{"strictMcpConfig": true}},
				"systemPrompt": map[string]any{"append": "p"},
			},
		},
		{
			name: "嵌套 map 递归合并且同键上层覆盖",
			base: map[string]any{"claudeCode": map[string]any{"options": map[string]any{
				"strictMcpConfig": true,
				"settingSources":  []string{"project"},
			}}},
			extra: map[string]any{"claudeCode": map[string]any{"options": map[string]any{
				"disallowedTools": []string{"Task"},
				"strictMcpConfig": false,
			}}},
			want: map[string]any{"claudeCode": map[string]any{"options": map[string]any{
				"strictMcpConfig": false,
				"settingSources":  []string{"project"},
				"disallowedTools": []string{"Task"},
			}}},
		},
		{
			name: "map 与非 map 冲突时上层覆盖",
			base: map[string]any{"systemPrompt": map[string]any{"append": "p"}},
			extra: map[string]any{
				"systemPrompt": "replace",
			},
			want: map[string]any{"systemPrompt": "replace"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			baseCopy := fmtMeta(tc.base)
			got := mergeMeta(tc.base, tc.extra)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("mergeMeta = %#v, want %#v", got, tc.want)
			}
			if !reflect.DeepEqual(fmtMeta(tc.base), baseCopy) {
				t.Fatalf("base 被改动：%#v", tc.base)
			}
		})
	}
}

// fmtMeta 深拷贝断言快照用（只覆盖测试里用到的形状）。
func fmtMeta(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if mv, ok := v.(map[string]any); ok {
			out[k] = fmtMeta(mv)
			continue
		}
		out[k] = v
	}
	return out
}
