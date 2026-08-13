package acp

import (
	"encoding/json"
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
}

// 契约：codex 的隔离走进程环境变量 CODEX_CONFIG（枚举机器级 skill 按
// frontmatter name 逐个禁用）+ additionalDirectories（技能包 + 工作目录）。
// 不产生 _meta。
func TestCodexAdapter_Isolation_DisablesMachineSkillsByFrontmatterName(t *testing.T) {
	home := t.TempDir()
	// 机器级 skill：目录名与 frontmatter name 故意不同——禁用选择器必须
	// 用 frontmatter name，不是目录名。
	dir := filepath.Join(home, ".codex", "skills", "some-dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: brainstorming\ndescription: 机器级技能。\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}

	inj := codexAdapter{}.Isolation(IsolationInput{
		SkillpackDir: "/data/skillpack",
		Cwd:          "/work/proj",
		Home:         home,
	})

	if inj.Meta != nil {
		t.Fatalf("codex must not use _meta, got %v", inj.Meta)
	}
	if !reflect.DeepEqual(inj.AdditionalDirs, []string{"/data/skillpack", "/work/proj"}) {
		t.Fatalf("additionalDirs = %v, want [skillpack, cwd]", inj.AdditionalDirs)
	}

	var cfg struct {
		Skills struct {
			Config []struct {
				Name    string `json:"name"`
				Enabled bool   `json:"enabled"`
			} `json:"config"`
		} `json:"skills"`
		MCPServers map[string]any `json:"mcp_servers"`
	}
	if err := json.Unmarshal([]byte(inj.Env["CODEX_CONFIG"]), &cfg); err != nil {
		t.Fatalf("CODEX_CONFIG not valid json: %v", err)
	}
	if len(cfg.Skills.Config) != 1 || cfg.Skills.Config[0].Name != "brainstorming" || cfg.Skills.Config[0].Enabled {
		t.Fatalf("skills.config = %+v, want brainstorming disabled by frontmatter name", cfg.Skills.Config)
	}
	if cfg.MCPServers == nil {
		t.Fatal("mcp_servers must be present (empty object), skill isolation must not drop the key")
	}
}

// 契约：codex 缺 cwd 时 additionalDirectories 只有技能包，不塞空串。
func TestCodexAdapter_Isolation_OmitsEmptyCwd(t *testing.T) {
	inj := codexAdapter{}.Isolation(IsolationInput{
		SkillpackDir: "/data/skillpack",
		Home:         t.TempDir(),
	})
	if !reflect.DeepEqual(inj.AdditionalDirs, []string{"/data/skillpack"}) {
		t.Fatalf("additionalDirs = %v, want [skillpack] only", inj.AdditionalDirs)
	}
}

// 契约：SkillpackDir 为空 = 不隔离，三端都返回零值注入（会话沿用机器级）。
func TestAdapters_Isolation_EmptySkillpackIsNoop(t *testing.T) {
	adapters := map[string]Adapter{
		"claude":  claudeAdapter{},
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
