package service

import (
	"context"
	"fmt"
	"log/slog"

	"acpp/server/internal/model"
)

func (s *ChatService) rebuildAll(sessionID uint) ([]model.Message, error) {
	entries, err := s.transcripts.Read(sessionKey(sessionID))
	if err != nil {
		return nil, fmt.Errorf("read transcript: %w", err)
	}
	return RebuildMessages(sessionID, entries), nil
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
