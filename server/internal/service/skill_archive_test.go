package service

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// htmlReport 是带附属文件的真实技能形态：正文的一半在 references/ 里，
// 搬家时少了它技能就残废了。
var htmlReport = SkillCreateInput{
	Name:        "html-report",
	Description: `把有结构的成果做成单文件 HTML 报告：分析项目、出周报、做方案对比时用——"随便说说"只影响报告长度，不改变该不该出报告。`,
	Body:        "# html-report\n\n内容有结构时，线性文字会把结构压扁，HTML 不会。",
}

const tokensDoc = ":root {\n  --radius: 10px;\n}\n"

// exportAll 把整个技能库导出成内存里的一个包，供断言与往返导入用。
func exportAll(t *testing.T, s *SkillService) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := s.ExportZip("", &buf); err != nil {
		t.Fatalf("export: %v", err)
	}
	return buf.Bytes()
}

func zipEntries(t *testing.T, raw []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		out[f.Name] = string(data)
	}
	return out
}

// 契约：导出包的结构就是技能库的结构（`<name>/SKILL.md` + 附属文件），
// 解开即可放回 skills/ 下。系统垃圾（点文件）不进包。
func TestSkillService_ExportZip_PacksLibraryWithAttachments(t *testing.T) {
	s, dataDir := newSkillService(t)
	if _, err := s.Create(commitStyle); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.Create(htmlReport); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.PutFile("html-report", "references/tokens.md", tokensDoc); err != nil {
		t.Fatalf("put attachment: %v", err)
	}
	junk := filepath.Join(dataDir, "skills", "html-report", ".DS_Store")
	if err := os.WriteFile(junk, []byte("\x00\x01"), 0o644); err != nil {
		t.Fatalf("write junk: %v", err)
	}

	entries := zipEntries(t, exportAll(t, s))

	for _, want := range []string{
		"commit-style/SKILL.md",
		"html-report/SKILL.md",
		"html-report/references/tokens.md",
	} {
		if _, ok := entries[want]; !ok {
			t.Fatalf("archive is missing %s; has %v", want, keysOf(entries))
		}
	}
	if entries["html-report/references/tokens.md"] != tokensDoc {
		t.Fatalf("attachment content:\n%s", entries["html-report/references/tokens.md"])
	}
	for name := range entries {
		if filepath.Base(name) == ".DS_Store" {
			t.Fatalf("system junk should not be packed: %s", name)
		}
	}
}

// 契约：单个技能导出只含那一个——用来把一个技能发给别人，不是交出整个库。
func TestSkillService_ExportZip_SingleSkillOnly(t *testing.T) {
	s, _ := newSkillService(t)
	if _, err := s.Create(commitStyle); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.Create(htmlReport); err != nil {
		t.Fatalf("create: %v", err)
	}

	var buf bytes.Buffer
	if err := s.ExportZip("html-report", &buf); err != nil {
		t.Fatalf("export one: %v", err)
	}

	entries := zipEntries(t, buf.Bytes())
	if _, ok := entries["html-report/SKILL.md"]; !ok {
		t.Fatalf("archive should contain the skill; has %v", keysOf(entries))
	}
	for name := range entries {
		if !strings.HasPrefix(name, "html-report/") {
			t.Fatalf("archive leaked another skill: %s", name)
		}
	}
}

// 契约：导出不存在的技能是 404，不是一个空包——空包在新机器上导入会静默
// 什么也不发生，人以为搬过去了。
func TestSkillService_ExportZip_UnknownSkillIsNotFound(t *testing.T) {
	s, _ := newSkillService(t)

	err := s.ExportZip("commit-style", &bytes.Buffer{})

	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("export unknown skill err = %v, want ErrNotFound", err)
	}
}

// 契约：换设备搬家的主路径——在另一台机器上导入，描述、正文与附属文件
// 原样落回，且**一律停用**（技能会注入每条会话，得由人看过再开）。
func TestSkillService_ImportZip_RestoresSkillsDisabled(t *testing.T) {
	src, _ := newSkillService(t)
	if _, err := src.Create(htmlReport); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := src.PutFile("html-report", "references/tokens.md", tokensDoc); err != nil {
		t.Fatalf("put attachment: %v", err)
	}
	raw := exportAll(t, src)

	dst, dstDir := newSkillService(t)
	res, err := dst.ImportZip(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	if len(res.Imported) != 1 || res.Imported[0] != "html-report" {
		t.Fatalf("imported = %+v, want [html-report]", res)
	}
	got, err := dst.Get("html-report")
	if err != nil {
		t.Fatalf("get imported: %v", err)
	}
	if got.Description != htmlReport.Description {
		t.Fatalf("description:\ngot  %q\nwant %q", got.Description, htmlReport.Description)
	}
	if got.Body != htmlReport.Body {
		t.Fatalf("body:\ngot  %q\nwant %q", got.Body, htmlReport.Body)
	}
	if got.Enabled {
		t.Fatal("imported skill must start disabled")
	}
	if _, err := os.Lstat(filepath.Join(dstDir, "skillpack", "skills", "html-report")); err == nil {
		t.Fatal("imported skill should have no pack link yet")
	}
	file, err := dst.GetFile("html-report", "references/tokens.md")
	if err != nil {
		t.Fatalf("get imported attachment: %v", err)
	}
	if file.Content != tokensDoc {
		t.Fatalf("attachment content:\n%s", file.Content)
	}
}

// 契约：已存在的同名技能跳过而不是覆盖——那是这台机器上用户自己的东西，
// 搬家不该无声地把它换掉。结果里说明跳过原因。
func TestSkillService_ImportZip_SkipsExistingSkill(t *testing.T) {
	src, _ := newSkillService(t)
	if _, err := src.Create(htmlReport); err != nil {
		t.Fatalf("create: %v", err)
	}
	raw := exportAll(t, src)

	dst, _ := newSkillService(t)
	mine := SkillCreateInput{
		Name:        "html-report",
		Description: "我自己写的报告口径：只出表格，不画图。",
		Body:        "# html-report\n\n我的版本。",
	}
	if _, err := dst.Create(mine); err != nil {
		t.Fatalf("create mine: %v", err)
	}

	res, err := dst.ImportZip(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	if len(res.Imported) != 0 {
		t.Fatalf("imported = %v, want nothing", res.Imported)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Reason != "exists" {
		t.Fatalf("skipped = %+v, want one exists entry", res.Skipped)
	}
	kept, err := dst.Get("html-report")
	if err != nil {
		t.Fatalf("get mine: %v", err)
	}
	if kept.Body != mine.Body {
		t.Fatalf("existing skill overwritten:\ngot  %q\nwant %q", kept.Body, mine.Body)
	}
}

// 契约：带路径穿越的包整包拒绝，一个字节都不落盘。一条
// `../../.ssh/authorized_keys` 就能写到技能库外面去，这种包不可信。
func TestSkillService_ImportZip_RejectsPathEscape(t *testing.T) {
	s, dataDir := newSkillService(t)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"html-report/SKILL.md", "../../evil.md"} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create entry: %v", err)
		}
		if _, err := w.Write([]byte("---\nname: x\ndescription: y\n---\n")); err != nil {
			t.Fatalf("write entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}

	_, err := s.ImportZip(bytes.NewReader(buf.Bytes()), int64(buf.Len()))

	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("import escaping archive err = %v, want ErrInvalid", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "..", "evil.md")); err == nil {
		t.Fatal("escaping entry was written outside the library")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "skills", "html-report")); err == nil {
		t.Fatal("nothing should be written from a rejected archive")
	}
}

// 契约：外来包里的目录名归一到 kebab-case，frontmatter 的 name 跟着对齐
// ——两端按目录名发现技能，不一致会让「列表里叫 A、AI 眼里叫 B」。
func TestSkillService_ImportZip_NormalizesNameAndFrontmatter(t *testing.T) {
	s, _ := newSkillService(t)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("Bet_Report/SKILL.md")
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	doc := "---\nname: Bet_Report\ndescription: 出投注运营报告：排行、走势、RTP。\n---\n\n# Bet_Report\n\n口径固定，不要临场写 SQL。\n"
	if _, err := w.Write([]byte(doc)); err != nil {
		t.Fatalf("write entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}

	res, err := s.ImportZip(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	if len(res.Imported) != 1 || res.Imported[0] != "bet-report" {
		t.Fatalf("imported = %+v, want [bet-report]", res)
	}
	got, err := s.Get("bet-report")
	if err != nil {
		t.Fatalf("get imported: %v", err)
	}
	if got.Description != "出投注运营报告：排行、走势、RTP。" {
		t.Fatalf("description: %q", got.Description)
	}
	raw, err := os.ReadFile(s.skillFile("bet-report"))
	if err != nil {
		t.Fatalf("read imported doc: %v", err)
	}
	if !strings.Contains(string(raw), "name: bet-report\n") {
		t.Fatalf("frontmatter name not aligned:\n%s", string(raw))
	}
}

// 契约：没有 SKILL.md 的目录不是技能，跳过并说明原因——包里夹带的别的
// 东西不该变成一个空壳技能躺在库里。
func TestSkillService_ImportZip_SkipsDirectoriesWithoutSkillDoc(t *testing.T) {
	s, _ := newSkillService(t)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("notes/todo.md")
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if _, err := w.Write([]byte("# 随手记\n")); err != nil {
		t.Fatalf("write entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}

	res, err := s.ImportZip(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	if len(res.Imported) != 0 {
		t.Fatalf("imported = %v, want nothing", res.Imported)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Reason != "no_doc" {
		t.Fatalf("skipped = %+v, want one no_doc entry", res.Skipped)
	}
	list, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("library should stay empty, got %+v", list)
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
