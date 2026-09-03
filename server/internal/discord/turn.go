package discord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"acpp/server/internal/acp"
	"acpp/server/internal/mcp"
	"acpp/server/internal/schedule"
)

// 本文件是子区的回合执行层：开会话（挂载工具面）、跑一轮 prompt、
// 消费会话事件、回合小结与历史衔接。消息分发与队列在 chat.go。

// turnOutcome 是一轮跑完的观测结果：定时任务的运行管线据此判成败、
// 提摘要；普通对话不看它。
type turnOutcome struct {
	reply   string
	err     error
	stop    acp.StopReason
	tools   int
	tokens  int
	elapsed time.Duration
}

// runTurn 跑一轮：确保会话（load 恢复优先）、对齐绑定设置、发 prompt、
// 轮末把正文分段发回子区。
func (s *Service) runTurn(ctx context.Context, token string, b Binding, threadID string, tc *threadChat, input string, atts []attachment) turnOutcome {
	key := "dc:" + threadID
	sess, fresh, err := s.openChatSession(ctx, key, b, threadID, tc)
	if err != nil {
		if !errors.Is(err, acp.ErrPoolFull) {
			s.say(ctx, token, threadID, "❌ 会话启动失败\n-# "+trimRunes(err.Error(), 400))
		}
		return turnOutcome{err: err}
	}
	// 附件先落盘再转内容块；个别失败只提示，不拦整轮。
	attBlocks, attNotes := s.attachmentBlocks(ctx, b.Workdir, atts)
	for _, n := range attNotes {
		s.say(ctx, token, threadID, n)
	}
	// 每轮前把绑定的模型/深度/权限拨到位——绑定可能刚被 /model 改过。
	s.applyBindingSettings(ctx, key, b)
	_ = sess

	// typing 指示器只活 10 秒，循环续到回合结束。
	tctx, stopTyping := context.WithCancel(ctx)
	defer stopTyping()
	go func() {
		for {
			c, cancel := context.WithTimeout(tctx, 5*time.Second)
			_ = botREST(c, token, "POST", "/channels/"+threadID+"/typing", struct{}{}, nil)
			cancel()
			select {
			case <-tctx.Done():
				return
			case <-time.After(8 * time.Second):
			}
		}
	}()

	blocks := attBlocks
	// 全新上下文 + 子区已有历史 = 挂载变更后的强制重开（load 装不上新
	// 挂载，只能 new）：把子区历史摘录注进开场，对话不断片。
	if fresh {
		if hist := s.threadHistory(ctx, token, threadID); hist != "" {
			blocks = append([]acp.ContentBlock{{Type: "text",
				Text: "（上下文衔接·系统注入）这个子区此前已有对话，你的会话刚重新开始。以下是历史摘录，衔接着回答，不要重复自我介绍：\n\n" + hist}}, blocks...)
		}
	}
	if input != "" {
		blocks = append(blocks, acp.ContentBlock{Type: "text", Text: input})
	}
	if len(blocks) == 0 {
		return turnOutcome{}
	}
	started := time.Now()
	result, err := s.acpMgr.Prompt(ctx, key, blocks)
	stopTyping()

	tc.mu.Lock()
	reply := strings.TrimSpace(tc.buf.String())
	tc.buf.Reset()
	// 轮结束就清空所有挂起的卡：这一轮都收尾了，还没裁决的也不作数了。
	tc.asks = nil
	toolCount := len(tc.toolLog)
	touched := len(tc.touched)
	tc.statTurns++
	tc.statTools += toolCount
	tokens := 0
	if result.Usage != nil {
		tokens = result.Usage.TotalTokens
		tc.statTokens += tokens
	}
	unattended := tc.job != nil
	tc.mu.Unlock()

	s.finalizeToolCard(token, threadID, tc)
	out := turnOutcome{reply: reply, err: err, stop: result.StopReason, tools: toolCount, tokens: tokens, elapsed: time.Since(started)}

	switch {
	case unattended && isNoReport(reply):
		// 巡检无事：NO_REPORT 是给管线看的信号，不是给人看的正文。
	case err != nil:
		s.say(ctx, token, threadID, "❌ 这一轮失败了\n-# "+trimRunes(err.Error(), 400))
	case reply == "":
		// 纯工具轮正常结束就沉默——活干完了没话说不该打扰；异常结束
		//（中止/超限/拒绝）才值得说一声。
		if result.StopReason != acp.StopEndTurn && result.StopReason != "" &&
			result.StopReason != acp.StopCancelled {
			s.say(ctx, token, threadID, fmt.Sprintf("-# ⚠️ 回合中止：%s", result.StopReason))
		}
	default:
		segs := splitMessage(mdToDiscord(reply), discordMsgLimit)
		// 干过活的回合在末段附一行小结（秒答的琐碎问答不值得带尾巴）。
		if note := turnSummary(time.Since(started), toolCount, touched, result.Usage); note != "" {
			if last := segs[len(segs)-1]; len(last)+len(note)+1 <= discordMsgLimit+80 {
				segs[len(segs)-1] = last + "\n" + note
			}
		}
		for _, seg := range segs {
			s.say(ctx, token, threadID, seg)
		}
	}
	return out
}

// openChatSession 打开（或复用）子区的 acp 会话，OnEvent 绑定到该子区的
// 运行态；新拿到的 acpSessionId 落盘供重启后恢复。
func (s *Service) openChatSession(ctx context.Context, key string, b Binding, threadID string, tc *threadChat) (*acp.Session, bool, error) {
	if sess, ok := s.acpMgr.Get(key); ok {
		return sess, false, nil
	}
	rt, err := s.deps.AgentRuntime(ctx, b.Agent)
	if err != nil {
		return nil, false, err
	}
	resume := ""
	if t, ok := s.store.config().thread(threadID); ok {
		resume = t.ACPSessionID
	}
	token := s.store.config().BotToken
	// 数据库工具面与网页会话同源：clone 目录归属的项目配了数据源才挂，
	// 挂载失败不拦对话（数据源是增强，不是前提）。
	var mcpServers []any
	var metaExtra map[string]any
	if s.deps.Mounts != nil {
		withDB := true
		if t, ok := s.store.config().thread(threadID); ok {
			withDB = !t.DBOff
		}
		var mErr error
		mcpServers, metaExtra, mErr = s.deps.Mounts(ctx, key, b.Workdir, b.Agent, withDB, b.DataSourceID, b.ServerID, func(rel, title string) {
			s.reportOpened(token, threadID, b, rel, title)
		})
		if mErr != nil {
			slog.Warn("数据源挂载失败，跳过", "workdir", b.Workdir, "err", mErr)
			mcpServers, metaExtra = nil, nil
		}
	}
	// 定时运行的会话多一段无人值守约定（claude 走系统提示词；codex 没有
	// 注入口，靠开场输入里的同一段）。
	instructions := discordInstructionsFor(b)
	tc.mu.Lock()
	if tc.job != nil {
		instructions += "\n\n" + cronInstructions
	}
	tc.mu.Unlock()
	// 自家 acpp-chat 工具面（send_file）：agent 把文件直接发给用户的出口。
	if cs, cm, cErr := s.chatMounts(threadID, b); cErr != nil {
		slog.Warn("chat 工具面挂载失败，跳过", "err", cErr)
	} else {
		mcpServers = append(mcpServers, cs...)
		metaExtra = mcp.MergeClaudeMounts(metaExtra, cm)
	}
	sess, err := s.acpMgr.Open(ctx, acp.OpenOptions{
		Key:     key,
		Runtime: rt,
		Cwd:     b.Workdir,
		OnEvent: func(ev acp.Event) {
			s.onChatEvent(token, threadID, tc, ev)
		},
		ResumeACPSessionID: resume,
		MCPServers:         mcpServers,
		MetaExtra:          metaExtra,
		InstructionsExtra:  instructions,
	})
	if err != nil {
		return nil, false, err
	}
	if id := sess.ACPSessionID(); id != "" && id != resume {
		if _, err := s.store.update(func(c *Config) {
			if t, ok := c.thread(threadID); ok {
				t.ACPSessionID = id
				c.upsertThread(t)
			}
		}); err != nil {
			slog.Warn("acpSessionId 落盘失败", "err", err)
		}
	}
	// fresh = 这是一条全新上下文（没有恢复到旧 thread）：要么本来就是
	// 新子区，要么挂载变更强制走了 new——后者需要历史衔接。
	return sess, resume == "" || sess.ACPSessionID() != resume, nil
}

// applyBindingSettings 把绑定的模型/思考深度/权限档拨到会话上。
// 失败只记日志——模型清单可能变过，聊天不该因此失败。
func (s *Service) applyBindingSettings(ctx context.Context, key string, b Binding) {
	patch := acp.SettingsPatch{}
	if b.Model != "" {
		patch.Model = &b.Model
	}
	if b.Effort != "" {
		e := acp.Effort(b.Effort)
		patch.Effort = &e
	}
	lv := acp.AccessLevel(b.AccessOrDefault())
	patch.Level = &lv
	if _, err := s.acpMgr.Apply(ctx, key, patch); err != nil {
		slog.Warn("对齐子区会话设置失败", "key", key, "err", err)
	}
}

// onChatEvent 消费子区会话的归一化事件。正文攒进回合缓冲；权限与提问
// 转成子区里的编号问答；其余（思考/工具/计划）不进频道——干净原则。
func (s *Service) onChatEvent(token, threadID string, tc *threadChat, ev acp.Event) {
	switch ev.Kind {
	case acp.EventMessage:
		// 子代理的过程输出不混进主回复（网页端也是单独归属显示）；
		// 子代理的启动与结果在工具活动卡上有它自己的行。
		if ev.SubagentOf != "" {
			return
		}
		tc.mu.Lock()
		tc.buf.WriteString(ev.Text)
		tc.mu.Unlock()
	case acp.EventPermission:
		go s.askPermission(token, threadID, tc, ev)
	case acp.EventElicitation:
		tc.mu.Lock()
		unattended := tc.job != nil
		tc.mu.Unlock()
		if unattended {
			go s.declineElicitation(token, threadID, ev)
			return
		}
		go s.askElicitation(token, threadID, tc, ev)
	case acp.EventPermissionDone, acp.EventElicitationDone:
		go s.askDone(token, threadID, tc, ev.PermissionID+ev.ElicitationID)
	case acp.EventPlan:
		go s.updatePlanCard(token, threadID, tc, ev.Entries)
	case acp.EventToolCall:
		go s.noteToolCall(token, threadID, tc, ev)
	}
}

// turnSummary 是回合结束时的观察小字：耗时 + 工具数 + 改动文件数 +
// token 用量。快问快答（<20s 且没动工具）不带尾巴——那种回合一眼就看
// 完了，小结只是噪音。
func turnSummary(d time.Duration, tools, touched int, usage *acp.Usage) string {
	if d < 20*time.Second && tools == 0 {
		return ""
	}
	t := fmt.Sprintf("%ds", int(d.Seconds()))
	if d >= time.Minute {
		t = fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	out := "-# ⏱ " + t
	if tools > 0 {
		out += fmt.Sprintf(" · 🔧 %d", tools)
	}
	if touched > 0 {
		out += fmt.Sprintf(" · ✏️ %d 个文件", touched)
	}
	if usage != nil && usage.TotalTokens > 0 {
		out += " · 🧮 " + fmtTokens(usage.TotalTokens)
	}
	return out
}

// fmtTokens 把 token 数缩成 12.3k 这种量级读法。
func fmtTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM tok", float64(n)/1_000_000)
	case n >= 1000:
		return fmt.Sprintf("%.1fk tok", float64(n)/1000)
	default:
		return fmt.Sprintf("%d tok", n)
	}
}

// slashTypo 是「打成纯文本的斜杠命令」集合：只拦裸命令词。
var slashTypo = map[string]bool{
	"/help": true, "/db": true, "/stop": true, "/status": true,
	"/model": true, "/effort": true, "/access": true, "/init": true, "/unbind": true,
}

// handleMessageEdit 消费 MESSAGE_UPDATE：还在排队（⏳）的消息，编辑
// 即生效——下一轮带的是新文本。已进入对话的编辑不追溯（那一轮已经跑
// 完了），也不提示：编辑历史记录是用户的自由，bot 不该指手画脚。
func (s *Service) handleMessageEdit(d json.RawMessage) {
	var ev messageEvent
	if err := json.Unmarshal(d, &ev); err != nil || ev.Author.Bot || ev.ID == "" {
		return
	}
	s.chatMu.Lock()
	tc, ok := s.chats[ev.ChannelID]
	s.chatMu.Unlock()
	if !ok {
		return
	}
	text := strings.TrimSpace(stripMention(ev.Content, s.botID()))
	tc.mu.Lock()
	defer tc.mu.Unlock()
	for i := range tc.queue {
		if tc.queue[i].msgID == ev.ID {
			tc.queue[i].text = text
			tc.queue[i].atts = ev.Attachments
			return
		}
	}
}

// threadHistory 从 Discord 拉子区最近的对话摘录（挂载变更强制重开会话
// 后的上下文衔接）。Discord 本身就是对话记录的正源，够用；工具调用细节
// 拿不回来，衔接的是「聊到哪了」不是完整状态。
func (s *Service) threadHistory(ctx context.Context, token, threadID string) string {
	var msgs []struct {
		Content string `json:"content"`
		Author  struct {
			Username string `json:"username"`
			Bot      bool   `json:"bot"`
		} `json:"author"`
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := botREST(cctx, token, "GET", "/channels/"+threadID+"/messages?limit=25", nil, &msgs); err != nil {
		slog.Warn("拉子区历史失败", "err", err)
		return ""
	}
	if len(msgs) < 2 {
		return ""
	}
	// 接口给的是最新在前，倒过来按时间正序拼；卡片消息（无正文）跳过。
	var b strings.Builder
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		text := strings.TrimSpace(m.Content)
		if text == "" {
			continue
		}
		who := m.Author.Username
		if m.Author.Bot {
			who = "你（assistant）"
		}
		b.WriteString(who + "：" + trimRunes(text, 600) + "\n")
	}
	return trimRunes(b.String(), 6000)
}

// ---- 定时任务的运行管线（schedule.Runner）----
//
// 一次定时运行 = 频道里一条起始消息 + 挂在它下面的子区 + 一条全新的 acp
// 会话。起始消息扮演的是普通对话里用户那条 @acpp：往下全是现成管线（工具
// 卡、权限卡、报告卡、send_file、回合小结），跑完子区还能接着聊。
// 与 openclaw 的 isolated 会话同构，多出来的一条是「可续聊」。

// jobRun 是一次定时运行挂在子区运行态上的标记：有它就是无人值守。
type jobRun struct {
	job schedule.Job
	run schedule.Run
}

// jobSessionIdle 是「给定时任务腾位置」的门槛：池满时把空闲超过这么久的
// 子区会话先收掉（上下文在 agent 侧，下次说话 load 回来）。
const jobSessionIdle = 2 * time.Minute

// runJob 跑一次定时任务。资源暂不可用（bot 没连上、会话池满）报
// schedule.ErrBusy 让调度器稍后重试；其余错误算这一次失败。
func (s *Service) runJob(ctx context.Context, job schedule.Job, run schedule.Run) (schedule.Result, error) {
	cfg := s.store.config()
	b, ok := cfg.binding(job.Scope)
	if !ok {
		return schedule.Result{}, fmt.Errorf("频道 %s 没有绑定工作区", job.Scope)
	}
	if !s.connected() || cfg.BotToken == "" {
		return schedule.Result{}, schedule.ErrBusy
	}
	if !s.reserveSeat() {
		return schedule.Result{}, schedule.ErrBusy
	}
	token := cfg.BotToken
	when := run.StartedAt.In(job.Location())
	title := jobThreadTitle(job, when)

	starterID, err := s.postJobStarter(ctx, token, b.ChannelID, jobHeadline(job, when, "🔄 运行中", ""))
	if err != nil {
		return schedule.Result{}, fmt.Errorf("发起始消息: %w", err)
	}
	threadID, err := s.openJobThread(ctx, token, b.ChannelID, starterID, title)
	if err != nil {
		s.editJobStarter(ctx, token, b.ChannelID, starterID, jobHeadline(job, when, "❌ 失败", "开子区失败："+err.Error()))
		return schedule.Result{}, fmt.Errorf("开子区: %w", err)
	}
	if _, err := s.store.update(func(c *Config) {
		c.upsertThread(Thread{ThreadID: threadID, ChannelID: b.ChannelID, Title: title, JobID: job.ID, CreatedAt: time.Now()})
	}); err != nil {
		slog.Warn("定时任务子区记录落盘失败", "err", err)
	}
	s.chatMu.Lock()
	s.chanKind[threadID] = b.ChannelID
	s.chatMu.Unlock()

	tc := s.chatState(threadID)
	tc.mu.Lock()
	tc.running = true
	tc.job = &jobRun{job: job, run: run}
	// 权限卡 @ 任务的创建者：无人值守但不是无人能管。
	if isSnowflake(job.CreatedBy) {
		tc.lastUser = job.CreatedBy
	}
	tc.mu.Unlock()

	out := s.runTurn(ctx, token, b, threadID, tc, jobPreamble(job, run, when)+job.Prompt, nil)

	tc.mu.Lock()
	tc.job = nil
	tc.mu.Unlock()

	if errors.Is(out.err, acp.ErrPoolFull) {
		// 席位刚被抢走：撤掉起始消息与子区交给调度器稍后重试——留着的话
		// 每次重试都在频道里多一条「失败」。
		tc.mu.Lock()
		tc.running = false
		tc.mu.Unlock()
		s.discardJobThread(ctx, token, b.ChannelID, starterID, threadID)
		return schedule.Result{}, schedule.ErrBusy
	}
	// 跑完就把会话席位让出来：定时任务不该占着池子等空闲回收；子区续聊时
	// 凭 acpSessionId load 回来（提示词也随之换回普通对话那套）。
	if err := s.acpMgr.Close("dc:" + threadID); err != nil && !errors.Is(err, acp.ErrNoSession) {
		slog.Warn("关定时任务会话失败", "err", err)
	}
	// 运行期间排进来的用户消息接着跑（队列空则只是交还执行权）。
	go s.runThread(ctx, token, b, threadID, tc)

	res := schedule.Result{Ref: threadID, Tools: out.tools, Tokens: out.tokens}
	var status string
	switch {
	case out.err != nil:
		res.Status, res.Error = schedule.StatusError, out.err.Error()
		status = "❌ 失败"
	case out.stop != "" && out.stop != acp.StopEndTurn:
		res.Status, res.Error = schedule.StatusError, "回合中止："+string(out.stop)
		status = "❌ 中止"
	case isNoReport(out.reply):
		res.Status = schedule.StatusSilent
		status = "✅ 无需汇报"
	default:
		res.Status = schedule.StatusOK
		res.Summary = jobSummary(out.reply)
		status = "✅ " + fmtElapsed(out.elapsed)
		if out.tools > 0 {
			status += fmt.Sprintf(" · 🔧 %d", out.tools)
		}
	}
	note := res.Summary
	if res.Status == schedule.StatusError {
		note = res.Error
	}
	s.editJobStarter(ctx, token, b.ChannelID, starterID, jobHeadline(job, when, status, note))
	if res.Status == schedule.StatusSilent {
		s.archiveThread(ctx, token, threadID)
	}
	return res, nil
}

// reserveSeat 确认会话池还有席位；满了先收掉空闲超过 jobSessionIdle 的
// 子区会话腾位置，仍满则报没有。
func (s *Service) reserveSeat() bool {
	if s.acpMgr.OpenCount() < chatMaxSessions {
		return true
	}
	for _, key := range s.acpMgr.Idle(jobSessionIdle) {
		if err := s.acpMgr.Close(key); err != nil {
			continue
		}
		if s.acpMgr.OpenCount() < chatMaxSessions {
			return true
		}
	}
	return s.acpMgr.OpenCount() < chatMaxSessions
}

// jobDisabled 是调度器自动停用任务后的通报：发在任务所在频道，@ 创建者。
func (s *Service) jobDisabled(job schedule.Job, reason string) {
	cfg := s.store.config()
	if cfg.BotToken == "" {
		return
	}
	text := fmt.Sprintf("⛔ 定时任务 **%s** %s。\n-# 修好原因后用 /cron resume（或网页 Discord 页）重新启用；最近几次的失败原因看 /cron runs。", job.Name, reason)
	mentions := noMentions()
	if isSnowflake(job.CreatedBy) {
		text = "<@" + job.CreatedBy + "> " + text
		mentions = map[string]any{"users": []string{job.CreatedBy}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := botREST(ctx, cfg.BotToken, "POST", "/channels/"+job.Scope+"/messages",
		map[string]any{"content": text, "allowed_mentions": mentions}, nil)
	if err != nil {
		slog.Warn("定时任务停用通报发送失败", "job", job.ID, "err", err)
	}
}

// declineElicitation 是无人值守运行里 agent 提问时的回答：取消，并在子区
// 留一行说明。留一张没人填的表比直接取消更糟——回合会一直挂着。
func (s *Service) declineElicitation(token, threadID string, ev acp.Event) {
	if err := s.acpMgr.ResolveElicitation("dc:"+threadID, ev.ElicitationID,
		acp.ElicitationResult{Action: "cancel"}); err != nil {
		slog.Warn("取消无人值守提问失败", "err", err)
	}
	s.say(context.Background(), token, threadID, "-# 🤖 无人值守运行：agent 的提问已自动取消（按约定它该自行按保守口径继续）。")
}

// postJobStarter 在频道里发这次运行的起始消息，返回消息 id。
func (s *Service) postJobStarter(ctx context.Context, token, channelID, content string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var msg struct {
		ID string `json:"id"`
	}
	err := botREST(cctx, token, "POST", "/channels/"+channelID+"/messages",
		map[string]any{"content": content, "allowed_mentions": noMentions()}, &msg)
	if err != nil {
		return "", err
	}
	return msg.ID, nil
}

// editJobStarter 把起始消息改成终态（状态 + 一行摘要），尽力而为。
func (s *Service) editJobStarter(ctx context.Context, token, channelID, msgID, content string) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := botREST(cctx, token, "PATCH", "/channels/"+channelID+"/messages/"+msgID,
		map[string]any{"content": content, "allowed_mentions": noMentions()}, nil)
	if err != nil {
		slog.Warn("定时任务起始消息更新失败", "err", err)
	}
}

// openJobThread 以起始消息开子区（与 @bot 开子区同一条 REST）。
func (s *Service) openJobThread(ctx context.Context, token, channelID, msgID, title string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var th struct {
		ID string `json:"id"`
	}
	err := botREST(cctx, token, "POST", fmt.Sprintf("/channels/%s/messages/%s/threads", channelID, msgID),
		map[string]any{"name": title, "auto_archive_duration": 1440}, &th)
	if err != nil {
		return "", err
	}
	if th.ID == "" {
		return "", fmt.Errorf("平台没有返回子区 id")
	}
	return th.ID, nil
}

// archiveThread 归档子区（巡检无事时把它收起来，频道里只剩起始消息那一行）。
func (s *Service) archiveThread(ctx context.Context, token, threadID string) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := botREST(cctx, token, "PATCH", "/channels/"+threadID, map[string]any{"archived": true}, nil); err != nil {
		slog.Warn("归档定时任务子区失败", "err", err)
	}
}

// discardJobThread 撤掉一次没跑起来的运行留下的痕迹：子区、起始消息、记录。
func (s *Service) discardJobThread(ctx context.Context, token, channelID, msgID, threadID string) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := botREST(cctx, token, "DELETE", "/channels/"+threadID, nil, nil); err != nil {
		slog.Warn("删定时任务子区失败", "err", err)
	}
	if err := botREST(cctx, token, "DELETE", "/channels/"+channelID+"/messages/"+msgID, nil, nil); err != nil {
		slog.Warn("删定时任务起始消息失败", "err", err)
	}
	if _, err := s.store.update(func(c *Config) { c.removeThread(threadID) }); err != nil {
		slog.Warn("清定时任务子区记录失败", "err", err)
	}
	s.chatMu.Lock()
	delete(s.chats, threadID)
	delete(s.chanKind, threadID)
	s.chatMu.Unlock()
}

// jobPreamble 是每次运行开场的系统注入：运行时事实（几点、上次结果）+
// 无人值守约定。agent 不知道也推不出这些，尤其是「上次运行时间」——巡检
// 类任务的时间窗全靠它。
func jobPreamble(job schedule.Job, run schedule.Run, when time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "（定时任务·系统注入）任务「%s」按计划于 %s 运行", job.Name, when.Format("2006-01-02 15:04 (MST)"))
	if run.Trigger == schedule.TriggerManual {
		b.WriteString("（由人手动触发）")
	}
	b.WriteString("。\n")
	if job.LastRunAt != nil {
		fmt.Fprintf(&b, "上次运行：%s，结果 %s", job.LastRunAt.In(job.Location()).Format("2006-01-02 15:04"), jobStatusWord(job.LastStatus))
		if job.LastSummary != "" {
			fmt.Fprintf(&b, "，摘要：「%s」", trimRunes(job.LastSummary, 200))
		}
		b.WriteString("。\n")
	} else {
		b.WriteString("这是本任务第一次运行。\n")
	}
	b.WriteString("\n" + cronInstructions + "\n\n---- 任务 ----\n")
	return b.String()
}

// jobStatusWord 是运行状态的人话。
func jobStatusWord(status string) string {
	switch status {
	case schedule.StatusOK:
		return "成功"
	case schedule.StatusSilent:
		return "无需汇报"
	case schedule.StatusError:
		return "失败"
	case schedule.StatusSkipped:
		return "跳过"
	case schedule.StatusRunning:
		return "运行中"
	}
	return orDefault(status, "无")
}

// jobHeadline 是起始消息的正文：一行状态 + 可选的一行小字摘要。
func jobHeadline(job schedule.Job, when time.Time, status, note string) string {
	head := fmt.Sprintf("📅 **%s** · %s · %s", trimRunes(job.Name, 80), when.Format("01-02 15:04"), status)
	if note = strings.TrimSpace(note); note != "" {
		head += "\n-# " + trimRunes(strings.ReplaceAll(note, "\n", " "), 300)
	}
	return head
}

// jobThreadTitle 是运行子区的标题（平台上限 100 字符）。
func jobThreadTitle(job schedule.Job, when time.Time) string {
	return trimRunes(job.Name, 80) + " · " + when.Format("01-02 15:04")
}

// isNoReport 判定回复是不是「无事」信号：整条只有 NO_REPORT（允许标点与
// 少量尾巴），照 openclaw 对 NO_REPLY 的口径。
func isNoReport(reply string) bool {
	r := strings.TrimSpace(reply)
	if r == "" {
		return false
	}
	up := strings.ToUpper(r)
	if !strings.HasPrefix(up, "NO_REPORT") && !strings.HasPrefix(up, "NO REPORT") {
		return false
	}
	return len([]rune(r)) <= 300
}

// jobSummary 从回复里提一行摘要给起始消息：第一行有内容的正文，剥掉标题
// 与加粗记号。技能里要求首段写 TL;DR，所以第一行通常就是结论。
func jobSummary(reply string) string {
	inFence := false
	for _, line := range strings.Split(reply, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if inFence || line == "" || strings.HasPrefix(line, "|") || strings.HasPrefix(line, "-#") {
			continue
		}
		line = strings.TrimLeft(line, "#>*- ")
		line = strings.TrimSpace(strings.ReplaceAll(line, "**", ""))
		if line == "" {
			continue
		}
		return trimRunes(line, 200)
	}
	return ""
}

// fmtElapsed 把耗时缩成 4m12s / 38s。
func fmtElapsed(d time.Duration) string {
	if d >= time.Minute {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

// isSnowflake 判断一个字符串像不像 Discord 的 id（纯数字）——网页建的
// 任务 createdBy 是 "web"，不能拿去 @。
func isSnowflake(s string) bool {
	if len(s) < 10 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
