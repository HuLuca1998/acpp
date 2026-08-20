package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"acpp/server/internal/acp"
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
	case errors.Is(err, service.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, service.ErrInvalid):
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
