package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"acpp/server/internal/acp"
	"acpp/server/internal/mcp"
)

// 子区对话的资源边界：discord 的并发子区不多，池子收紧；无人值守场景
// 给宽松的轮超时；空闲子进程回收后凭 acpSessionId 随时 load 回来。
const (
	chatMaxSessions = 4
	chatTurnTimeout = 30 * time.Minute
	chatIdleTimeout = 20 * time.Minute
	// discordMsgLimit 是单条消息的安全长度（平台上限 2000，留余量给围栏补缀）。
	discordMsgLimit = 1900
)

// threadChat 是一个子区的对话运行态：输入队列 + 回合执行权 + 当前回合的
// 输出缓冲 + 挂起的问答。全部内存态——对话记录本身就在 Discord 里。
type threadChat struct {
	mu      sync.Mutex
	queue   []queuedMsg
	running bool
	// buf 收当前回合的 agent 正文（OnEvent 是会话级回调，回合开始前重置）。
	buf strings.Builder
	// ask 是挂起的权限/提问，子区的下一条消息优先当作答。
	ask *pendingAsk
	// planMsgID/planGen 是本回合计划卡的消息 id 与更新代号（见 plan.go）。
	planMsgID string
	planGen   uint64
	// toolLog/toolMsgID/toolGen/toolRendered 是本回合工具活动卡的状态
	// （见 tools.go）；toolLog 的长度就是回合小结里的工具调用数。
	toolLog      []toolEntry
	toolMsgID    string
	toolGen      uint64
	toolRendered string
	// touched 收本回合 edit 类工具动过的文件（回合小结报个数）。
	touched map[string]struct{}
	// stat* 是本子区自服务启动以来的累计（内存态，/usage 显示用）。
	statTurns  int
	statTools  int
	statTokens int
	// lastUser 是最近一位发起输入的用户 id——权限/提问卡 @ 它，让
	// 走开的人收到手机推送（无人值守场景的核心闭环）。
	lastUser string
}

// queuedMsg 是排队中的一条输入。queued 标记它曾在回合进行中等待过
// （挂过 ⏳，进入对话时要摘）；mainChannel 非空表示消息在主频道
// （开子区的那条 @），标记要打回那边。
type queuedMsg struct {
	text        string
	atts        []attachment
	msgID       string
	author      string
	mainChannel string
	queued      bool
}

// chatState 取（或建）子区的运行态。
func (s *Service) chatState(threadID string) *threadChat {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	tc, ok := s.chats[threadID]
	if !ok {
		tc = &threadChat{}
		s.chats[threadID] = tc
	}
	return tc
}

// messageEvent 只解消息分发用得到的字段。
type messageEvent struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	GuildID   string `json:"guild_id"`
	Content   string `json:"content"`
	Author    struct {
		ID  string `json:"id"`
		Bot bool   `json:"bot"`
	} `json:"author"`
	Mentions []struct {
		ID string `json:"id"`
	} `json:"mentions"`
	// MentionRoles：@ 到角色时用户 mention 数组是空的——bot 在 guild 里有
	// 一个同名集成角色，自动补全里排在 bot 用户旁边，选到它的概率一半一半，
	// 必须两种都认（实测踩坑：选了角色的 @ 完全没反应）。
	MentionRoles []string     `json:"mention_roles"`
	Attachments  []attachment `json:"attachments"`
}

// handleMessage 消费一条 MESSAGE_CREATE：
//   - 绑定频道里 @bot 的消息 → 开子区、起第一轮；
//   - 已知（或可归属的）子区里的消息 → 作答挂起的问答，否则进入对话；
//   - 其他一律忽略——主频道保持干净，bot 不插嘴。
func (s *Service) handleMessage(ctx context.Context, token string, d json.RawMessage) {
	if s.acpMgr == nil {
		return
	}
	var ev messageEvent
	if err := json.Unmarshal(d, &ev); err != nil || ev.Author.Bot {
		return
	}
	botID := s.botID()

	cfg := s.store.config()
	if b, ok := cfg.binding(ev.ChannelID); ok {
		// 绑定主频道：认 @bot（bot 用户或它的集成角色），也认 @db 令牌
		// ——「@db 查一下」的意图明明白白是在叫 bot，静默忽略只会让人
		// 以为 bot 挂了（真实报障：「为什么我发的消息 ai 不回复」）。
		mentioned := false
		for _, m := range ev.Mentions {
			if m.ID == botID {
				mentioned = true
				break
			}
		}
		if !mentioned && len(ev.MentionRoles) > 0 {
			role := s.botRoleIn(ctx, token, ev.GuildID)
			for _, r := range ev.MentionRoles {
				if r != "" && r == role {
					mentioned = true
					break
				}
			}
		}
		if !mentioned && hasDBToken(ev.Content) {
			mentioned = true
		}
		if mentioned {
			go s.startThread(ctx, token, b, ev)
		}
		return
	}
	if b, ok := s.threadBinding(ctx, token, cfg, ev.ChannelID); ok {
		go s.threadInput(ctx, token, b, ev)
	}
}

// threadBinding 判定一个 channel id 是不是某个绑定频道的子区，是则给出
// 绑定。查过的结论进缓存（含负结论），不然每条杂音消息都要 REST 一次。
func (s *Service) threadBinding(ctx context.Context, token string, cfg Config, channelID string) (Binding, bool) {
	if t, ok := cfg.thread(channelID); ok {
		if b, ok := cfg.binding(t.ChannelID); ok {
			return b, true
		}
		return Binding{}, false
	}
	s.chatMu.Lock()
	kind, cached := s.chanKind[channelID]
	s.chatMu.Unlock()
	if cached {
		if b, ok := cfg.binding(kind); ok {
			return b, true
		}
		return Binding{}, false
	}

	// 现查一次：是绑定频道的公开子区就采纳（用户手动开的子区也能聊）。
	var ch struct {
		Type     int    `json:"type"`
		ParentID string `json:"parent_id"`
		Name     string `json:"name"`
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err := botREST(cctx, token, "GET", "/channels/"+channelID, nil, &ch)
	cancel()
	parent := ""
	if err == nil && (ch.Type == 11 || ch.Type == 12) {
		if _, ok := cfg.binding(ch.ParentID); ok {
			parent = ch.ParentID
			if _, err := s.store.update(func(c *Config) {
				c.upsertThread(Thread{ThreadID: channelID, ChannelID: parent, Title: ch.Name, CreatedAt: time.Now()})
			}); err != nil {
				slog.Warn("子区记录落盘失败", "err", err)
			}
		}
	}
	s.chatMu.Lock()
	s.chanKind[channelID] = parent
	s.chatMu.Unlock()
	if b, ok := cfg.binding(parent); ok && parent != "" {
		return b, true
	}
	return Binding{}, false
}

// startThread 处理主频道的 @bot：以那条消息开子区，问题作为第一轮输入。
func (s *Service) startThread(ctx context.Context, token string, b Binding, ev messageEvent) {
	text := stripMention(ev.Content, s.botID())
	withDB := hasDBToken(text) || hasDBIntent(text)
	if hasDBToken(text) {
		text = stripDBToken(text)
	}
	if text == "" && len(ev.Attachments) == 0 {
		return
	}
	var th struct {
		ID string `json:"id"`
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err := botREST(cctx, token, "POST",
		fmt.Sprintf("/channels/%s/messages/%s/threads", ev.ChannelID, ev.ID),
		map[string]any{"name": threadTitle(text), "auto_archive_duration": 1440}, &th)
	cancel()
	if err != nil || th.ID == "" {
		slog.Error("开子区失败", "err", err)
		return
	}
	if _, err := s.store.update(func(c *Config) {
		c.upsertThread(Thread{ThreadID: th.ID, ChannelID: b.ChannelID, Title: threadTitle(text),
			DBEnabled: withDB, CreatedAt: time.Now()})
	}); err != nil {
		slog.Warn("子区记录落盘失败", "err", err)
	}
	s.chatMu.Lock()
	s.chanKind[th.ID] = b.ChannelID
	s.chatMu.Unlock()
	// 首条输入是主频道那条 @ 消息，进入对话的 ✅ 打在它身上。
	s.enqueue(ctx, token, b, th.ID, queuedMsg{text: text, atts: ev.Attachments, msgID: ev.ID, author: ev.Author.ID, mainChannel: ev.ChannelID})
}

// threadInput 处理子区里的一条用户消息：优先喂给挂起的问答，否则排队进对话。
func (s *Service) threadInput(ctx context.Context, token string, b Binding, ev messageEvent) {
	text := strings.TrimSpace(stripMention(ev.Content, s.botID()))
	if text == "" && len(ev.Attachments) == 0 {
		return
	}
	tc := s.chatState(ev.ChannelID)
	tc.mu.Lock()
	ask := tc.ask
	tc.mu.Unlock()
	// 挂着问答时纯文字优先尝试当答案；不像答案的（权限卡收到非编号、
	// 多题提问收到闲文本）落回下面照常排队，不吞不怼。
	if ask != nil && text != "" && len(ev.Attachments) == 0 {
		if s.answerAsk(ctx, token, ev.ChannelID, ask, ev.ID, text) {
			return
		}
	}
	// 打成纯文本的斜杠命令不进对话——把 /help 当消息发出去，agent 只会
	// 回一句「不认识」白费一轮。裸命令词才拦，带正文的不动。
	if slashTypo[text] {
		s.say(ctx, token, ev.ChannelID, "-# 斜杠命令要从输入框弹出的菜单里选（输入 / 会弹出来）。")
		return
	}
	// 消息里带 @db 令牌 = 引用数据库：当场挂载工具面（重开会话带上，
	// 上下文经 acpSessionId 恢复），令牌本身不进 prompt。意图词也自动
	// 挂——用户显式 /db 拨过的子区除外（显式决定比推断高一级）。
	if hasDBToken(text) {
		text = stripDBToken(text)
		s.setThreadDB(ev.ChannelID, true)
	} else if hasDBIntent(text) {
		if t, ok := s.store.config().thread(ev.ChannelID); ok && !t.DBManual && !t.DBEnabled {
			s.setThreadDB(ev.ChannelID, true)
		}
	}
	s.enqueue(ctx, token, b, ev.ChannelID, queuedMsg{text: text, atts: ev.Attachments, msgID: ev.ID, author: ev.Author.ID})
}

// enqueue 把输入排进子区队列；没有回合在跑就起 runner。回合在跑时给
// 消息标 ⏳（已排队，下一轮带上）——被吞进对话时换成 ✅，用户凭标记
// 分得清「进了对话」和「还在排队」。
func (s *Service) enqueue(ctx context.Context, token string, b Binding, threadID string, msg queuedMsg) {
	tc := s.chatState(threadID)
	tc.mu.Lock()
	busy := tc.running
	msg.queued = busy
	tc.queue = append(tc.queue, msg)
	if msg.author != "" {
		tc.lastUser = msg.author
	}
	if !busy {
		tc.running = true
	}
	tc.mu.Unlock()
	if busy {
		mark := threadID
		if msg.mainChannel != "" {
			mark = msg.mainChannel
		}
		s.react(ctx, token, mark, msg.msgID, "⏳")
		return
	}
	go s.runThread(ctx, token, b, threadID, tc)
}

// runThread 是子区的回合循环：每轮吞掉队列里的全部输入，直到队列干净。
func (s *Service) runThread(ctx context.Context, token string, b Binding, threadID string, tc *threadChat) {
	for {
		tc.mu.Lock()
		if len(tc.queue) == 0 {
			// 「确认队列空」和「交出执行权」必须在同一临界区——分开的话，
			// 收尾窗口里 enqueue 的消息会看到 running=true 只排队不起
			// runner，队列里就此躺一条没人管的消息（真实报障过）。
			tc.running = false
			tc.mu.Unlock()
			return
		}
		batch := tc.queue
		tc.queue = nil
		tc.buf.Reset()
		// 计划卡按回合另起：上一回合的卡定格成历史，这一回合的计划新发。
		tc.planMsgID = ""
		tc.mu.Unlock()

		// 进入对话的标记：排队的摘 ⏳，全部盖 ✅。首条消息在主频道，
		// 标记要打回它所在的频道。
		texts := make([]string, 0, len(batch))
		var atts []attachment
		for _, q := range batch {
			if q.text != "" {
				texts = append(texts, q.text)
			}
			atts = append(atts, q.atts...)
			markChannel := threadID
			if q.mainChannel != "" {
				markChannel = q.mainChannel
			}
			if q.queued {
				s.unreact(ctx, token, markChannel, q.msgID, "⏳")
			}
			s.react(ctx, token, markChannel, q.msgID, "✅")
		}
		s.runTurn(ctx, token, b, threadID, tc, strings.Join(texts, "\n\n"), atts)
	}
}

// react / unreact 给消息加、摘一个 bot 自己的 reaction（尽力而为，
// 平台对 reaction 限速较狠，失败只记日志）。
func (s *Service) react(ctx context.Context, token, channelID, msgID, emoji string) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := botREST(cctx, token, "PUT",
		fmt.Sprintf("/channels/%s/messages/%s/reactions/%s/@me", channelID, msgID, url.PathEscape(emoji)), nil, nil)
	if err != nil {
		slog.Warn("加回执标记失败", "err", err)
	}
}

func (s *Service) unreact(ctx context.Context, token, channelID, msgID, emoji string) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := botREST(cctx, token, "DELETE",
		fmt.Sprintf("/channels/%s/messages/%s/reactions/%s/@me", channelID, msgID, url.PathEscape(emoji)), nil, nil)
	if err != nil {
		slog.Warn("摘回执标记失败", "err", err)
	}
}

// runTurn 跑一轮：确保会话（load 恢复优先）、对齐绑定设置、发 prompt、
// 轮末把正文分段发回子区。
func (s *Service) runTurn(ctx context.Context, token string, b Binding, threadID string, tc *threadChat, input string, atts []attachment) {
	key := "dc:" + threadID
	sess, err := s.openChatSession(ctx, key, b, threadID, tc)
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
	tc.ask = nil
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
func (s *Service) openChatSession(ctx context.Context, key string, b Binding, threadID string, tc *threadChat) (*acp.Session, error) {
	if sess, ok := s.acpMgr.Get(key); ok {
		return sess, nil
	}
	rt, err := s.deps.AgentRuntime(ctx, b.Agent)
	if err != nil {
		return nil, err
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
		withDB := false
		if t, ok := s.store.config().thread(threadID); ok {
			withDB = t.DBEnabled
		}
		var mErr error
		mcpServers, metaExtra, mErr = s.deps.Mounts(ctx, key, b.Workdir, b.Agent, withDB, func(rel, title string) {
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
		InstructionsExtra:  discordInstructions,
	})
	if err != nil {
		return nil, err
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
	return sess, nil
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

// say 往子区发一条纯文本（bot 的对话输出）。
func (s *Service) say(ctx context.Context, token, threadID, text string) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := botREST(cctx, token, "POST", "/channels/"+threadID+"/messages", map[string]any{
		"content":          text,
		"allowed_mentions": noMentions(),
	}, nil)
	if err != nil {
		slog.Error("子区发消息失败", "err", err)
	}
}

// stopThread 处理 /stop：掐掉当前回合、清空排队的输入。
func (s *Service) stopThread(token string, ev interactionEvent) {
	if s.acpMgr == nil {
		s.ephemeral(token, ev, "对话功能没有启用。")
		return
	}
	key := "dc:" + ev.ChannelID
	tc := s.chatState(ev.ChannelID)
	tc.mu.Lock()
	queued := len(tc.queue)
	tc.queue = nil
	tc.ask = nil
	running := tc.running
	tc.mu.Unlock()
	if !running && queued == 0 {
		s.ephemeral(token, ev, "这里没有正在跑的回合。")
		return
	}
	if err := s.acpMgr.Cancel(key); err != nil {
		slog.Warn("中止子区回合失败", "key", key, "err", err)
	}
	s.ephemeral(token, ev, "⏹ 已中止，排队的输入也清掉了。")
}

// stripMention 去掉文本里的 @bot 标记（<@id> 与 <@!id> 两种写法）。
func stripMention(content, botID string) string {
	content = strings.ReplaceAll(content, "<@"+botID+">", "")
	content = strings.ReplaceAll(content, "<@!"+botID+">", "")
	content = roleMention.ReplaceAllString(content, "")
	return strings.TrimSpace(content)
}

var roleMention = regexp.MustCompile(`<@&\d+>`)

// botRoleIn 查 bot 在一个 guild 里的集成角色 id（tags.bot_id 指向自己），
// 结论缓存（含查不到的负结论——空串）。
func (s *Service) botRoleIn(ctx context.Context, token, guildID string) string {
	if guildID == "" {
		return ""
	}
	s.chatMu.Lock()
	role, ok := s.botRoles[guildID]
	s.chatMu.Unlock()
	if ok {
		return role
	}
	var roles []struct {
		ID   string `json:"id"`
		Tags struct {
			BotID string `json:"bot_id"`
		} `json:"tags"`
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err := botREST(cctx, token, "GET", "/guilds/"+guildID+"/roles", nil, &roles)
	cancel()
	found := ""
	if err != nil {
		slog.Warn("查 guild 角色失败", "guild", guildID, "err", err)
	} else {
		for _, r := range roles {
			if r.Tags.BotID == s.botID() {
				found = r.ID
				break
			}
		}
	}
	s.chatMu.Lock()
	s.botRoles[guildID] = found
	s.chatMu.Unlock()
	return found
}

// threadTitle 从首句取子区标题（平台上限 100，取 60 够认）。
func threadTitle(text string) string {
	line := strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
	return trimRunes(line, 60)
}

// splitMessage 把长回复按平台上限分段：优先按行切，超长行硬切；代码围栏
// 被切开时补闭合、下一段重开，别让后半段全乱码。
func splitMessage(text string, limit int) []string {
	if len([]rune(text)) <= limit {
		return []string{text}
	}
	var segs []string
	var cur strings.Builder
	curLen := 0
	fence := ""
	flush := func() {
		if curLen == 0 {
			return
		}
		out := cur.String()
		if fence != "" {
			out += "\n```"
		}
		segs = append(segs, out)
		cur.Reset()
		curLen = 0
		if fence != "" {
			cur.WriteString(fence)
			cur.WriteString("\n")
			curLen = len([]rune(fence)) + 1
		}
	}
	for line := range strings.SplitSeq(text, "\n") {
		runes := []rune(line)
		// 超长单行硬切。
		for len(runes) > limit {
			flush()
			segs = append(segs, string(runes[:limit]))
			runes = runes[limit:]
		}
		if curLen+len(runes)+1 > limit {
			flush()
		}
		if curLen > 0 {
			cur.WriteString("\n")
			curLen++
		}
		cur.WriteString(string(runes))
		curLen += len(runes)
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if fence == "" {
				fence = strings.TrimSpace(line)
			} else {
				fence = ""
			}
		}
	}
	flush()
	return segs
}

func (s *Service) botID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.BotID
}

// discordInstructions 是 discord 子区会话的场景约定，追加在基础提示词后
// （口径与 acp/instructions.go 一致：可判定、带理由、只讲模型不知道的）。
// 第三条是被真实事故逼出来的：数据库面没挂载时，agent 试图 ssh 隧道直连
// 生产库——那种野路子必须在提示词层面焊死。
const discordInstructions = `# Discord 对话须知

你在 Discord 频道的子区里与用户对话，输出以手机可读为准：

- 回复保持紧凑。结构化成果（盘点、对比、调研、方案、数据报告）不要在对话里铺长文，按 html-report 技能写成单文件报告并用 report_open 打开——它会以长图直接出现在频道里。
- 少用宽表格与四级以下标题：Discord 只认有限的 markdown，宽表格在手机上没法读。
- 数据库只经 mcp__acpp-db__* 工具访问。工具清单里没有它们就是数据库面没挂载，此时不要用 ssh、mysql 客户端或任何别的途径碰数据库——告诉用户在消息里带 @db（或用 /db on）挂载后再继续。
- 生成了图片、图表或文件要给用户看时，用 mcp__acpp-chat__send_file 把它发进对话（.html 自动渲染成长图）——只贴路径用户什么都看不到。`

// ---- 数据库工具面的按需开关 ----

var dbToken = regexp.MustCompile(`(^|\s)@(db\b|数据库)`)

func hasDBToken(text string) bool { return dbToken.MatchString(text) }

// dbIntent 识别「这条消息在说数据库」的意图词——比 @db 令牌宽、比常挂
// 精准：漏挂的代价是 AI 没工具只能编数据（真实报障），误挂的代价只是
// 工具清单多五条描述。词表刻意保守，单字「表」「库」不算。
var dbIntent = regexp.MustCompile(`数据库|数据源|数据表|表结构|建表|查库|库里|\s库\s|\s库$|\s表\s|\s表的|\bSQL\b|\bsql\b`)

func hasDBIntent(text string) bool { return dbIntent.MatchString(text) }

func stripDBToken(text string) string {
	return strings.TrimSpace(dbToken.ReplaceAllString(text, "$1"))
}

// setThreadDB 落盘子区的数据库开关并关掉现有会话——挂载是 session/new
// 参数，改不了在跑的会话；关掉后下一轮凭 acpSessionId 无感恢复上下文。
func (s *Service) setThreadDB(threadID string, on bool) bool {
	changed := false
	if _, err := s.store.update(func(c *Config) {
		if t, ok := c.thread(threadID); ok && t.DBEnabled != on {
			t.DBEnabled = on
			c.upsertThread(t)
			changed = true
		}
	}); err != nil {
		slog.Warn("数据库开关落盘失败", "err", err)
		return false
	}
	if changed && s.acpMgr != nil {
		if err := s.acpMgr.Close("dc:" + threadID); err != nil {
			slog.Warn("重开子区会话失败", "err", err)
		}
	}
	return changed
}

// markDBManual 记下「用户显式拨过开关」——此后意图词推断闭嘴。
func (s *Service) markDBManual(threadID string) {
	if _, err := s.store.update(func(c *Config) {
		if t, ok := c.thread(threadID); ok && !t.DBManual {
			t.DBManual = true
			c.upsertThread(t)
		}
	}); err != nil {
		slog.Warn("DBManual 落盘失败", "err", err)
	}
}

// toggleDB 处理 /db：只在子区里有意义（挂载是会话级的）。
func (s *Service) toggleDB(token string, ev interactionEvent) {
	t, known := s.store.config().thread(ev.ChannelID)
	if !known {
		s.ephemeral(token, ev, "这里不是工作区子区（或子区还没说过话）。在子区里聊一句之后再用 /db。")
		return
	}
	switch ev.option("switch") {
	case "on":
		s.setThreadDB(ev.ChannelID, true)
		s.markDBManual(ev.ChannelID)
		s.ephemeral(token, ev, "🗄️ 数据库工具面已挂载，下一轮生效。消息里带 @db 也能直接开。")
	case "off":
		s.setThreadDB(ev.ChannelID, false)
		s.markDBManual(ev.ChannelID)
		s.ephemeral(token, ev, "数据库工具面已卸载，下一轮生效（这个子区不再按意图词自动挂载）。")
	default:
		state := "关（默认）"
		if t.DBEnabled {
			state = "开"
		}
		s.ephemeral(token, ev, "本子区数据库工具面："+state+"。/db on 挂载、/db off 卸载，消息里带 @db 一步开启。")
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
