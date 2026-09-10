package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"acpp/server/internal/acp"
	"acpp/server/internal/ask"
	"acpp/server/internal/discord"
	"acpp/server/internal/service"
)

// envelope 是所有响应的统一外壳，前端固定读 data / error 两个字段。
type envelope struct {
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// page 是列表接口的返回结构。
type page[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}

func newPage[T any](items []T) page[T] {
	if items == nil {
		items = []T{}
	}
	return page[T]{
		Items:    items,
		Total:    int64(len(items)),
		Page:     1,
		PageSize: len(items),
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("write response", "err", err)
	}
}

func writeData(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, envelope{Data: data})
}

// writeError 把 service 层的哨兵错误翻译成对应的状态码。
func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	// discord 包刻意零依赖（adr-016），哨兵自带一套，这里做同义映射。
	case errors.Is(err, service.ErrNotFound), errors.Is(err, discord.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, service.ErrInvalid), errors.Is(err, discord.ErrInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, service.ErrUnauthorized):
		// 没有有效身份：前端据此跳到「需要邀请链接」页面（adr-007）。
		status = http.StatusUnauthorized
	case errors.Is(err, service.ErrForbidden):
		status = http.StatusForbidden
	case errors.Is(err, acp.ErrUnsupported):
		// 该 runtime 不支持这个统一设置维度；正常前端不会发（控件按
		// Settings 隐藏），发了就是入参问题。
		status = http.StatusBadRequest
	case errors.Is(err, ask.ErrTimeout):
		// 对方在期限内没答完：问题在被问的 agent 太慢，不是这个服务坏了。
		status = http.StatusGatewayTimeout
	case errors.Is(err, acp.ErrBusy):
		// 这条会话上一轮还没完。同步问答面（/api/ask）不排队，直接告诉
		// 调用方等一等再来。
		status = http.StatusConflict
	case errors.Is(err, acp.ErrAuthRequired):
		// agent 侧未登录（-32000）。424：问题出在我们依赖的外部进程，
		// 不是请求本身；也与租户认证的 401/403 严格区分。
		status = http.StatusFailedDependency
	default:
		slog.Error("request failed", "err", err)
	}
	writeJSON(w, status, envelope{Error: err.Error()})
}

// queryInt 解析正整数查询参数，缺失或非法时用默认值。
func queryInt(r *http.Request, key string, fallback int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

// queryBool 解析三态布尔查询参数：缺失或空为 nil（不过滤），"1"/"true" 为真，
// "0"/"false" 为假；别的写法当没给。
func queryBool(r *http.Request, key string) *bool {
	switch strings.ToLower(r.URL.Query().Get(key)) {
	case "1", "true":
		v := true
		return &v
	case "0", "false":
		v := false
		return &v
	}
	return nil
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.Join(service.ErrInvalid, err)
	}
	return nil
}

// pageParams 解析统一的分页查询参数（`?page=&pageSize=`）。
//
// 默认与上限收在这里而不是各 handler 里：跨端契约（AGENTS.md §2）说列表
// 一律包 `{items,total,page,pageSize}`，那入参也该是同一套，不然前端得记
// 「这个接口默认 20、那个默认 50」。
func pageParams(r *http.Request) (page, pageSize int) {
	page = queryInt(r, "page", 1)
	pageSize = queryInt(r, "pageSize", defaultPageSize)
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize
}

const (
	// defaultPageSize 与前端分页条的默认每页行数一致。
	defaultPageSize = 20
	maxPageSize     = 200
)

// slicePage 从整份切片里取一页。给「事实源不是数据库」的列表用
// （技能扫的是磁盘），SQL 那边照旧用 LIMIT/OFFSET。
func slicePage[T any](items []T, page, pageSize int) []T {
	start := (page - 1) * pageSize
	if start >= len(items) {
		return []T{}
	}
	end := min(start+pageSize, len(items))
	return items[start:end]
}

// 列表排序：`?sort=<字段>&order=asc|desc`。
//
// 排序必须在服务端做——客户端排序在分页列表上是错的：它只会把当前这一页
// 的 20 条重排一遍，用户以为看到的是「全部里最大的」，其实是「这 20 条里
// 最大的」。那种错比没有排序更坏。
//
// 字段名走白名单再拼进 SQL：它最终要进 ORDER BY，那是不能用占位符的位置。

// SortSpec 是一次排序请求，已经校验过字段名。
type SortSpec struct {
	// Column 是数据库列名（白名单里的原样），空表示用调用方的默认排序。
	Column string
	Desc   bool
}

// OrderBy 拼成可以直接进 GORM Order 的字符串；未指定排序时返回 fallback。
func (s SortSpec) OrderBy(fallback string) string {
	if s.Column == "" {
		return fallback
	}
	if s.Desc {
		return s.Column + " desc"
	}
	return s.Column + " asc"
}

// sortParams 解析排序参数。allowed 是**数据库列名**白名单——前端传的字段名
// 与它对不上就当没排序，不报错：一个拼错的排序参数不该让整页打不开。
func sortParams(r *http.Request, allowed ...string) SortSpec {
	column := strings.TrimSpace(r.URL.Query().Get("sort"))
	if column == "" || !slices.Contains(allowed, column) {
		return SortSpec{}
	}
	return SortSpec{
		Column: column,
		Desc:   strings.EqualFold(r.URL.Query().Get("order"), "desc"),
	}
}
