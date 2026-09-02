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
	// asks 是**同时**挂起的权限/提问，按卡片 nonce 索引。
	// 必须存成多份：agent 会并发发出多个权限请求（真机抓到一轮里两个
	// request_permission 前后脚到），早先这里是单值，后到的会把先到的
	// 静默顶掉——被顶掉那张卡还留在频道里，按钮点了只报「已失效」，
	// 而它对应的工具调用永远等不到裁决，整轮就此卡死（后端的权限等待
	// 没有超时出口）。
	asks map[string]*pendingAsk
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
		if s.mentionsBot(ctx, token, ev, botID) || hasDBToken(ev.Content) {
			go s.startThread(ctx, token, b, ev)
		}
		return
	}
	if b, ok := s.threadBinding(ctx, token, cfg, ev.ChannelID); ok {
		go s.threadInput(ctx, token, b, ev)
		return
	}
	// 既不是绑定频道、也不属于任何绑定频道的子区。被 @ 到还一声不吭的话，
	// 人只会以为 bot 挂了——同一个报障已经出现过一次（见上面那段注释），
	// 那次只修了「绑定频道里的 @db」，没覆盖「压根没绑定的频道」。
	if s.mentionsBot(ctx, token, ev, botID) {
		go s.hintUnbound(ctx, token, ev)
	}
}

// mentionsBot 判断这条消息有没有 @ 到 bot：直接 @ 用户，或 @ 它在这个
// guild 里的集成角色（自动补全里两者并排，选到角色的概率一半一半）。
func (s *Service) mentionsBot(ctx context.Context, token string, ev messageEvent, botID string) bool {
	for _, m := range ev.Mentions {
		if m.ID == botID {
			return true
		}
	}
	if len(ev.MentionRoles) == 0 {
		return false
	}
	role := s.botRoleIn(ctx, token, ev.GuildID)
	for _, r := range ev.MentionRoles {
		if r != "" && r == role {
			return true
		}
	}
	return false
}

// unboundHintTTL 是同一个未绑定频道两次提示之间的最短间隔。
// 一次说清就够了，连着 @ 几次不该收到几条一样的回复。
const unboundHintTTL = 30 * time.Minute

// hintUnbound 在没绑定的频道里回一句「怎么开始」。
func (s *Service) hintUnbound(ctx context.Context, token string, ev messageEvent) {
	s.chatMu.Lock()
	last, seen := s.unboundHinted[ev.ChannelID]
	fresh := seen && time.Since(last) < unboundHintTTL
	if !fresh {
		s.unboundHinted[ev.ChannelID] = time.Now()
	}
	s.chatMu.Unlock()
	if fresh {
		return
	}

	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := botREST(cctx, token, "POST", "/channels/"+ev.ChannelID+"/messages", map[string]any{
		"embeds": []map[string]any{{
			"title": "这个频道还没绑定工作区",
			"description": "用 `/init` 绑一个仓库：选仓库 → base 分支 → 数据库 → 服务器，" +
				"之后在这里 @ 我就会开子区干活。\n\n" +
				"（已经绑过的频道走错了？`/status` 看当前绑定。）",
			"color": colorBlurbe,
		}},
		"message_reference": map[string]any{"message_id": ev.ID, "fail_if_not_exists": false},
		"allowed_mentions":  noMentions(),
	}, nil)
	if err != nil {
		slog.Warn("未绑定频道的提示发送失败", "channel", ev.ChannelID, "err", err)
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
		c.upsertThread(Thread{ThreadID: th.ID, ChannelID: b.ChannelID, Title: threadTitle(text), CreatedAt: time.Now()})
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
	ask, pending := tc.askForAnswer()
	// 挂着问答时纯文字优先尝试当答案；不像答案的（权限卡收到非编号、
	// 多题提问收到闲文本）落回下面照常排队，不吞不怼。
	if ask != nil && text != "" && len(ev.Attachments) == 0 {
		if s.answerAsk(ctx, token, ev.ChannelID, ask, ev.ID, text) {
			return
		}
	} else if pending > 1 && isIndexAnswer(text) {
		// 多张卡同时挂着时编号指不明白是哪一张，与其猜一个不如说清楚
		// ——直接排进对话的话，agent 只会收到一个莫名其妙的数字。
		s.say(ctx, token, ev.ChannelID, fmt.Sprintf(
			"-# 现在有 %d 张卡等着裁决，回编号分不清指哪一张——请点卡片上的按钮。", pending))
		return
	}
	// 打成纯文本的斜杠命令不进对话——把 /help 当消息发出去，agent 只会
	// 回一句「不认识」白费一轮。裸命令词才拦，带正文的不动。
	if slashTypo[text] {
		s.say(ctx, token, ev.ChannelID, "-# 斜杠命令要从输入框弹出的菜单里选（输入 / 会弹出来）。")
		return
	}
	// 消息里带 @db 令牌 = 显式要数据库（默认本来就挂，这条只救
	// /db off 过的子区）：令牌本身不进 prompt。
	if hasDBToken(text) {
		text = stripDBToken(text)
		s.setThreadDB(ev.ChannelID, true)
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
	tc.asks = nil
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

- 回复保持紧凑。结构化成果（盘点、对比、调研、方案、数据报告）不要在对话里铺长文，按 html-report 技能写成单文件报告并用 report_open 打开——它会在频道里立一张卡，带一个点开即看的报告链接。
- 少用宽表格与四级以下标题：Discord 只认有限的 markdown，宽表格在手机上没法读。
- 数据库只经 mcp__acpp-db__* 工具访问。工具清单里没有它们就是数据库面没挂载，此时不要用 ssh、mysql 客户端或任何别的途径碰数据库——告诉用户在消息里带 @db（或用 /db on）挂载后再继续。
- 要把东西给用户（他开口要，或你产出了报告/图表/图片/数据文件），用 mcp__acpp-chat__send_file——只贴路径、或把内容读一遍贴出来，都不等于给了他文件。形态按 as 选：默认 .html 发成渲染后的外链、其余发原文件；**用户明说要「文件」「原件」就传 as=file**，要截图传 as=image。外链拿到的人都能打开，涉密内容一律发文件。
- 外链默认 7 天失效，发布后记住回执里的 id；用户说「看完了」「删了吧」就立刻 mcp__acpp-chat__revoke_link 撤掉，手上没 id 先 list_links 查。
- 写文件一律用你的 Write/Edit 文件工具（自动批准），不要用 cat<<EOF、echo > 这类 bash 写文件；报告图表的数据点直接手写进内联 SVG，不要跑 python/node 脚本生成——每条 bash 都要用户手工批准一次，脚本一多整个流程就在权限卡里泡着。`

// discordInstructionsFor 在通用须知后追加本频道的数据库归属：一个项目的
// prod/pre/dev 三个频道共用同一套提示词，唯一的区别就是这句「你现在对着
// 哪个库」——工具面已经锁死了范围，这句是让模型别去猜别的环境。
func discordInstructionsFor(b Binding) string {
	out := discordInstructions
	if b.DataSourceID != 0 {
		out += fmt.Sprintf(
			"\n- 本频道锁定数据源 **%s**：acpp-db 里只有这一条连接，别的环境这个频道连不到——用户问到别的环境的数据就直说，不要拿手上这个库的数据顶替。",
			dbLine(b))
	}
	// 服务器同理（adr-019）：工具面已经锁死范围，这句是让模型别去猜别的机器。
	if b.ServerID != 0 {
		out += fmt.Sprintf(
			"\n- 本频道锁定服务器 **%s**：acpp-server 里只有这一台，别的机器这个频道看不到。",
			serverLine(b))
	}
	return out
}

// ---- 数据库工具面的按需开关 ----

var dbToken = regexp.MustCompile(`(^|\s)@(db\b|数据库)`)

func hasDBToken(text string) bool { return dbToken.MatchString(text) }

func stripDBToken(text string) string {
	return strings.TrimSpace(dbToken.ReplaceAllString(text, "$1"))
}

// setThreadDB 落盘子区的数据库开关并关掉现有会话——挂载是 session/new
// 与 session/load 的参数，改不了在跑的会话；关掉后下一轮重开时带上新
// 挂载（load 凭 acpSessionId 恢复上下文，load 失败回退 new 时有
// threadHistory 历史衔接兜底）。曾误判「load 不装新挂载」在这里清过
// acpSessionId——真凶后来查明是 workdir 的 @分支 后缀让项目过滤落空
// （datasource projectCandidates 已修），不再白丢上下文。
func (s *Service) setThreadDB(threadID string, on bool) bool {
	changed := false
	if _, err := s.store.update(func(c *Config) {
		if t, ok := c.thread(threadID); ok && t.DBOff != !on {
			t.DBOff = !on
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

// handleDBCommand 分派 /db：带 source 是**频道级**换绑（改这个频道锁定的
// 库，三个环境频道各绑各的靠它），否则是**子区级**的工具面开关。
func (s *Service) handleDBCommand(ctx context.Context, token string, ev interactionEvent) {
	if ev.option("source") != "" {
		s.setBindingOption(ctx, token, ev, "db")
		return
	}
	s.toggleDB(token, ev)
}

// closeChannelThreads 关掉一个频道下所有子区的 acp 会话。挂载（数据库工具
// 面与它锁定的数据源）是 session/new 的参数，改不了在跑的会话——关掉之后
// 下一轮重开时带上新挂载，凭 acpSessionId 走 load 恢复上下文。
func (s *Service) closeChannelThreads(channelID string) {
	if s.acpMgr == nil {
		return
	}
	for _, t := range s.store.config().Threads {
		if t.ChannelID != channelID {
			continue
		}
		if err := s.acpMgr.Close("dc:" + t.ThreadID); err != nil {
			slog.Warn("重开子区会话失败", "thread", t.ThreadID, "err", err)
		}
	}
}

// toggleDB 处理 /db 的开关与状态：只在子区里有意义（挂载是会话级的）。
func (s *Service) toggleDB(token string, ev interactionEvent) {
	t, known := s.store.config().thread(ev.ChannelID)
	if !known {
		s.ephemeral(token, ev, "这里不是工作区子区（或子区还没说过话）。在子区里聊一句之后再用 /db。")
		return
	}
	switch ev.option("switch") {
	case "on":
		s.setThreadDB(ev.ChannelID, true)
		s.ephemeral(token, ev, "🗄️ 数据库工具面已挂载，下一轮生效。")
	case "off":
		s.setThreadDB(ev.ChannelID, false)
		s.ephemeral(token, ev, "数据库工具面已卸载，下一轮生效；/db on 或消息带 @db 再打开。")
	default:
		state := "开（默认）"
		if t.DBOff {
			state = "关"
		}
		scope := "不锁定（按项目过滤）"
		if b, ok := s.bindingForCommand(s.store.config(), ev.ChannelID); ok && b.DataSourceID != 0 {
			scope = dbLine(b)
		}
		s.ephemeral(token, ev, "本子区数据库工具面："+state+"；本频道锁定的库："+scope+
			"。\n-# /db off 卸载、/db on 或消息带 @db 打开、/db source:… 换绑。")
	}
}
