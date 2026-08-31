package discord

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// /skills、/usage、/mcps 与 /git：观察面命令。技能清单直接读技能包目录
// （文件系统即启用状态，与会话注入同一来源）；用量是子区运行态的内存累计；
// git 状态现读工作树。

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
	cfg := s.store.config()
	dbOn := false
	var bind Binding
	if t, ok := cfg.thread(ev.ChannelID); ok {
		dbOn = !t.DBOff
		bind, _ = cfg.binding(t.ChannelID)
	}
	var b strings.Builder
	b.WriteString("## 🔌 本子区的 MCP 工具面\n")
	b.WriteString("- **acpp-chat** — `send_file` 交付文件（附件 / 渲染外链 / 长图三选一）·" +
		" `list_links` 看还挂着哪些外链 · `revoke_link` 撤销外链\n")
	b.WriteString("- **acpp-report** — `report_open`：把写好的 HTML 报告发进子区（卡片 + 点开即看的链接）\n")
	if dbOn {
		scope := "按项目过滤"
		if bind.DataSourceID != 0 {
			scope = "锁定 " + dbLine(bind)
		}
		b.WriteString("- **acpp-db** — `db_sources` `db_tables` `db_schema` `db_query`（可写数据源另有 `db_execute`）：" + scope + "\n")
	} else {
		b.WriteString("- ~~acpp-db~~ — 已被 /db off 卸载。`/db on` 或消息带 `@db` 重新挂上\n")
	}
	b.WriteString("-# 挂载在会话建立时定死；开关数据库面会重开会话（上下文自动恢复）。")
	s.ephemeralKeep(token, ev, b.String())
}

// showGitStatus 回本频道工作树的 git 状态：分支、与远端的差距、改动的文件
// 分类列出。子区里执行看的是父频道那棵树（一个频道一棵，见 clone.go）。
//
// 现读而不是缓存：agent 随时在改文件，隔一轮就不准了。
func (s *Service) showGitStatus(ctx context.Context, token string, ev interactionEvent) {
	b, ok := s.bindingForCommand(s.store.config(), ev.ChannelID)
	if !ok {
		s.ephemeral(token, ev, "这个频道还没绑定工作区，先 /init。")
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	st, err := readGitStatus(cctx, b.Workdir, b.Base)
	if err != nil {
		s.ephemeral(token, ev, "读 git 状态失败："+trimRunes(err.Error(), 300))
		return
	}

	var w strings.Builder
	w.WriteString("## 📂 " + b.Repo + " 的工作树\n")
	branch := st.Branch
	if branch == "" {
		branch = "游离 HEAD"
	}
	w.WriteString("分支 `" + branch + "`")
	// 新分支是从 origin/<base> 切的，upstream 就是它——这时下面那行「基于
	// base」已经把话说完了，不再重复一遍。
	if st.Upstream != "" && st.Upstream != "origin/"+st.Base {
		w.WriteString(" · 对比 `" + st.Upstream + "`")
		switch {
		case st.Ahead > 0 && st.Behind > 0:
			w.WriteString(fmt.Sprintf("：领先 %d、落后 %d", st.Ahead, st.Behind))
		case st.Ahead > 0:
			w.WriteString(fmt.Sprintf("：领先 %d 个提交", st.Ahead))
		case st.Behind > 0:
			w.WriteString(fmt.Sprintf("：落后 %d 个提交", st.Behind))
		default:
			w.WriteString("：同步")
		}
	}
	if st.Base != "" {
		fmt.Fprintf(&w, "\n基于 `%s`", st.Base)
		switch {
		case st.BaseAhead > 0 && st.BaseBehind > 0:
			fmt.Fprintf(&w, "：本分支多 %d 个提交，base 上另有 %d 个新提交（可以合过来）", st.BaseAhead, st.BaseBehind)
		case st.BaseAhead > 0:
			fmt.Fprintf(&w, "：本分支多 %d 个提交（还没合回去）", st.BaseAhead)
		case st.BaseBehind > 0:
			fmt.Fprintf(&w, "：base 上有 %d 个新提交（可以合过来）", st.BaseBehind)
		default:
			w.WriteString("：一致")
		}
	}
	w.WriteString("\n")
	if st.clean() {
		w.WriteString("\n工作树干净，没有未提交的改动。")
		s.ephemeralKeep(token, ev, w.String())
		return
	}
	for _, sec := range []struct {
		title string
		files []string
	}{
		{"⚠️ 冲突", st.Conflicted},
		{"✏️ 修改", st.Modified},
		{"➕ 新增", st.Added},
		{"🗑️ 删除", st.Deleted},
		{"↔️ 重命名", st.Renamed},
	} {
		w.WriteString(fileSection(sec.title, sec.files))
	}
	w.WriteString("-# " + b.Workdir)
	s.ephemeralKeep(token, ev, trimRunes(w.String(), discordMsgLimit-20))
}

// gitFileLimit 是每类最多列出的文件数：Discord 单条消息 2000 字符，一次
// 大改动能有上百个文件，列全了什么都看不清——给个数量与前几条，要细节
// 让 agent 去说。
const gitFileLimit = 12

func fileSection(title string, files []string) string {
	if len(files) == 0 {
		return ""
	}
	var w strings.Builder
	fmt.Fprintf(&w, "\n**%s %d**\n", title, len(files))
	shown := files
	if len(shown) > gitFileLimit {
		shown = shown[:gitFileLimit]
	}
	for _, f := range shown {
		w.WriteString("`" + trimRunes(f, 90) + "`\n")
	}
	if len(files) > len(shown) {
		fmt.Fprintf(&w, "-# …另有 %d 个\n", len(files)-len(shown))
	}
	return w.String()
}
