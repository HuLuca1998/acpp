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
	case errors.Is(err, acp.ErrUnsupported):
		// 该 runtime 不支持这个统一设置维度；正常前端不会发（控件按
		// Settings 隐藏），发了就是入参问题。
		status = http.StatusBadRequest
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
