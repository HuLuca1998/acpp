package discord

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// /skills 与 /usage：观察面命令。技能清单直接读技能包目录（文件系统即
// 启用状态，与会话注入同一来源）；用量是子区运行态的内存累计。

// showSkills 列出注入对话的技能（skillpack/skills 下的启用项）。
func (s *Service) showSkills(token string, ev interactionEvent) {
	dir := filepath.Join(s.deps.SkillpackDir, "skills")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		s.ephemeral(token, ev, "当前没有注入任何技能。")
		return
	}
	var lines []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		desc := skillDescription(filepath.Join(dir, name, "SKILL.md"))
		if desc != "" {
			lines = append(lines, fmt.Sprintf("- **%s** — %s", name, trimRunes(desc, 120)))
		} else {
			lines = append(lines, "- **"+name+"**")
		}
	}
	if len(lines) == 0 {
		s.ephemeral(token, ev, "当前没有注入任何技能。")
		return
	}
	s.ephemeralKeep(token, ev, "## 🧩 注入对话的技能（"+fmt.Sprint(len(lines))+"）\n"+
		strings.Join(lines, "\n")+"\n-# 技能按需触发；管理在 acpp 网页的技能页。")
}

// skillDescription 从 SKILL.md frontmatter 抽 description（够用的轻解析：
// 只认 `description:` 打头的那一行，不引 yaml 库）。
func skillDescription(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "description:"); ok {
			// 只取第一句——frontmatter 里的触发场景枚举太长，清单里放不下。
			v = strings.TrimSpace(v)
			if i := strings.IndexAny(v, "。."); i > 0 {
				v = v[:i+len("。")]
			}
			return v
		}
	}
	return ""
}

// showUsage 显示本子区的累计用量（服务启动以来的内存计数）。
func (s *Service) showUsage(token string, ev interactionEvent) {
	s.chatMu.Lock()
	tc, ok := s.chats[ev.ChannelID]
	s.chatMu.Unlock()
	if !ok {
		s.ephemeral(token, ev, "这个子区还没有对话记录（或服务重启后还没说过话）。")
		return
	}
	tc.mu.Lock()
	turns, tools, tokens := tc.statTurns, tc.statTools, tc.statTokens
	running := tc.running
	tc.mu.Unlock()
	state := "空闲"
	if running {
		state = "回合进行中"
	}
	s.ephemeralKeep(token, ev, fmt.Sprintf(
		"## 📊 本子区用量\n回合 **%d** · 工具调用 **%d** · token **%s**\n-# %s · 自后端本次启动起累计",
		turns, tools, fmtTokens(tokens), state))
}

// showMCPs 列出本子区会话挂载的 MCP 工具面。状态从配置推导（挂载在
// session/new 时定死），不去问 agent——问也问不到。
func (s *Service) showMCPs(token string, ev interactionEvent) {
	_, isThread := s.store.config().thread(ev.ChannelID)
	if !isThread {
		s.ephemeral(token, ev, "在对话子区里用 /mcps 查看挂载的工具面。")
		return
	}
	dbOn := false
	if t, ok := s.store.config().thread(ev.ChannelID); ok {
		dbOn = !t.DBOff
	}
	var b strings.Builder
	b.WriteString("## 🔌 本子区的 MCP 工具面\n")
	b.WriteString("- **acpp-chat** — `send_file`：把文件直接发进对话（HTML 自动渲染成长图）\n")
	b.WriteString("- **acpp-report** — `report_open`：把写好的 HTML 报告以长图发进子区\n")
	if dbOn {
		b.WriteString("- **acpp-db** — `db_sources` `db_tables` `db_schema` `db_query`（可写数据源另有 `db_execute`）：按项目过滤的数据库工具\n")
	} else {
		b.WriteString("- ~~acpp-db~~ — 已被 /db off 卸载。`/db on` 或消息带 `@db` 重新挂上\n")
	}
	b.WriteString("-# 挂载在会话建立时定死；开关数据库面会重开会话（上下文自动恢复）。")
	s.ephemeralKeep(token, ev, b.String())
}
