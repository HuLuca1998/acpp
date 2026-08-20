// Package mcpcall 记录 MCP 工具调用并读回来：谁调的、传了什么、拿回
// 什么、花多久。
//
// 工具声明是代码，不入库；这个包只管**发生过的调用**——工具台的调用
// 记录与次数统计读它，datasource 的工具面写它（经一个只有 Record 的
// 窄接口，两个包因此不互相 import）。
package mcpcall

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"acpp/server/internal/model"
)

// 调用记录的留存与截断上限。
//
// 调用记录是**运行时观测，不是账本**：它回答的是「刚才那次调用发生了
// 什么」「这个工具 AI 到底用不用」，留最近这些就够。上限写死而不做成
// 配置项——没人会想调它，多一个旋钮只是多一处要解释的东西。
const (
	retention  = 2000
	argsLimit  = 4 << 10
	textLimit  = 8 << 10
	sweepEvery = 100
)

// Service 是调用记录的业务面。
//
// 写入发生在 MCP 请求路径上（mcp.Server.OnCall），因此这里的每个写操作
// 都必须便宜且不返回错误——观测失败不该让 AI 的工具调用跟着失败。
type Service struct {
	db *gorm.DB
	// writes 是进程内的写入计数，用来隔一段时间才裁一次旧记录：
	// 每写一条都跑一遍删除，是为一个几千行的表付连续的锁开销。
	writes atomic.Int64
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// Record 落一条调用记录。入参里的长文本在这里截断——调用方给的是原样。
func (s *Service) Record(ctx context.Context, rec model.MCPCall) {
	if s == nil || s.db == nil {
		return
	}
	rec.Args = truncateText(rec.Args, argsLimit)
	rec.Result = truncateText(rec.Result, textLimit)
	if err := s.db.WithContext(ctx).Create(&rec).Error; err != nil {
		slog.Warn("mcp call record", "tool", rec.Tool, "err", err)
		return
	}
	if s.writes.Add(1)%sweepEvery == 0 {
		s.sweep(ctx)
	}
}

// sweep 裁掉留存上限之外的旧记录。按自增 id 划线而不是按时间：id 单调，
// 一条 DELETE 就够，不用先数总量。
func (s *Service) sweep(ctx context.Context) {
	var maxID uint
	if err := s.db.WithContext(ctx).Model(&model.MCPCall{}).
		Select("COALESCE(MAX(id), 0)").Scan(&maxID).Error; err != nil {
		return
	}
	if maxID <= retention {
		return
	}
	if err := s.db.WithContext(ctx).
		Where("id <= ?", maxID-retention).
		Delete(&model.MCPCall{}).Error; err != nil {
		slog.Warn("mcp call sweep", "err", err)
	}
}

// Filter 是调用记录的筛选条件，空字段表示不筛。
type Filter struct {
	Server string
	Tool   string
	Source string
	// ErrorsOnly 只看失败的调用——排查时这是最常用的一刀。
	ErrorsOnly bool
}

// List 按时间倒序分页读调用记录。
func (s *Service) List(ctx context.Context, f Filter, page, pageSize int) ([]model.MCPCall, int64, error) {
	q := s.filtered(ctx, f)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count mcp calls: %w", err)
	}
	var rows []model.MCPCall
	if err := q.Order("id desc").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("list mcp calls: %w", err)
	}
	return rows, total, nil
}

// Stats 按工具聚合调用统计，次数降序。工具台的清单靠它标出「这个工具
// 被用过几次」——一个从没被调用过的工具，多半是描述没写对。
func (s *Service) Stats(ctx context.Context) ([]model.MCPToolStat, error) {
	// last_used_at 扫成字符串再自己解析：聚合列没有 schema 信息，GORM
	// 认不出它是时间，直接扫进 time.Time 会报 unsupported Scan。
	// 格式见 sqliteTimeLayouts。
	type row struct {
		Server     string
		Tool       string
		Count      int64
		ErrorCount int64
		AvgMs      int64
		LastUsedAt string
	}
	var rows []row
	err := s.db.WithContext(ctx).Model(&model.MCPCall{}).
		Select("server, tool, COUNT(*) AS count, " +
			"SUM(CASE WHEN is_error THEN 1 ELSE 0 END) AS error_count, " +
			"CAST(AVG(duration_ms) AS INTEGER) AS avg_ms, " +
			"MAX(created_at) AS last_used_at").
		Group("server, tool").Order("count desc").Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("stat mcp calls: %w", err)
	}

	out := make([]model.MCPToolStat, 0, len(rows))
	for _, r := range rows {
		stat := model.MCPToolStat{
			Server: r.Server, Tool: r.Tool,
			Count: r.Count, ErrorCount: r.ErrorCount, AvgMs: r.AvgMs,
		}
		// 解析不出来就留零值：少一个时间戳无非是页面上少一行「最近使用」，
		// 不值得让整份统计失败。
		stat.LastUsedAt = parseSqliteTime(r.LastUsedAt)
		out = append(out, stat)
	}
	return out, nil
}

// Clear 清空调用记录。
func (s *Service) Clear(ctx context.Context) error {
	// GORM 的全表删除要显式条件，1=1 就是那个「我确实想全删」的表态。
	if err := s.db.WithContext(ctx).Where("1 = 1").Delete(&model.MCPCall{}).Error; err != nil {
		return fmt.Errorf("clear mcp calls: %w", err)
	}
	return nil
}

func (s *Service) filtered(ctx context.Context, f Filter) *gorm.DB {
	q := s.db.WithContext(ctx).Model(&model.MCPCall{})
	if f.Server != "" {
		q = q.Where("server = ?", f.Server)
	}
	if f.Tool != "" {
		q = q.Where("tool = ?", f.Tool)
	}
	if f.Source != "" {
		q = q.Where("source = ?", f.Source)
	}
	if f.ErrorsOnly {
		q = q.Where("is_error = ?", true)
	}
	return q
}

// truncateText 按**字节**截断并标注，退到最近的字符边界，
// 免得末尾留下半个 UTF-8 字符（前端会渲染成一个乱码方块）。
func truncateText(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n…（已截断）"
}

// sqliteTimeLayouts 是 sqlite 里时间可能的两种写法。
//
// glebarez/sqlite 把 time.Time 存成空格分隔的那种（Go 的 time.String 形状），
// 但取列时 driver 会转成 RFC3339——聚合函数绕过了那层转换，拿到的是**原始
// 存储串**。两种都试是因为这层耦合在 driver 里，升级它就可能变。
var sqliteTimeLayouts = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	time.RFC3339Nano,
}

// parseSqliteTime 解析聚合列里的时间，认不出就回零值——少一个时间戳
// 无非是页面上少一行「最近使用」，不值得让整份统计失败。
func parseSqliteTime(s string) time.Time {
	for _, layout := range sqliteTimeLayouts {
		if ts, err := time.Parse(layout, s); err == nil {
			return ts
		}
	}
	return time.Time{}
}
