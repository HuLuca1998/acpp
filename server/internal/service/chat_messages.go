package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
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

// rawOutputLimit 是单条工具调用输出下发给界面的上限。工具卡的滚动区里
// 几十 KB 已远超可读范围，再多只是把打开会话的响应撑到几 MB——传输、
// 解析、渲染一路全卡。转录里保留完整原文，截断只发生在读路径。
const rawOutputLimit = 64 * 1024

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
	raw, ok := p["rawOutput"].(json.RawMessage)
	if !ok || len(raw) <= rawOutputLimit {
		return p, false
	}
	// 数据库查询的结果是结构化 JSON，截断会让前端解析不出结果表格。
	if ri, ok := p["rawInput"].(json.RawMessage); ok {
		var probe struct {
			SQL string `json:"sql"`
		}
		if json.Unmarshal(ri, &probe) == nil && probe.SQL != "" {
			return p, false
		}
	}
	slim, ok := truncateRawOutput(raw)
	if !ok {
		return p, false
	}
	next := make(model.JSONMap, len(p)+1)
	maps.Copy(next, p)
	next["rawOutput"] = slim
	next["rawOutputTruncated"] = true
	return next, true
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
