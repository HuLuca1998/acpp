package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// commitStyle 是真实形态的技能输入（非占位数据）。description 里带冒号与
// 引号，正好覆盖 YAML 转义契约。
var commitStyle = SkillCreateInput{
	Name:        "commit-style",
	Description: `提交规范——写 git commit 时使用："Conventional Commits"、中文描述、分批提交。`,
	Body:        "# commit-style\n\n提交用 Conventional Commits，描述写中文，一个提交一个主题。",
}

func newSkillService(t *testing.T) (*SkillService, string) {
	t.Helper()
	dataDir := t.TempDir()
	return NewSkillService(dataDir), dataDir
}

// 契约：Create 落盘 SKILL.md（frontmatter 由服务端组装）、默认启用，
// description/body 原样往返——含 YAML 特殊字符。
func TestSkillService_Create_RoundTripsStructuredFields(t *testing.T) {
	s, _ := newSkillService(t)

	detail, err := s.Create(commitStyle)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !detail.Enabled {
		t.Fatal("new skill should be enabled by default")
	}

	got, err := s.Get("commit-style")
	if err != nil {
		t.Fatalf("get after create: %v", err)
	}
	if got.Description != commitStyle.Description {
		t.Fatalf("description round-trip:\ngot  %q\nwant %q", got.Description, commitStyle.Description)
	}
	if got.Body != commitStyle.Body {
		t.Fatalf("body round-trip:\ngot  %q\nwant %q", got.Body, commitStyle.Body)
	}
}

// 契约：skillpack 是分发面——启用的技能在 skillpack/skills 下有相对链接，
// 骨架（plugin.json、.agents/skills）随首次操作自动搭好。两端 agent 的
// 发现机制都依赖这个布局，它是对外契约而非实现细节。
func TestSkillService_Create_PopulatesSkillpackLayout(t *testing.T) {
	s, dataDir := newSkillService(t)

	if _, err := s.Create(commitStyle); err != nil {
		t.Fatalf("create: %v", err)
	}

	link := filepath.Join(dataDir, "skillpack", "skills", "commit-style")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("skillpack link: %v", err)
	}
	if filepath.IsAbs(target) {
		t.Fatalf("link target %q must be relative to survive data-dir migration", target)
	}
	// 链接必须真的解析到源文件，否则注入后 agent 读不到内容。
	if _, err := os.Stat(filepath.Join(link, "SKILL.md")); err != nil {
		t.Fatalf("resolve SKILL.md through link: %v", err)
	}

	manifest, err := os.ReadFile(filepath.Join(dataDir, "skillpack", ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatalf("plugin.json: %v", err)
	}
	if !strings.Contains(string(manifest), `"acpp"`) {
		t.Fatalf("plugin.json = %s, want name acpp", manifest)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "skillpack", ".agents", "skills")); err != nil {
		t.Fatalf(".agents/skills entry: %v", err)
	}
}

// 契约：非法入参一律 ErrInvalid——name 不是 kebab-case、缺 description、重复创建。
func TestSkillService_Create_RejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		in   SkillCreateInput
	}{
		{"empty name", SkillCreateInput{Description: "描述"}},
		{"non kebab name", SkillCreateInput{Name: "Bad_Name", Description: "描述"}},
		{"path-ish name", SkillCreateInput{Name: "../evil", Description: "描述"}},
		{"missing description", SkillCreateInput{Name: "only-name"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newSkillService(t)
			if _, err := s.Create(tt.in); !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
		})
	}

	t.Run("duplicate", func(t *testing.T) {
		s, _ := newSkillService(t)
		if _, err := s.Create(commitStyle); err != nil {
			t.Fatalf("first create: %v", err)
		}
		if _, err := s.Create(commitStyle); !errors.Is(err, ErrInvalid) {
			t.Fatalf("duplicate create err = %v, want ErrInvalid", err)
		}
	})
}

// 契约：List 遍历文件夹即事实源——手工放进源目录的技能（未经 Create）
// 一样出现在列表里，且因为没有分发链接而处于停用态。
func TestSkillService_List_DiscoversHandPlacedSkills(t *testing.T) {
	s, dataDir := newSkillService(t)

	dir := filepath.Join(dataDir, "skills", "deploy-runbook")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: deploy-runbook\ndescription: 部署手册——发布线上版本时使用。\n---\n\n# 部署\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}

	skills, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("len = %d, want 1 (%+v)", len(skills), skills)
	}
	got := skills[0]
	if got.Name != "deploy-runbook" || got.Enabled || got.Description != "部署手册——发布线上版本时使用。" {
		t.Fatalf("got %+v, want deploy-runbook disabled with parsed description", got)
	}
}

// 契约：frontmatter 坏了的技能也要出现在列表里让用户修，字段容错为空。
func TestSkillService_List_KeepsBrokenSkillVisible(t *testing.T) {
	s, dataDir := newSkillService(t)

	dir := filepath.Join(dataDir, "skills", "broken-skill")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("no frontmatter at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	skills, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "broken-skill" || skills[0].Description != "" {
		t.Fatalf("got %+v, want broken-skill with empty description", skills)
	}
}

// 契约：启停只动分发链接，来回切换不碰源内容。
func TestSkillService_Update_TogglesEnabledWithoutTouchingSource(t *testing.T) {
	s, dataDir := newSkillService(t)
	if _, err := s.Create(commitStyle); err != nil {
		t.Fatalf("create: %v", err)
	}

	off := false
	detail, err := s.Update("commit-style", SkillUpdateInput{Enabled: &off})
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if detail.Enabled {
		t.Fatal("skill should be disabled")
	}
	if _, err := os.Lstat(filepath.Join(dataDir, "skillpack", "skills", "commit-style")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("link should be gone, lstat err = %v", err)
	}
	if detail.Body != commitStyle.Body {
		t.Fatal("disable must not touch source content")
	}

	on := true
	detail, err = s.Update("commit-style", SkillUpdateInput{Enabled: &on})
	if err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if !detail.Enabled {
		t.Fatal("skill should be enabled again")
	}
}

// 契约：编辑手放的技能会保留 name/description 之外的 frontmatter 行
// （第三方技能常有 license 等字段），并把 name 纠正到目录名。
func TestSkillService_Update_PreservesUnknownFrontmatter(t *testing.T) {
	s, dataDir := newSkillService(t)

	dir := filepath.Join(dataDir, "skills", "third-party")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: wrong-name\ndescription: 第三方技能。\nlicense: Complete terms in LICENSE.txt\n---\n\n# 正文\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}

	desc := "改过的描述——触发场景补全。"
	if _, err := s.Update("third-party", SkillUpdateInput{Description: &desc}); err != nil {
		t.Fatalf("update: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	if !strings.Contains(content, "license: Complete terms in LICENSE.txt") {
		t.Fatalf("license line lost:\n%s", content)
	}
	if !strings.Contains(content, "name: third-party") {
		t.Fatalf("name should be corrected to dir name:\n%s", content)
	}
}

// 契约：Delete 连源目录带分发链接一起清掉，之后按不存在处理。
func TestSkillService_Delete_RemovesSourceAndLink(t *testing.T) {
	s, dataDir := newSkillService(t)
	if _, err := s.Create(commitStyle); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := s.Delete("commit-style"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dataDir, "skills", "commit-style")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("source dir should be removed")
	}
	if _, err := os.Lstat(filepath.Join(dataDir, "skillpack", "skills", "commit-style")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("skillpack link should be removed")
	}
	if _, err := s.Get("commit-style"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete err = %v, want ErrNotFound", err)
	}
}

// 契约：name 是路径段，越界形态一律按 404 拒绝，不落到文件系统。
func TestSkillService_PathTraversalNamesAreNotFound(t *testing.T) {
	s, _ := newSkillService(t)
	for _, name := range []string{"../evil", "a/b", ".hidden", "UPPER", ""} {
		if _, err := s.Get(name); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get(%q) err = %v, want ErrNotFound", name, err)
		}
		if err := s.Delete(name); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Delete(%q) err = %v, want ErrNotFound", name, err)
		}
	}
}

// 契约：附属文件读写往返，子目录自动补齐，SKILL.md 不出现在文件列表里。
func TestSkillService_Files_RoundTripAndExcludeSkillMD(t *testing.T) {
	s, _ := newSkillService(t)
	if _, err := s.Create(commitStyle); err != nil {
		t.Fatalf("create: %v", err)
	}

	script := "#!/usr/bin/env python3\nprint(\"validate frontmatter\")\n"
	if _, err := s.PutFile("commit-style", "scripts/validate.py", script); err != nil {
		t.Fatalf("put file: %v", err)
	}

	files, err := s.ListFiles("commit-style")
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	if len(files) != 1 || files[0].Path != "scripts/validate.py" {
		t.Fatalf("files = %+v, want only scripts/validate.py", files)
	}

	got, err := s.GetFile("commit-style", "scripts/validate.py")
	if err != nil {
		t.Fatalf("get file: %v", err)
	}
	if got.Content != script {
		t.Fatalf("content round-trip:\ngot  %q\nwant %q", got.Content, script)
	}
}

// 契约：删除文件后变空的中间目录一并清掉，不留空壳。
func TestSkillService_DeleteFile_PrunesEmptyDirs(t *testing.T) {
	s, dataDir := newSkillService(t)
	if _, err := s.Create(commitStyle); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.PutFile("commit-style", "references/patterns.md", "# 模式\n"); err != nil {
		t.Fatalf("put file: %v", err)
	}

	if err := s.DeleteFile("commit-style", "references/patterns.md"); err != nil {
		t.Fatalf("delete file: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dataDir, "skills", "commit-style", "references")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("empty references/ dir should be pruned")
	}
	if _, err := os.Lstat(filepath.Join(dataDir, "skills", "commit-style", "SKILL.md")); err != nil {
		t.Fatal("SKILL.md must survive dir pruning")
	}
}

// 契约：文件路径钉死在技能目录内——穿越 404；SKILL.md 走结构化编辑不给旁路。
func TestSkillService_Files_PathGuards(t *testing.T) {
	s, _ := newSkillService(t)
	if _, err := s.Create(commitStyle); err != nil {
		t.Fatalf("create: %v", err)
	}

	for _, rel := range []string{"../outside.md", "/etc/passwd", "a/../../b", ".git/config"} {
		if _, err := s.PutFile("commit-style", rel, "x"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("PutFile(%q) err = %v, want ErrNotFound", rel, err)
		}
	}
	if _, err := s.PutFile("commit-style", "SKILL.md", "x"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("PutFile(SKILL.md) err = %v, want ErrInvalid", err)
	}
}

// 契约：二进制文件在列表里标出，且拒绝按文本读取。
func TestSkillService_GetFile_RejectsBinary(t *testing.T) {
	s, dataDir := newSkillService(t)
	if _, err := s.Create(commitStyle); err != nil {
		t.Fatalf("create: %v", err)
	}
	bin := []byte{0x89, 'P', 'N', 'G', 0x00, 0x1a}
	if err := os.WriteFile(filepath.Join(dataDir, "skills", "commit-style", "assets-logo.png"), bin, 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := s.ListFiles("commit-style")
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	if len(files) != 1 || !files[0].Binary {
		t.Fatalf("files = %+v, want one binary entry", files)
	}
	if _, err := s.GetFile("commit-style", "assets-logo.png"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("get binary err = %v, want ErrInvalid", err)
	}
}

// 契约：脚本头部注释（desc/usage/arg/opt/env）被解析成元信息，扩展名在
// 解释器映射内的标记为可运行。
func TestSkillService_ListScripts_ParsesHeaderMeta(t *testing.T) {
	s, _ := newSkillService(t)
	if _, err := s.Create(commitStyle); err != nil {
		t.Fatalf("create: %v", err)
	}

	script := `#!/usr/bin/env python3
# desc: 校验 SKILL.md frontmatter 是否符合规范
# usage: validate.py <skill-dir> [--strict]
# arg: skill-dir 技能目录路径
# opt: strict 严格模式
# env: ACPP_DEBUG 打开调试输出

print("ok")
`
	if _, err := s.PutFile("commit-style", "scripts/validate.py", script); err != nil {
		t.Fatalf("put script: %v", err)
	}
	// 头部之外的注释不算元信息：正文里出现 desc: 不应覆盖头部。
	if _, err := s.PutFile("commit-style", "scripts/notes.txt", "随手记，不是脚本"); err != nil {
		t.Fatalf("put notes: %v", err)
	}

	scripts, err := s.ListScripts("commit-style")
	if err != nil {
		t.Fatalf("list scripts: %v", err)
	}
	if len(scripts) != 2 {
		t.Fatalf("scripts = %+v, want validate.py + notes.txt", scripts)
	}

	var validate *SkillScript
	for i := range scripts {
		if scripts[i].Path == "scripts/validate.py" {
			validate = &scripts[i]
		}
	}
	if validate == nil {
		t.Fatalf("validate.py missing in %+v", scripts)
	}
	if !validate.Runnable {
		t.Fatal("python script should be runnable")
	}
	if validate.Description != "校验 SKILL.md frontmatter 是否符合规范" {
		t.Fatalf("desc = %q", validate.Description)
	}
	if len(validate.Args) != 1 || validate.Args[0].Name != "skill-dir" || validate.Args[0].Label != "技能目录路径" {
		t.Fatalf("args = %+v", validate.Args)
	}
	if len(validate.Opts) != 1 || validate.Opts[0].Name != "strict" {
		t.Fatalf("opts = %+v", validate.Opts)
	}
	if len(validate.Envs) != 1 || validate.Envs[0].Name != "ACPP_DEBUG" {
		t.Fatalf("envs = %+v", validate.Envs)
	}
}

// 契约：RunScript 以技能目录为 cwd 真实执行，args/opts/env 按约定传入，
// 退出码与输出如实返回。
func TestSkillService_RunScript_PassesArgsOptsEnv(t *testing.T) {
	s, _ := newSkillService(t)
	if _, err := s.Create(commitStyle); err != nil {
		t.Fatalf("create: %v", err)
	}

	script := `#!/bin/bash
# desc: 回显参数与环境
# arg: target 目标名
# opt: strict 严格模式
# env: ACPP_MODE 运行模式
echo "cwd=$(basename "$PWD") target=$1 flag=$2 mode=$ACPP_MODE"
ls SKILL.md >/dev/null || exit 3
`
	if _, err := s.PutFile("commit-style", "scripts/echo-args.sh", script); err != nil {
		t.Fatalf("put script: %v", err)
	}

	result, err := s.RunScript(t.Context(), "commit-style", SkillScriptRunInput{
		Path: "scripts/echo-args.sh",
		Args: []string{"skill-dist"},
		Opts: []string{"strict"},
		Env:  map[string]string{"ACPP_MODE": "check"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit = %d, stderr = %s", result.ExitCode, result.Stderr)
	}
	want := "cwd=commit-style target=skill-dist flag=--strict mode=check\n"
	if result.Stdout != want {
		t.Fatalf("stdout = %q, want %q", result.Stdout, want)
	}
}

// 契约：非零退出码不是 error，是结果的一部分；scripts/ 之外一律拒绝运行。
func TestSkillService_RunScript_ReportsExitCodeAndGuardsPath(t *testing.T) {
	s, _ := newSkillService(t)
	if _, err := s.Create(commitStyle); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.PutFile("commit-style", "scripts/fail.sh", "#!/bin/bash\nexit 42\n"); err != nil {
		t.Fatalf("put script: %v", err)
	}
	if _, err := s.PutFile("commit-style", "references/doc.sh", "echo not-a-script"); err != nil {
		t.Fatalf("put ref: %v", err)
	}

	result, err := s.RunScript(t.Context(), "commit-style", SkillScriptRunInput{Path: "scripts/fail.sh"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.ExitCode != 42 {
		t.Fatalf("exit = %d, want 42", result.ExitCode)
	}

	if _, err := s.RunScript(t.Context(), "commit-style", SkillScriptRunInput{Path: "references/doc.sh"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("run outside scripts/ err = %v, want ErrInvalid", err)
	}
}
