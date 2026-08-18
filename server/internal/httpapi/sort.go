package httpapi

import (
	"net/http"
	"slices"
	"strings"
)

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
