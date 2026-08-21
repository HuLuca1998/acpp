package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

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
	return all, total, nil
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
