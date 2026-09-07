package discord

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"acpp/server/internal/schedule"
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
	// 服务器面（adr-019）：配了机器就挂，没有开关；锁定与否是频道绑定的事。
	srvScope := "全部启用的机器"
	if bind.ServerID != 0 {
		srvScope = "锁定 " + serverLine(bind)
	}
	b.WriteString("- **acpp-server** — `server_hosts` `server_info` `server_ls` `server_read` `server_grep`" +
		" `docker_ps` `docker_logs` `docker_inspect` `docker_stats` `server_ps` `server_ports` `server_journal`" +
		"（全只读）：" + srvScope + "\n")
	b.WriteString("-# 挂载在会话建立时定死；开关数据库面、换绑数据库或服务器都会重开会话（上下文自动恢复）。")
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

// ---- /cron 与定时任务卡 ----
//
// 三处入口共享 schedule 服务：子区里 agent 用 acpp-cron 工具建（主路，
// toolface.go）、/cron 看与管、网页 Discord 页（discord.go 的公开方法）。
// 任务卡是子区里的常驻把手：立即运行 / 停用 / 删除，不用记 id。

// jobPrefix 是任务卡按钮的 custom_id 前缀：cj:<动作>:<任务 id>[:<卡片消息 id>]。
const jobPrefix = "cj:"

// handleCronCommand 分派 /cron：不带 action 看清单；其余动作按 id 或名字前缀找任务。
func (s *Service) handleCronCommand(ctx context.Context, token string, ev interactionEvent) {
	if s.sched == nil {
		s.ephemeral(token, ev, "定时任务未启用（后端没配调度器）。")
		return
	}
	cfg := s.store.config()
	b, ok := s.bindingForCommand(cfg, ev.ChannelID)
	if !ok {
		s.ephemeral(token, ev, "这个频道没绑定工作区，先 /init。")
		return
	}
	action := orDefault(ev.option("action"), "list")
	jobs := s.sched.Jobs(b.ChannelID)
	if action == "list" {
		if len(jobs) == 0 {
			s.ephemeralKeep(token, ev, "本频道还没有定时任务。\n-# 在子区里对 AI 说「以后每天早上 10 点……发到这个频道」它就会建一条；网页 Discord 页也能建。")
			return
		}
		var sb strings.Builder
		sb.WriteString("## 📅 本频道的定时任务\n")
		for _, j := range jobs {
			sb.WriteString(jobLine(j) + "\n")
		}
		sb.WriteString("-# /cron action:run|pause|resume|runs|remove id:<id 或名字前缀，remove 可逗号分隔多条> · action:clear 删光")
		s.ephemeralKeep(token, ev, sb.String())
		return
	}
	// 删除走多条通路：id 逗号分隔；clear 一次带走整批，必须二次确认。
	switch action {
	case "remove":
		picks, err := findJobs(jobs, ev.option("id"))
		if err != nil {
			s.ephemeral(token, ev, err.Error())
			return
		}
		s.ephemeral(token, ev, s.removeJobs(picks))
		return
	case "clear":
		s.confirmClear(token, ev, b.ChannelID, len(jobs))
		return
	}
	job, err := findJob(jobs, ev.option("id"))
	if err != nil {
		s.ephemeral(token, ev, err.Error())
		return
	}
	switch action {
	case "run":
		s.ephemeral(token, ev, s.triggerJob(job.ID))
	case "pause", "resume":
		on := action == "resume"
		upd, err := s.sched.Update(job.ID, schedule.Patch{Enabled: &on})
		if err != nil {
			s.ephemeral(token, ev, "改不了："+trimRunes(err.Error(), 200))
			return
		}
		if on {
			s.ephemeral(token, ev, "▶️ 已启用 **"+upd.Name+"**，下次 "+nextText(upd)+"。")
		} else {
			s.ephemeral(token, ev, "⏸ 已停用 **"+upd.Name+"**；/cron action:resume 恢复。")
		}
	case "runs":
		s.ephemeralKeep(token, ev, runsText(job))
	default:
		s.ephemeral(token, ev, "不认识的动作："+action)
	}
}

// findJobs 解析 remove 的 id 参数：逗号分隔的多个 id / 名字前缀，每段按
// findJob 的规则各自匹配（任一段对不上整条拒绝，免得删掉一半才报错），
// 重复命中只算一次。整个参数为空时退回 findJob 的单条语义。
func findJobs(jobs []schedule.Job, ref string) ([]schedule.Job, error) {
	parts := strings.Split(ref, ",")
	var out []schedule.Job
	seen := map[string]bool{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" && len(parts) > 1 {
			continue // 「a, b,」的尾逗号不算一段
		}
		j, err := findJob(jobs, p)
		if err != nil {
			return nil, err
		}
		if seen[j.ID] {
			continue
		}
		seen[j.ID] = true
		out = append(out, j)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("没指定要删哪条（/cron 看清单）")
	}
	return out, nil
}

// removeJobs 逐条删除并汇总成一句回执：批量删到一半失败时，用户得知道
// 哪几条没了、哪几条还在。
func (s *Service) removeJobs(jobs []schedule.Job) string {
	var done, failed []string
	for _, j := range jobs {
		if err := s.sched.Remove(j.ID); err != nil && !errors.Is(err, schedule.ErrNotFound) {
			failed = append(failed, j.Name+"（"+trimRunes(err.Error(), 80)+"）")
			continue
		}
		done = append(done, j.Name)
	}
	var sb strings.Builder
	switch len(done) {
	case 0:
	case 1:
		sb.WriteString("🗑 已删除 **" + done[0] + "**。")
	default:
		sb.WriteString(fmt.Sprintf("🗑 已删除 %d 条：**%s**。", len(done), strings.Join(done, "**、**")))
	}
	if len(failed) > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("删不掉：" + strings.Join(failed, "；"))
	}
	return sb.String()
}

// confirmClear 是 /cron action:clear 的第一步：弹一张只有本人看得见的确认
// 卡。与任务卡的删除、外链的撤销同一套两步——清空不可逆，而且一次带走的
// 是整个频道的任务，误触的代价比单条删大得多。
func (s *Service) confirmClear(token string, ev interactionEvent, channelID string, n int) {
	if n == 0 {
		s.ephemeral(token, ev, "本频道没有定时任务。")
		return
	}
	err := interactionCallback(token, ev.ID, ev.Token, 4, map[string]any{
		"flags": 1<<6 | 1<<15,
		"components": v2Container(colorRed, []map[string]any{
			v2Text(fmt.Sprintf("### 删光本频道的 %d 条定时任务？", n)),
			v2Text("-# 不可恢复，每条任务的提示词一并没了。只是暂时不跑的话逐条「停用」（/cron action:pause）。"),
			v2Row(v2DangerButton("确认全部删除", jobPrefix+"clr:"+channelID)),
		}),
	})
	if err != nil {
		slog.Warn("清空确认卡弹出失败", "err", err)
	}
}

// findJob 按 id 或名字前缀在清单里找唯一的一条；只有一条时可以不指定。
func findJob(jobs []schedule.Job, ref string) (schedule.Job, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		if len(jobs) == 1 {
			return jobs[0], nil
		}
		return schedule.Job{}, fmt.Errorf("本频道有 %d 条任务，用 id 参数指定一条（/cron 看清单）", len(jobs))
	}
	var hits []schedule.Job
	for _, j := range jobs {
		if j.ID == ref {
			return j, nil
		}
		if strings.HasPrefix(strings.ToLower(j.Name), strings.ToLower(ref)) {
			hits = append(hits, j)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return schedule.Job{}, fmt.Errorf("没有 id 或名字以「%s」开头的任务", ref)
	}
	return schedule.Job{}, fmt.Errorf("「%s」匹配到 %d 条任务，请用 id", ref, len(hits))
}

// triggerJob 手动触发一次，返回给人看的那句话。
func (s *Service) triggerJob(id string) string {
	err := s.sched.RunNow(id)
	switch {
	case errors.Is(err, schedule.ErrRunning):
		return "这条任务正在运行中，等它跑完再试。"
	case errors.Is(err, schedule.ErrNotFound):
		return "任务已经不存在了。"
	case err != nil:
		return "触发失败：" + trimRunes(err.Error(), 200)
	}
	return "▶️ 已触发，频道里马上会出现这次运行的起始消息与子区。"
}

// nextText 是下次运行时刻的展示（任务时区）。
func nextText(j schedule.Job) string {
	if j.NextRunAt == nil {
		return "—"
	}
	return j.NextRunAt.In(j.Location()).Format("01-02 15:04")
}

// jobLine 是清单里的一条：状态 · 名字 · 计划 · 下次 · 上次，第二行小字 id。
func jobLine(j schedule.Job) string {
	state := "🟢"
	switch {
	case j.Running:
		state = "🔄"
	case !j.Enabled:
		state = "⏸"
	}
	line := fmt.Sprintf("%s **%s** · %s", state, j.Name, j.Describe())
	if j.Enabled && j.NextRunAt != nil {
		line += " · 下次 " + nextText(j)
	}
	if j.LastRunAt != nil {
		line += " · 上次 " + statusMark(j.LastStatus) + " " + j.LastRunAt.In(j.Location()).Format("01-02 15:04")
	}
	if j.DisabledReason != "" {
		line += " · " + j.DisabledReason
	}
	return line + "\n-# id `" + j.ID + "`"
}

// statusMark 是运行状态的图标。
func statusMark(status string) string {
	switch status {
	case schedule.StatusOK:
		return "✅"
	case schedule.StatusSilent:
		return "✅"
	case schedule.StatusError:
		return "❌"
	case schedule.StatusSkipped:
		return "⏭"
	case schedule.StatusRunning:
		return "🔄"
	}
	return "·"
}

// runsText 是最近运行记录（新的在前，最多 10 条）。
func runsText(j schedule.Job) string {
	if len(j.Runs) == 0 {
		return "**" + j.Name + "** 还没跑过。"
	}
	var sb strings.Builder
	sb.WriteString("## 📅 " + j.Name + " · 最近运行\n")
	loc := j.Location()
	n := 0
	for i := len(j.Runs) - 1; i >= 0 && n < 10; i-- {
		r := j.Runs[i]
		n++
		line := statusMark(r.Status) + " " + r.StartedAt.In(loc).Format("01-02 15:04")
		if r.EndedAt != nil && r.Status != schedule.StatusSkipped {
			line += " · " + fmtElapsed(r.EndedAt.Sub(r.StartedAt))
		}
		if r.Tools > 0 {
			line += fmt.Sprintf(" · 🔧 %d", r.Tools)
		}
		if r.Trigger == schedule.TriggerManual {
			line += " · 手动"
		}
		switch {
		case r.Error != "":
			line += "\n-# " + trimRunes(r.Error, 200)
		case r.Summary != "":
			line += "\n-# " + trimRunes(r.Summary, 200)
		}
		sb.WriteString(line + "\n")
	}
	return sb.String()
}

// postJobCard 往子区发（或重发）任务卡。
func (s *Service) postJobCard(token, threadID string, job schedule.Job) {
	if token == "" {
		return
	}
	s.postCard(token, threadID, map[string]any{
		"flags": 1 << 15, "components": jobCard(job), "allowed_mentions": noMentions(),
	})
}

// jobCard 是任务卡：名字、计划与状态、prompt 摘录、三个把手。绿色——它是
// 一次对话的成果（adr-017 §11 的色彩语言）。
func jobCard(job schedule.Job) []map[string]any {
	state := "🟢 启用"
	toggle := v2Button("⏸ 停用", jobPrefix+"toggle:"+job.ID, 2)
	if !job.Enabled {
		state = "⏸ 已停用"
		if job.DisabledReason != "" {
			state += "（" + job.DisabledReason + "）"
		}
		toggle = v2Button("▶️ 启用", jobPrefix+"toggle:"+job.ID, 2)
	}
	meta := "-# " + job.Describe() + " · " + state
	if job.Enabled && job.NextRunAt != nil {
		meta += " · 下次 " + nextText(job)
	}
	if job.LastRunAt != nil {
		meta += " · 上次 " + statusMark(job.LastStatus) + " " + job.LastRunAt.In(job.Location()).Format("01-02 15:04")
	}
	inner := []map[string]any{
		v2Text("### 📅 定时任务《" + trimRunes(job.Name, 80) + "》"),
		v2Text(meta),
		v2Text(">>> " + trimRunes(job.Prompt, 600)),
		v2Text("-# id `" + job.ID + "` · 到点在频道里开一个子区自动跑，跑完可以在子区里追问"),
		v2Row(v2Button("▶️ 立即运行", jobPrefix+"run:"+job.ID, 1), toggle,
			v2DangerButton("🗑 删除", jobPrefix+"rm:"+job.ID)),
	}
	return v2Container(colorGreen, inner)
}

// removedJobCard 是删除后的终态卡。
func removedJobCard(name, by string) []map[string]any {
	return v2Container(colorGrey, []map[string]any{
		v2Text("### 🗑 定时任务已删除"),
		v2Text("-# 《" + trimRunes(name, 80) + "》· 由 " + by + " 删除"),
	})
}

// v2Button 是普通按钮：style 1 主色、2 次要。
func v2Button(label, customID string, style int) map[string]any {
	return map[string]any{"type": 2, "style": style, "label": label, "custom_id": customID}
}

// handleJobButton 处理任务卡上的按钮：cj:run / cj:toggle / cj:rm（弹确认）/
// cj:rmk（真删）；以及 /cron action:clear 确认卡上的 cj:clr（清空整个频道）。
func (s *Service) handleJobButton(ctx context.Context, token string, ev interactionEvent) {
	rest := strings.TrimPrefix(ev.Data.CustomID, jobPrefix)
	action, id, _ := strings.Cut(rest, ":")
	if s.sched == nil {
		s.ephemeral(token, ev, "定时任务未启用。")
		return
	}
	switch action {
	case "run":
		s.ephemeral(token, ev, s.triggerJob(id))
	case "toggle":
		job, ok := s.sched.Get(id)
		if !ok {
			s.ephemeral(token, ev, "任务已经不存在了。")
			return
		}
		on := !job.Enabled
		job, err := s.sched.Update(id, schedule.Patch{Enabled: &on})
		if err != nil {
			s.ephemeral(token, ev, "改不了："+trimRunes(err.Error(), 200))
			return
		}
		s.patchCard(ctx, token, ev.ChannelID, ev.Message.ID, jobCard(job))
		if on {
			s.ephemeral(token, ev, "▶️ 已启用，下次 "+nextText(job)+"。")
		} else {
			s.ephemeral(token, ev, "⏸ 已停用；点「启用」或 /cron action:resume 恢复。")
		}
	case "rm":
		// 与外链撤销同一套两步确认：卡片挂在子区里人人可点。
		err := interactionCallback(token, ev.ID, ev.Token, 4, map[string]any{
			"flags": 1<<6 | 1<<15,
			"components": v2Container(colorRed, []map[string]any{
				v2Text("### 删除这条定时任务？"),
				v2Text("-# 不可恢复，任务的提示词一并没了。只是暂时不跑的话用「停用」。"),
				v2Row(v2DangerButton("确认删除", jobPrefix+"rmk:"+id+":"+ev.Message.ID)),
			}),
		})
		if err != nil {
			slog.Warn("删除确认卡弹出失败", "err", err)
		}
	case "rmk":
		jobID, cardID, _ := strings.Cut(id, ":")
		if err := interactionCallback(token, ev.ID, ev.Token, 6, nil); err != nil {
			slog.Warn("删除 deferred 回调失败", "err", err)
			return
		}
		job, _ := s.sched.Get(jobID)
		name := orDefault(job.Name, "（已不存在）")
		if err := s.sched.Remove(jobID); err != nil && !errors.Is(err, schedule.ErrNotFound) {
			s.followup(ctx, ev.Token, colorRed, "### 删除失败\n-# "+trimRunes(err.Error(), 300))
			return
		}
		s.patchCard(ctx, token, ev.ChannelID, cardID, removedJobCard(name, ev.user()))
		s.followup(ctx, ev.Token, colorGrey, "### 🗑 已删除\n-# 《"+trimRunes(name, 80)+"》不会再跑了。")
	case "clr":
		// id 段装的是频道 id（见 confirmClear）：/cron 可能在子区里敲，
		// ev.ChannelID 是子区，任务却挂在父频道名下，所以不能现场推。
		if err := interactionCallback(token, ev.ID, ev.Token, 6, nil); err != nil {
			slog.Warn("清空 deferred 回调失败", "err", err)
			return
		}
		jobs := s.sched.Jobs(id)
		if len(jobs) == 0 {
			s.followup(ctx, ev.Token, colorGrey, "### 本频道已经没有定时任务了")
			return
		}
		s.followup(ctx, ev.Token, colorGrey, "### 🗑 已清空\n-# "+s.removeJobs(jobs))
	default:
		s.ephemeral(token, ev, "这个按钮已失效。")
	}
}
