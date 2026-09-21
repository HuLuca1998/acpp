package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// betReportDoc 是 codex 侧真实建出来的技能形态：frontmatter 的 description
// 裸写，正文短、口径固定。对应的目录直接躺在分发目录里——codex 自带的
// skill-creator 写死把新技能建到 $CODEX_HOME/skills，而技能隔离把那条路径
// 软链到了 skillpack/skills。
const betReportDoc = `---
name: bet-report
description: 出 pp-game 的投注运营报告——游戏/玩家/商户投注额排行，带走势、昨日环比、RTP、风控信号。触发词：出今天的投注报告、日报、GGR 怎么样。
---

# bet-report

已经有固定脚本和口径，不要临场自己写 SQL 和 HTML。
`

const betReportDesc = "出 pp-game 的投注运营报告——游戏/玩家/商户投注额排行，带走势、昨日环比、RTP、风控信号。触发词：出今天的投注报告、日报、GGR 怎么样。"

// writeStray 在分发目录里造一个 codex 侧建出来的技能：真实目录（不是软链），
// 带附属脚本。
func writeStray(t *testing.T, dataDir, dir string) {
	t.Helper()
	root := filepath.Join(dataDir, "skillpack", "skills", dir)
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
		t.Fatalf("mkdir stray: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(betReportDoc), 0o644); err != nil {
		t.Fatalf("write stray SKILL.md: %v", err)
	}
	script := "#!/usr/bin/env node\n// desc: 出投注日报\n"
	if err := os.WriteFile(filepath.Join(root, "scripts", "dashboard.mjs"), []byte(script), 0o644); err != nil {
		t.Fatalf("write stray script: %v", err)
	}
}

// 契约：codex 侧建在分发目录里的技能会被纳管——出现在列表里、保持启用、
// 附属文件跟着进源目录、分发目录只留软链。用户在页面上看得见也编辑得了，
// 而 agent 侧的注入面不中断。
func TestSkillService_AdoptStrays_MovesCodexBuiltSkillIntoLibrary(t *testing.T) {
	s, dataDir := newSkillService(t)
	writeStray(t, dataDir, "bet-report")

	list, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list = %+v, want the adopted skill only", list)
	}
	got := list[0]
	if got.Name != "bet-report" {
		t.Fatalf("adopted name = %q, want bet-report", got.Name)
	}
	if got.Description != betReportDesc {
		t.Fatalf("adopted description:\ngot  %q\nwant %q", got.Description, betReportDesc)
	}
	if !got.Enabled {
		t.Fatal("adopted skill should stay enabled: it was already injected from the pack dir")
	}

	// 源目录是事实源：正文与附属脚本都在。
	if _, err := os.Stat(filepath.Join(dataDir, "skills", "bet-report", "SKILL.md")); err != nil {
		t.Fatalf("SKILL.md in source dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "skills", "bet-report", "scripts", "dashboard.mjs")); err != nil {
		t.Fatalf("attachment moved with the skill: %v", err)
	}

	// 分发目录只留软链——启用状态的唯一表达形式。
	info, err := os.Lstat(filepath.Join(dataDir, "skillpack", "skills", "bet-report"))
	if err != nil {
		t.Fatalf("lstat pack entry: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("pack entry mode = %v, want a symlink", info.Mode())
	}

	// 页面能打开它：详情正文可读。
	detail, err := s.Get("bet-report")
	if err != nil {
		t.Fatalf("get adopted skill: %v", err)
	}
	if !strings.Contains(detail.Body, "不要临场自己写 SQL") {
		t.Fatalf("adopted body:\n%s", detail.Body)
	}
}

// 契约：codex 自己铺的系统技能（点开头）与正常的启用软链都不动，重复收养
// 不产生第二份。分发目录同时是 codex 的运行目录，搬走它的东西会弄坏它。
func TestSkillService_AdoptStrays_LeavesCodexSystemDirsAndLinksAlone(t *testing.T) {
	s, dataDir := newSkillService(t)
	if _, err := s.Create(commitStyle); err != nil {
		t.Fatalf("create: %v", err)
	}
	sysDir := filepath.Join(dataDir, "skillpack", "skills", ".system", "skill-creator")
	if err := os.MkdirAll(sysDir, 0o755); err != nil {
		t.Fatalf("mkdir codex system skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sysDir, "SKILL.md"), []byte("---\nname: skill-creator\n---\n"), 0o644); err != nil {
		t.Fatalf("write codex system skill: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := s.AdoptStrays(); err != nil {
			t.Fatalf("adopt run %d: %v", i+1, err)
		}
	}

	if _, err := os.Stat(filepath.Join(sysDir, "SKILL.md")); err != nil {
		t.Fatalf("codex system skill should stay in the pack dir: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dataDir, "skills", "system")); err == nil {
		t.Fatal("codex system dir must not be adopted as a skill")
	}
	list, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Name != "commit-style" {
		t.Fatalf("list = %+v, want commit-style only", list)
	}
}

// 契约：源目录已有同名技能时绝不覆盖——游离的那份改名收养，用户自己写的
// 内容原样留着。
//
// 真实前提是「已有的那份处于停用」：启用中的技能在分发目录里是软链，codex
// 会顺着软链直接改源文件，根本走不到收养这一步。
func TestSkillService_AdoptStrays_KeepsExistingSkillOnNameClash(t *testing.T) {
	s, dataDir := newSkillService(t)
	mine := SkillCreateInput{
		Name:        "bet-report",
		Description: "我自己写的投注报告口径：只看 GGR 与 RTP，不出玩家排行。",
		Body:        "# bet-report\n\n我的版本：口径以财务对账为准。",
	}
	if _, err := s.Create(mine); err != nil {
		t.Fatalf("create mine: %v", err)
	}
	off := false
	if _, err := s.Update("bet-report", SkillUpdateInput{Enabled: &off}); err != nil {
		t.Fatalf("disable mine: %v", err)
	}
	writeStray(t, dataDir, "bet-report")

	if err := s.AdoptStrays(); err != nil {
		t.Fatalf("adopt: %v", err)
	}

	kept, err := s.Get("bet-report")
	if err != nil {
		t.Fatalf("get mine: %v", err)
	}
	if kept.Body != mine.Body {
		t.Fatalf("existing skill overwritten:\ngot  %q\nwant %q", kept.Body, mine.Body)
	}
	if kept.Enabled {
		t.Fatal("existing skill should keep its disabled state")
	}
	adopted, err := s.Get("bet-report-codex")
	if err != nil {
		t.Fatalf("get adopted copy: %v", err)
	}
	if adopted.Description != betReportDesc {
		t.Fatalf("adopted copy description:\ngot  %q\nwant %q", adopted.Description, betReportDesc)
	}
	if !adopted.Enabled {
		t.Fatal("adopted copy should stay enabled")
	}
}

// 契约：外部来源的目录名归一到技能命名规范（kebab-case）——名字就是路径与
// API 的键，不合规就点不开详情页。
func TestSkillService_AdoptStrays_NormalizesNameToKebabCase(t *testing.T) {
	s, dataDir := newSkillService(t)
	writeStray(t, dataDir, "Bet_Report")

	if err := s.AdoptStrays(); err != nil {
		t.Fatalf("adopt: %v", err)
	}

	if _, err := s.Get("bet-report"); err != nil {
		t.Fatalf("get normalized name: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dataDir, "skills", "Bet_Report")); err == nil {
		t.Fatal("original non-kebab dir should not survive in the source dir")
	}
}
