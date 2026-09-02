package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"acpp/server/internal/acp"
	"acpp/server/internal/mcp"
)

// 本文件是子区的回合执行层：开会话（挂载工具面）、跑一轮 prompt、
// 消费会话事件、回合小结与历史衔接。消息分发与队列在 chat.go。

// runTurn 跑一轮：确保会话（load 恢复优先）、对齐绑定设置、发 prompt、
// 轮末把正文分段发回子区。
func (s *Service) runTurn(ctx context.Context, token string, b Binding, threadID string, tc *threadChat, input string, atts []attachment) {
	key := "dc:" + threadID
	sess, fresh, err := s.openChatSession(ctx, key, b, threadID, tc)
	if err != nil {
		s.say(ctx, token, threadID, "❌ 会话启动失败\n-# "+trimRunes(err.Error(), 400))
		return
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
		return
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
	if result.Usage != nil {
		tc.statTokens += result.Usage.TotalTokens
	}
	tc.mu.Unlock()

	s.finalizeToolCard(token, threadID, tc)

	switch {
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
		InstructionsExtra:  discordInstructionsFor(b),
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
