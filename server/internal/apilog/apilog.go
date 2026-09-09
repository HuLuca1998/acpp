// Package apilog 记录 HTTP API 请求并读回来：方法、路径、身份、请求头与正文、
// 响应状态与正文、耗时。
//
// 写入发生在每个 API 请求的路径上（httpapi 的中间件），所以 Record 必须便宜
// 且不返回错误——观测失败不该让业务请求跟着失败。读回来的是日志页。
package apilog

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"gorm.io/gorm"

	"acpp/server/internal/db"
	"acpp/server/internal/model"
)

// 留存与截断上限。写死不做配置项：没人会想调它，多一个旋钮只是多一处要解释的东西。
const (
	retention  = 5000
	BodyLimit  = 8 << 10
	sweepEvery = 100
)

// Service 是请求记录的业务面。
type Service struct {
	db     *gorm.DB
	writes atomic.Int64
}

func NewService(gdb *gorm.DB) *Service {
	return &Service{db: gdb}
}

// Record 落一条记录。正文在这里截断，头在这里抹凭证——调用方给的是原样。
func (s *Service) Record(ctx context.Context, rec model.APILog) {
	if s == nil || s.db == nil {
		return
	}
	rec.RequestBody = truncateText(rec.RequestBody, BodyLimit)
	rec.ResponseBody = truncateText(rec.ResponseBody, BodyLimit)
	if err := s.db.WithContext(ctx).Create(&rec).Error; err != nil {
		slog.Warn("api log record", "path", rec.Path, "err", err)
		return
	}
	if s.writes.Add(1)%sweepEvery == 0 {
		s.sweep(ctx)
	}
}

// sweep 裁掉留存上限之外的旧记录。按自增 id 划线：id 单调，一条 DELETE 就够。
func (s *Service) sweep(ctx context.Context) {
	var maxID uint
	if err := s.db.WithContext(ctx).Model(&model.APILog{}).
		Select("COALESCE(MAX(id), 0)").Scan(&maxID).Error; err != nil {
		return
	}
	if maxID <= retention {
		return
	}
	if err := s.db.WithContext(ctx).
		Where("id <= ?", maxID-retention).
		Delete(&model.APILog{}).Error; err != nil {
		slog.Warn("api log sweep", "err", err)
	}
}

// Filter 是日志列表的筛选条件，零值不过滤。
type Filter struct {
	// Keyword 对路径做子串匹配。
	Keyword string
	Method  string
	// StatusClass 是状态码的百位："2" 只要 2xx，以此类推。
	StatusClass string
	Identity    string
}

// List 分页读记录；orderBy 由调用方经白名单拼好，空串按 id 倒序。
func (s *Service) List(ctx context.Context, f Filter, page, pageSize int, orderBy string) ([]model.APILog, int64, error) {
	q := s.filtered(ctx, f)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count api logs: %w", err)
	}
	if orderBy == "" {
		orderBy = "id desc"
	}
	// 列表不带正文：一页 20 条各带 16KB 正文是白搭的传输，详情单独取。
	var rows []model.APILog
	if err := q.Omit("request_body", "response_body", "request_headers", "response_headers").
		Order(orderBy).
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("list api logs: %w", err)
	}
	return rows, total, nil
}

// Get 取一条完整记录（含头与正文）。
func (s *Service) Get(ctx context.Context, id uint) (*model.APILog, error) {
	var rec model.APILog
	if err := s.db.WithContext(ctx).First(&rec, id).Error; err != nil {
		return nil, fmt.Errorf("get api log %d: %w", id, err)
	}
	return &rec, nil
}

// Clear 清空记录。
func (s *Service) Clear(ctx context.Context) error {
	if err := s.db.WithContext(ctx).Where("1 = 1").Delete(&model.APILog{}).Error; err != nil {
		return fmt.Errorf("clear api logs: %w", err)
	}
	return nil
}

func (s *Service) filtered(ctx context.Context, f Filter) *gorm.DB {
	q := s.db.WithContext(ctx).Model(&model.APILog{})
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		q = q.Where("path LIKE ? ESCAPE '\\'", db.LikePattern(kw))
	}
	if f.Method != "" {
		q = q.Where("method = ?", strings.ToUpper(f.Method))
	}
	if len(f.StatusClass) == 1 && f.StatusClass[0] >= '1' && f.StatusClass[0] <= '5' {
		lo := int(f.StatusClass[0]-'0') * 100
		q = q.Where("status >= ? AND status < ?", lo, lo+100)
	}
	if f.Identity != "" {
		q = q.Where("identity = ?", f.Identity)
	}
	return q
}

// redactedHeaders 是落库前要抹掉值的头：凭证与 cookie。记录里留下键名，
// 让人知道「这个请求带了凭证」，但值永远不进库。
var redactedHeaders = map[string]bool{
	"Authorization":       true,
	"Cookie":              true,
	"Set-Cookie":          true,
	"Proxy-Authorization": true,
	"X-Api-Key":           true,
}

// HeadersJSON 把 http.Header 压成一层 JSON 对象文本，凭证抹成 [redacted]。
// 多值头用逗号并起来——日志页要的是一眼看全，不是还原线级形状。
func HeadersJSON(h http.Header) string {
	out := make(map[string]string, len(h))
	for k, vs := range h {
		key := http.CanonicalHeaderKey(k)
		if redactedHeaders[key] {
			out[key] = "[redacted]"
			continue
		}
		out[key] = strings.Join(vs, ", ")
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

// IsTextual 判断这种 Content-Type 的正文值不值得存：JSON、文本、表单存，
// 图片、压缩包这类二进制只记大小。
func IsTextual(contentType string) bool {
	ct := strings.ToLower(contentType)
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	ct = strings.TrimSpace(ct)
	switch {
	case ct == "":
		return false
	case strings.HasPrefix(ct, "text/"),
		strings.HasSuffix(ct, "/json"), strings.HasSuffix(ct, "+json"),
		strings.HasSuffix(ct, "/xml"), strings.HasSuffix(ct, "+xml"),
		ct == "application/x-www-form-urlencoded", ct == "application/javascript":
		return true
	}
	return false
}

// truncateText 按字节截断并标注，退到最近的字符边界，免得末尾留半个 UTF-8 字符。
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
