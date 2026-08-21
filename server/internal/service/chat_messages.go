package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"acpp/server/internal/model"
)

// rebuildCacheEntry 是一次转录重建的结果，键定在文件的 (size, mtime) 上：
// 转录是 append-only 的，两者都没变就没有新内容，重建结果必然一致。
// 消息切片对外只读（Messages 只做 reslice），可以安全共享。
type rebuildCacheEntry struct {
	size    int64
	modTime time.Time
	msgs    []model.Message
	lastUse time.Time
}

// maxRebuildCache 是缓存的会话数上限。长会话的重建结果几 MB 起步，
// 无限攒着会把内存吃穿；本地面板同时在看的会话就那么几条，超限淘汰
// 最久未用的即可。
const maxRebuildCache = 8

func (s *ChatService) rebuildAll(sessionID uint) ([]model.Message, error) {
	// stat 在 Read 之前取：读的过程中转录可能又追加了，用读前的快照
	// 存缓存宁可下次多判一回失效，也绝不把旧内容标成新版本。
	info, err := os.Stat(s.transcripts.Path(sessionKey(sessionID)))
	if err != nil {
		if os.IsNotExist(err) {
			// 新会话还没有转录，与 Store.Read 的语义一致：空列表。
			return nil, nil
		}
		return nil, fmt.Errorf("stat transcript: %w", err)
	}

	s.rebuildMu.Lock()
	if e, ok := s.rebuilds[sessionID]; ok && e.size == info.Size() && e.modTime.Equal(info.ModTime()) {
		e.lastUse = time.Now()
		msgs := e.msgs
		s.rebuildMu.Unlock()
		return msgs, nil
	}
	s.rebuildMu.Unlock()

	entries, err := s.transcripts.Read(sessionKey(sessionID))
	if err != nil {
		return nil, fmt.Errorf("read transcript: %w", err)
	}
	msgs := RebuildMessages(sessionID, entries)

	s.rebuildMu.Lock()
	s.rebuilds[sessionID] = &rebuildCacheEntry{
		size:    info.Size(),
		modTime: info.ModTime(),
		msgs:    msgs,
		lastUse: time.Now(),
	}
	if len(s.rebuilds) > maxRebuildCache {
		var oldest uint
		var oldestUse time.Time
		for id, e := range s.rebuilds {
			if oldest == 0 || e.lastUse.Before(oldestUse) {
				oldest, oldestUse = id, e.lastUse
			}
		}
		delete(s.rebuilds, oldest)
	}
	s.rebuildMu.Unlock()
	return msgs, nil
}

// Messages 从转录重建消息并按尾部分页：limit<=0 表示全量；
// before>0 时只取 id 小于它的那一段的尾部（「加载更早」的游标——
// 转录是 append-only 的，重建 id 按行号递增，跨请求稳定）。
// total 是全量条数，供前端判断还有没有更早的。
func (s *ChatService) Messages(sessionID uint, limit int, before uint) ([]model.Message, int, error) {
	all, err := s.rebuildAll(sessionID)
	if err != nil {
		return nil, 0, err
	}
	total := len(all)

	if before > 0 {
		cut := len(all)
		for i, m := range all {
			if m.ID >= before {
				cut = i
				break
			}
		}
		all = all[:cut]
	}
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}
	return slimMessages(all), total, nil
}

// rawOutputLimit 是消息列表里单条工具输出的预览上限。历史加载的优先级
// 是正文先行：工具卡默认折叠，它的大输出没道理在打开会话时就全量下发
// ——超限的截成预览 + 标记，前端展开那一刻经 ToolOutput 按需取全量。
// 转录里保留完整原文，截断只发生在列表读路径。
const rawOutputLimit = 4 * 1024

// rawInputLimit / rawInputFieldLimit：入参通常是命令行这类小字段，但
// Write 类工具会把整个文件内容塞进来。整体超限时把超长字符串字段截成
// 预览，命令、路径这类关键小字段保持完好。
const rawInputLimit = 4 * 1024
const rawInputFieldLimit = 2 * 1024

// slimMessages 把超大的工具输出截到上限并打上标记供界面说明。
// 消息来自共享的重建缓存，只能拷贝不能就地改。
func slimMessages(msgs []model.Message) []model.Message {
	out := msgs
	copied := false
	for i, m := range msgs {
		slim, changed := slimPayload(m.Payload)
		if !changed {
			continue
		}
		if !copied {
			out = make([]model.Message, len(msgs))
			copy(out, msgs)
			copied = true
		}
		out[i].Payload = slim
	}
	return out
}

func slimPayload(p model.JSONMap) (model.JSONMap, bool) {
	if p == nil {
		return p, false
	}
	// 数据库查询整体豁免：语句在 rawInput、结果表格要完整 JSON 才解析
	// 得出来，截了功能就没了。
	if ri, ok := p["rawInput"].(json.RawMessage); ok {
		var probe struct {
			SQL string `json:"sql"`
		}
		if json.Unmarshal(ri, &probe) == nil && probe.SQL != "" {
			return p, false
		}
	}

	var next model.JSONMap
	mutable := func() model.JSONMap {
		if next == nil {
			next = make(model.JSONMap, len(p)+2)
			maps.Copy(next, p)
		}
		return next
	}

	if raw, ok := p["rawOutput"].(json.RawMessage); ok && len(raw) > rawOutputLimit {
		if slim, ok := truncateRawOutput(raw); ok {
			m := mutable()
			m["rawOutput"] = slim
			m["rawOutputTruncated"] = true
		}
	}
	if raw, ok := p["rawInput"].(json.RawMessage); ok && len(raw) > rawInputLimit {
		if slim, ok := truncateRawInput(raw); ok {
			m := mutable()
			m["rawInput"] = slim
			m["rawInputTruncated"] = true
		}
	}
	if next == nil {
		return p, false
	}
	return next, true
}

// truncateRawInput 把入参对象里超长的字符串字段截成预览，其余原样。
// 认不出对象形状的原样保留。
func truncateRawInput(raw json.RawMessage) (json.RawMessage, bool) {
	var obj map[string]any
	if json.Unmarshal(raw, &obj) != nil {
		return nil, false
	}
	changed := false
	for k, v := range obj {
		if s, ok := v.(string); ok && len(s) > rawInputFieldLimit {
			obj[k] = cutText(s, rawInputFieldLimit)
			changed = true
		}
	}
	if !changed {
		return nil, false
	}
	return marshalRaw(obj)
}

// truncateRawOutput 认 rawOutput 的三种线级形状（与前端 tool-call 的
// outputOf 对齐）：纯字符串、MCP content 数组、{formatted_output} 对象。
// 认不出的形状原样保留——宁可响应大，也不能发出截坏的 JSON。
func truncateRawOutput(raw json.RawMessage) (json.RawMessage, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return marshalRaw(cutText(s, rawOutputLimit))
	}
	var arr []map[string]any
	if json.Unmarshal(raw, &arr) == nil {
		budget := rawOutputLimit
		for _, part := range arr {
			if text, ok := part["text"].(string); ok {
				kept := cutText(text, budget)
				budget -= len(kept)
				part["text"] = kept
			}
		}
		return marshalRaw(arr)
	}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) == nil {
		if text, ok := obj["formatted_output"].(string); ok {
			obj["formatted_output"] = cutText(text, rawOutputLimit)
			return marshalRaw(obj)
		}
	}
	return nil, false
}

// cutText 按字节上限截断，退到合法的 UTF-8 边界（最多回退 3 字节）。
func cutText(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(s) <= limit {
		return s
	}
	cut := s[:limit]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

func marshalRaw(v any) (json.RawMessage, bool) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	return b, true
}

// ToolDetail 是一次工具调用的完整入出参，供展开折叠时按需拉取。
type ToolDetail struct {
	RawInput  json.RawMessage `json:"rawInput,omitempty"`
	RawOutput json.RawMessage `json:"rawOutput,omitempty"`
}

// ToolOutput 返回一次工具调用的完整入出参。消息列表里超大字段默认
// 截成预览下发（slimMessages），用户展开工具卡时按需来取这一条——
// 打开会话保持轻，完整信息也一直可达。
func (s *ChatService) ToolOutput(sessionID uint, toolCallID string) (*ToolDetail, error) {
	all, err := s.rebuildAll(sessionID)
	if err != nil {
		return nil, err
	}
	// 同一 toolCallId 理论上只出现一次；从尾部找起，最新的先命中。
	for _, m := range slices.Backward(all) {
		if m.Kind != model.KindToolCall || m.Payload == nil {
			continue
		}
		if m.Payload["toolCallId"] != toolCallID {
			continue
		}
		detail := &ToolDetail{}
		detail.RawInput, _ = m.Payload["rawInput"].(json.RawMessage)
		detail.RawOutput, _ = m.Payload["rawOutput"].(json.RawMessage)
		return detail, nil
	}
	return nil, ErrNotFound
}

// BackfillMessageCounts 为历史会话补消息数缓存（新列上线时全是 0）。
// 启动后在后台跑一次，量级是会话数 × 单次重建的毫秒级成本。
func (s *ChatService) BackfillMessageCounts(ctx context.Context) {
	var sessions []model.Session
	if err := s.db.WithContext(ctx).Select("id", "message_count").Find(&sessions).Error; err != nil {
		slog.Warn("backfill message counts", "err", err)
		return
	}
	for _, session := range sessions {
		if session.MessageCount > 0 {
			continue
		}
		all, err := s.rebuildAll(session.ID)
		if err != nil || len(all) == 0 {
			continue
		}
		if err := s.db.WithContext(ctx).Model(&model.Session{}).
			Where("id = ?", session.ID).
			Update("message_count", len(all)).Error; err != nil {
			slog.Warn("backfill message count", "session", session.ID, "err", err)
		}
	}
}

// TranscriptPath 返回会话转录文件的路径，供原始日志下载。
func (s *ChatService) TranscriptPath(sessionID uint) string {
	return s.transcripts.Path(sessionKey(sessionID))
}

// 对话索引：把一条会话里的用户提问抽成可跳转的锚点，供界面在对话左侧
// 排成一列刻度。
//
// 数据不另存一份——消息本来就是从转录重建的，而重建结果已经带缓存常驻
// 内存（见 chat_messages.go），这里只是在那份全量上过一遍。所以索引天然
// 覆盖**整条会话**，跟界面分页加载到哪儿无关。

const (
	// digestThreshold 是「值得跑模型」的提问长度（rune）。短提问首行就是
	// 最好的索引文案，「继续」「这个再改改」精简完还是它自己，跑一轮推理
	// 纯属白等。
	digestThreshold = 60
	// outlineFallbackRunes 是回落文案的截断长度：没有摘要时索引显示提问
	// 首行的这么多字。
	outlineFallbackRunes = 60
	// outlineReplyRunes 是气泡里回答预览的长度：够两行，不够读完——它是
	// 认路用的路标，不是内容本身。
	outlineReplyRunes = 90
	// maxDigestBatch 是一次后台补齐的条数上限。老会话第一次打开可能攒了
	// 上百条长提问，一口气跑完要几分钟且把本机模型占满——分批跑，剩下的
	// 下次打开接着补。
	maxDigestBatch = 40
	// maxDigestEntries 是单条会话的摘要缓存条数上限，防止超长会话把这个
	// JSON 字段撑成几百 KB。超限后不再新增，已有的照常用。
	maxDigestEntries = 800
	// digestFlushEvery 是补齐过程中的落库间隔（条）。全跑完才写一次的话，
	// 中途退出就前功尽弃；每条都写又太吵。
	digestFlushEvery = 8
	// digestTimeout 与标题生成同理：ollama 冷启动要把模型载进内存，
	// 十几秒是常态，而这是后台任务，等得起。
	digestTimeout = 90 * time.Second
)

// OutlineEntry 是索引条上的一格：一条用户提问的锚点与文案。
type OutlineEntry struct {
	// MessageID 与消息列表里的 id 同源，界面用它滚到对应消息。
	MessageID uint      `json:"messageId"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"createdAt"`
	// Digested 说明这行文案是模型精简过的，还是提问首行的截断。界面据此
	// 决定要不要在气泡里补一句提问原文。
	Digested bool `json:"digested"`
	// Reply 是这一轮 agent 回答的开头，给索引气泡当第二行——光有提问，
	// 一列相似的刻度还是分不清哪轮是哪轮，「问了什么 + 答了什么」才定位
	// 得到。取不到（这一轮还没回答完）就是空串。
	Reply string `json:"reply,omitempty"`
}

// SessionOutline 是一条会话的完整提问索引。
type SessionOutline struct {
	Items []OutlineEntry `json:"items"`
	// Pending 是还在后台等模型精简的长提问条数。界面看到非零就知道文案
	// 之后还会变好，可以过一会儿再拉一次。
	Pending int `json:"pending"`
}

// Outline 抽出会话里全部用户提问，拼成可跳转的索引。
//
// 长提问的摘要走缓存：命中就用，没命中先回落首行并在后台补齐——索引要
// 立刻能用，不能为了好看的文案让界面干等外部模型。
func (s *ChatService) Outline(sessionID uint) (SessionOutline, error) {
	all, err := s.rebuildAll(sessionID)
	if err != nil {
		return SessionOutline{}, err
	}

	digests := s.loadDigests(sessionID)
	items := make([]OutlineEntry, 0, 16)
	// 缺摘要的长提问按原文收集，去重后交给后台——同一句话问两遍只该跑
	// 一次模型。
	var missing []string
	seen := make(map[string]bool)

	for _, m := range all {
		if m.Kind != model.KindText {
			continue
		}
		if m.Role == model.RoleAgent {
			// 上一条提问的回答开头：一轮里 agent 会说好几段，只要第一段。
			if n := len(items); n > 0 && items[n-1].Reply == "" {
				items[n-1].Reply = previewLine(m.Content, outlineReplyRunes)
			}
			continue
		}
		if m.Role != model.RoleUser {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		entry := OutlineEntry{
			MessageID: m.ID,
			Text:      outlineFallback(content),
			CreatedAt: m.CreatedAt,
		}
		if isLongPrompt(content) {
			key := PromptFingerprint(content)
			if digest := digests[key]; digest != "" {
				entry.Text = digest
				entry.Digested = true
			} else if !seen[key] {
				seen[key] = true
				missing = append(missing, content)
			}
		}
		items = append(items, entry)
	}

	if len(missing) > 0 {
		s.startDigestFill(sessionID, missing)
	}
	return SessionOutline{Items: items, Pending: len(missing)}, nil
}

// PromptFingerprint 是提问正文的内容指纹，用作摘要缓存的键。
// 取 sha256 前 8 字节：本地一条会话几百条提问，碰撞概率可以忽略，而全长
// 摘要键会让缓存字段白白大一倍。
func PromptFingerprint(content string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(content)))
	return hex.EncodeToString(sum[:8])
}

// isLongPrompt 判断这条提问值不值得跑模型精简。
func isLongPrompt(content string) bool {
	return len([]rune(content)) > digestThreshold
}

// outlineFallback 是没有摘要时的索引文案：提问的首个非空行，压掉多余
// 空白后截断。取首行而不是开头 N 个字——贴了一大段日志再提问的形态里，
// 首行往往就是那句人话。
func outlineFallback(content string) string {
	return previewLine(content, outlineFallbackRunes)
}

// previewLine 取一段文本的首个非空行，压掉多余空白后截断。
// 取首行而不是开头 N 个字——贴了一大段日志再提问的形态里，首行往往就是
// 那句人话；agent 的回答同理，第一行通常是结论。
func previewLine(text string, maxRunes int) string {
	line := text
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) != "" {
			line = l
			break
		}
	}
	line = strings.Join(strings.Fields(line), " ")
	// agent 的回答第一行常是 markdown 标题或加粗结论，符号进了气泡就是
	// 噪音——只剥行首标记，行内的代码反引号留着（那多半是标识符本身）。
	line = strings.TrimLeft(line, "#*->` \t")
	if r := []rune(line); len(r) > maxRunes {
		return string(r[:maxRunes]) + "…"
	}
	return line
}

// loadDigests 读一条会话的摘要缓存。读不到就当空——索引照常出，只是全
// 用回落文案。
func (s *ChatService) loadDigests(sessionID uint) map[string]string {
	if s.db == nil {
		return nil
	}
	var sess model.Session
	if err := s.db.Select("prompt_digests").First(&sess, sessionID).Error; err != nil {
		slog.Debug("outline: 读摘要缓存失败", "session", sessionID, "err", err)
		return nil
	}
	out := make(map[string]string, len(sess.PromptDigests))
	for k, v := range sess.PromptDigests {
		if str, ok := v.(string); ok && str != "" {
			out[k] = str
		}
	}
	return out
}

// startDigestFill 起一个后台任务补齐缺失的摘要。同一条会话同时只跑一个
// ——索引会被反复请求（打开、切面板、轮询），每次都起一批会把本机模型
// 排满，而它们要算的还是同一批东西。
func (s *ChatService) startDigestFill(sessionID uint, prompts []string) {
	if s.titler == nil || !s.titler.Enabled() {
		return
	}
	s.digestMu.Lock()
	if s.digesting[sessionID] {
		s.digestMu.Unlock()
		return
	}
	if s.digesting == nil {
		s.digesting = make(map[uint]bool)
	}
	s.digesting[sessionID] = true
	s.digestMu.Unlock()

	go func() {
		defer func() {
			s.digestMu.Lock()
			delete(s.digesting, sessionID)
			s.digestMu.Unlock()
		}()
		s.fillDigests(sessionID, prompts)
	}()
}

// fillDigests 串行跑模型补齐摘要并分批落库。
//
// 串行是刻意的：本机就一个 ollama，并发只会让每一条都变慢，而这是后台
// 任务，快慢没人等。单条失败跳过——下次打开会话再补，界面这期间用回落
// 文案，看不出异常。
func (s *ChatService) fillDigests(sessionID uint, prompts []string) {
	if len(prompts) > maxDigestBatch {
		prompts = prompts[:maxDigestBatch]
	}
	got := make(map[string]string, len(prompts))
	flush := func() {
		if len(got) == 0 {
			return
		}
		s.storeDigests(sessionID, got)
		got = make(map[string]string, digestFlushEvery)
	}

	for _, prompt := range prompts {
		ctx, cancel := context.WithTimeout(context.Background(), digestTimeout)
		digest, err := s.titler.Summarize(ctx, prompt)
		cancel()
		if err != nil {
			slog.Debug("outline: 摘要生成失败", "session", sessionID, "err", err)
			continue
		}
		got[PromptFingerprint(prompt)] = digest
		if len(got) >= digestFlushEvery {
			flush()
		}
	}
	flush()
}

// storeDigests 把新算出的摘要并进会话的缓存字段。
//
// 读-改-写而不是整体覆盖：这期间可能有别的轮刚落了自己的摘要。补齐任务
// 每条会话同时只有一个，剩下的竞争窗口窄到可以接受——真丢了一条，下次
// 打开会话补回来。
func (s *ChatService) storeDigests(sessionID uint, got map[string]string) {
	if s.db == nil {
		return
	}
	merged := s.loadDigests(sessionID)
	if merged == nil {
		merged = make(map[string]string, len(got))
	}
	for k, v := range got {
		if len(merged) >= maxDigestEntries {
			break
		}
		merged[k] = v
	}
	payload := make(model.JSONMap, len(merged))
	for k, v := range merged {
		payload[k] = v
	}
	if err := s.db.Model(&model.Session{}).Where("id = ?", sessionID).
		Update("prompt_digests", payload).Error; err != nil {
		slog.Warn("outline: 摘要落库失败", "session", sessionID, "err", err)
	}
}

// digestTurnPrompt 在轮末给这一轮的用户提问补摘要——新提问下次打开会话
// 就已经是精简过的，不必等界面请求索引时再现算。短提问与已有缓存直接跳过。
func (s *ChatService) digestTurnPrompt(sessionID uint, all []model.Message) {
	if s.titler == nil || !s.titler.Enabled() {
		return
	}
	// 末条用户提问就是刚发的那一句。
	content := ""
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Role == model.RoleUser && all[i].Kind == model.KindText {
			content = strings.TrimSpace(all[i].Content)
			break
		}
	}
	if content == "" || !isLongPrompt(content) {
		return
	}
	if s.loadDigests(sessionID)[PromptFingerprint(content)] != "" {
		return
	}
	s.startDigestFill(sessionID, []string{content})
}
