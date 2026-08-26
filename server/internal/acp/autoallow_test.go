package acp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// 造出技能库与分发包的**真实形状**：技能实体住在 <data>/skills/<name>，
// 分发包里的 skillpack/skills/<name> 只是指向它的软链（文件系统即启用状态）。
// 于是 agent 拿到的 skillpack 路径解析软链后会落到技能库那一侧——这正是只登记
// 一个根就会全判不中的地方。旁边再放两个前缀相同的诱饵目录，钉死「前缀匹配」
// 这个经典错法。返回的 refFile 是 agent 实际会拿到的那条（经分发包访问）。
func skillpackFixture(t *testing.T) (pack, refFile, evilFile string) {
	t.Helper()
	base := t.TempDir()

	refs := filepath.Join(base, "skills", "demo", "references")
	if err := os.MkdirAll(refs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(refs, "boilerplate.html"), []byte("<p>x</p>"), 0o644); err != nil {
		t.Fatal(err)
	}

	pack = filepath.Join(base, "skillpack")
	packSkills := filepath.Join(pack, "skills")
	if err := os.MkdirAll(packSkills, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../skills/demo", filepath.Join(packSkills, "demo")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pack, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(packSkills, filepath.Join(pack, ".agents", "skills")); err != nil {
		t.Fatal(err)
	}
	refFile = filepath.Join(packSkills, "demo", "references", "boilerplate.html")

	for _, evil := range []string{pack + "-evil", filepath.Join(base, "skills-evil")} {
		if err := os.MkdirAll(evil, 0o755); err != nil {
			t.Fatal(err)
		}
		evilFile = filepath.Join(evil, "secret.txt")
		if err := os.WriteFile(evilFile, []byte("s"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return pack, refFile, evilFile
}

func readRequest(kind string, paths ...string) RequestPermissionParams {
	p := RequestPermissionParams{
		ToolCall: PermissionToolCall{ToolCallID: "toolu_1", Kind: kind},
		Options: []PermissionOption{
			{OptionID: "reject", Kind: "reject_once", Name: "Deny"},
			{OptionID: "allow", Kind: "allow_once", Name: "Allow Once"},
			{OptionID: "allow_always", Kind: "allow_always", Name: "Always Allow"},
		},
	}
	if len(paths) > 0 {
		locs := make([]map[string]any, 0, len(paths))
		for _, path := range paths {
			locs = append(locs, map[string]any{"path": path})
		}
		p.ToolCall.Locations, _ = json.Marshal(locs)
	}
	return p
}

// 契约：只有「读技能包内文件」才自动放行，且放行的是一次性选项。
func TestAutoAllowRead(t *testing.T) {
	pack, refFile, evilFile := skillpackFixture(t)
	roots := autoAllowRoots(pack)
	if len(roots) != 2 {
		t.Fatalf("autoAllowRoots(%q) = %v, want 2 roots (分发包 + 技能库)", pack, roots)
	}
	viaSymlink := filepath.Join(pack, ".agents", "skills", "demo", "references", "boilerplate.html")
	missing := filepath.Join(pack, "skills", "demo", "references", "not-yet.md")
	libFile := filepath.Join(filepath.Dir(pack), "skills", "demo", "references", "boilerplate.html")

	cases := []struct {
		name  string
		req   RequestPermissionParams
		roots []string
		want  bool
	}{
		{"经分发包软链访问技能文件", readRequest("read", refFile), roots, true},
		{"直接访问技能库里的同一文件", readRequest("read", libFile), roots, true},
		{"经 .agents 软链访问同一文件", readRequest("read", viaSymlink), roots, true},
		{"包内还不存在的文件（父目录在包内）", readRequest("read", missing), roots, true},
		{"前缀相同的诱饵目录不算包内", readRequest("read", evilFile), roots, false},
		{"包外文件", readRequest("read", "/etc/passwd"), roots, false},
		{"写请求一律交用户", readRequest("edit", refFile), roots, false},
		{"多路径只要有一条在外就整条交回", readRequest("read", refFile, evilFile), roots, false},
		{"相对路径无从判断", readRequest("read", "skills/demo/x.md"), roots, false},
		{"拿不到路径（codex 形状）", readRequest("read"), roots, false},
		{"没配技能包时永不放行", readRequest("read", refFile), nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			optionID, ok := autoAllowRead(c.req, c.roots)
			if ok != c.want {
				t.Fatalf("autoAllowRead() ok = %v, want %v", ok, c.want)
			}
			if ok && optionID != "allow" {
				t.Fatalf("optionID = %q, want the allow_once option (%q)", optionID, "allow")
			}
		})
	}
}

// 契约：路径既可以来自 ACP 标准的 locations，也可以来自 claude Read 工具的
// rawInput.file_path——两个来源互为兜底，少一个照样判得出。
func TestAutoAllowRead_PathFromRawInput(t *testing.T) {
	pack, refFile, _ := skillpackFixture(t)
	roots := autoAllowRoots(pack)

	req := readRequest("read")
	req.ToolCall.RawInput, _ = json.Marshal(map[string]string{"file_path": refFile})
	if _, ok := autoAllowRead(req, roots); !ok {
		t.Fatal("rawInput.file_path 应能单独支撑判定")
	}
}

// 契约：没有 allow_once 这一档就不放行——宁可问用户，也不去猜哪个选项
// 等价于「只允许这一次」。
func TestAutoAllowRead_RequiresAllowOnceOption(t *testing.T) {
	pack, refFile, _ := skillpackFixture(t)
	roots := autoAllowRoots(pack)

	req := readRequest("read", refFile)
	req.Options = []PermissionOption{
		{OptionID: "reject", Kind: "reject_once", Name: "Deny"},
		{OptionID: "allow_always", Kind: "allow_always", Name: "Always Allow"},
	}
	if _, ok := autoAllowRead(req, roots); ok {
		t.Fatal("缺 allow_once 时不该放行（allow_always 会在 agent 侧落持久规则）")
	}
}

// 契约：技能包目录不存在时解析不出根，等于关掉自动放行。
func TestAutoAllowRoots_MissingDir(t *testing.T) {
	if got := autoAllowRoots(filepath.Join(t.TempDir(), "nope")); got != nil {
		t.Fatalf("autoAllowRoots(不存在) = %v, want nil", got)
	}
	if got := autoAllowRoots(""); got != nil {
		t.Fatalf("autoAllowRoots(\"\") = %v, want nil", got)
	}
}

// 契约：claude 把技能包登记为只读自动放行根（技能包在 cwd 之外，不放行就
// 每读一个参考文件弹一次卡）；codex 不登记——它的权限请求只有 shell 命令
// 字符串，拿不到可靠路径，而且技能本就在它自己的 CODEX_HOME 里。
func TestIsolation_AutoAllowReadDirs(t *testing.T) {
	pack, _, _ := skillpackFixture(t)

	claude := claudeAdapter{}.Isolation(IsolationInput{SkillpackDir: pack, Cwd: t.TempDir()})
	// 登记的必须是解析过软链的真实路径，否则判定时两侧比不上；分发包与技能库
	// 两个根都要，技能实体住在后者。
	for _, want := range []string{pack, filepath.Join(filepath.Dir(pack), "skills")} {
		real, err := filepath.EvalSymlinks(want)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(claude.AutoAllowReadDirs, real) {
			t.Fatalf("AutoAllowReadDirs = %v, want it to contain %q", claude.AutoAllowReadDirs, real)
		}
	}

	codex := codexAdapter{}.Isolation(IsolationInput{
		SkillpackDir: pack, Cwd: t.TempDir(), Home: t.TempDir(),
	})
	if codex.AutoAllowReadDirs != nil {
		t.Fatalf("codex AutoAllowReadDirs = %v, want none", codex.AutoAllowReadDirs)
	}
}
