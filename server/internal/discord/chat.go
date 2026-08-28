package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"acpp/server/internal/acp"
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
}

// queuedMsg 是排队中的一条输入。queued 标记它曾在回合进行中等待过
// （挂过 ⏳，进入对话时要摘）；mainChannel 非空表示消息在主频道
// （开子区的那条 @），标记要打回那边。
type queuedMsg struct {
	text        string
	atts        []attachment
	msgID       string
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
	Attachments []attachment `json:"attachments"`
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
		// 绑定主频道：只认 @bot。
		for _, m := range ev.Mentions {
			if m.ID == botID {
				go s.startThread(ctx, token, b, ev)
				return
			}
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
		c.upsertThread(Thread{ThreadID: th.ID, ChannelID: b.ChannelID, Title: threadTitle(text), CreatedAt: time.Now()})
	}); err != nil {
		slog.Warn("子区记录落盘失败", "err", err)
	}
	s.chatMu.Lock()
	s.chanKind[th.ID] = b.ChannelID
	s.chatMu.Unlock()
	// 首条输入是主频道那条 @ 消息，进入对话的 ✅ 打在它身上。
	s.enqueue(ctx, token, b, th.ID, queuedMsg{text: text, atts: ev.Attachments, msgID: ev.ID, mainChannel: ev.ChannelID})
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
	// 挂着问答时纯文字优先当答案；带附件的消息不像答案，照常排队。
	if ask != nil && text != "" && len(ev.Attachments) == 0 {
		s.answerAsk(ctx, token, ev.ChannelID, ask, ev.ID, text)
		return
	}
	s.enqueue(ctx, token, b, ev.ChannelID, queuedMsg{text: text, atts: ev.Attachments, msgID: ev.ID})
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
	defer func() {
		tc.mu.Lock()
		tc.running = false
		tc.mu.Unlock()
	}()
	for {
		tc.mu.Lock()
		if len(tc.queue) == 0 {
			tc.mu.Unlock()
			return
		}
		batch := tc.queue
		tc.queue = nil
		tc.buf.Reset()
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
		s.say(ctx, token, threadID, "❌ 拉不起 agent："+trimRunes(err.Error(), 500))
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
	result, err := s.acpMgr.Prompt(ctx, key, blocks)
	stopTyping()

	tc.mu.Lock()
	reply := strings.TrimSpace(tc.buf.String())
	tc.buf.Reset()
	tc.ask = nil
	tc.mu.Unlock()

	switch {
	case err != nil:
		s.say(ctx, token, threadID, "❌ 这一轮失败了："+trimRunes(err.Error(), 500))
	case reply == "":
		// 纯工具轮正常结束就沉默——活干完了没话说不该打扰；异常结束
		//（中止/超限/拒绝）才值得说一声。
		if result.StopReason != acp.StopEndTurn && result.StopReason != "" &&
			result.StopReason != acp.StopCancelled {
			s.say(ctx, token, threadID, fmt.Sprintf("（这一轮没说完：%s）", result.StopReason))
		}
	default:
		for _, seg := range splitMessage(reply, discordMsgLimit) {
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
		var mErr error
		mcpServers, metaExtra, mErr = s.deps.Mounts(ctx, b.Workdir, b.Agent)
		if mErr != nil {
			slog.Warn("数据源挂载失败，跳过", "workdir", b.Workdir, "err", mErr)
			mcpServers, metaExtra = nil, nil
		}
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
		tc.mu.Lock()
		tc.buf.WriteString(ev.Text)
		tc.mu.Unlock()
	case acp.EventPermission:
		go s.askPermission(token, threadID, tc, ev)
	case acp.EventElicitation:
		go s.askElicitation(token, threadID, tc, ev)
	case acp.EventPermissionDone, acp.EventElicitationDone:
		go s.askDone(token, threadID, tc, ev.PermissionID+ev.ElicitationID)
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
	return strings.TrimSpace(content)
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
