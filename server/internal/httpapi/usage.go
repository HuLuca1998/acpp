package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"acpp/server/internal/usage"
)

// usageHandler 是用量报表的 HTTP 面。
//
// 租户可用：读的范围由 Scope 收在查询条件里，owner 看全部身份、租户只看
// 自己的。写（重算历史）在 owner 前缀表里，租户够不着。
type usageHandler struct {
	ledger *usage.Ledger
}

// scope 把身份翻成账本自己的范围类型。usage 是叶子级业务包、反过来被
// service import，不能依赖 service.Scope，所以翻译落在这一层。
func usageScope(r *http.Request) usage.Scope {
	s := scopeOf(r)
	if s.Owner {
		return usage.OwnerScope()
	}
	return usage.TenantScope(s.TenantID)
}

// usageFilter 从查询参数解出筛选条。
//
// 时间缺省给最近 14 天：报表打开就该有东西看，而「全部」在数据涨起来
// 之后是一次全表扫。要全量的显式传 from=0。
func usageFilter(r *http.Request) usage.Filter {
	q := r.URL.Query()
	f := usage.Filter{
		Flavor:  q.Get("flavor"),
		Project: q.Get("project"),
		Origin:  q.Get("origin"),
		Model:   q.Get("model"),
	}
	if v := q.Get("session"); v != "" {
		if id, err := strconv.ParseUint(v, 10, 64); err == nil {
			f.Session = uint(id)
		}
	}
	// tenant 只有 owner 说了算；租户传了也会被 Scope 盖掉（apply 里
	// 先判 Owner），这里不用额外挡。
	if v := q.Get("tenant"); v != "" {
		if id, err := strconv.ParseUint(v, 10, 64); err == nil {
			t := uint(id)
			f.Tenant = &t
		}
	}

	f.From, f.To = timeRange(q.Get("from"), q.Get("to"))
	return f
}

// timeRange 解出 [from, to)。两端都按**本地时区**的日界对齐——报表说的
// 「今天」是人过的今天，不是 UTC 的今天。
func timeRange(from, to string) (time.Time, time.Time) {
	const day = "2006-01-02"
	loc := time.Local
	now := time.Now()
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
	start := end.AddDate(0, 0, -14)

	if from == "0" {
		// 显式要全量。
		return time.Time{}, time.Time{}
	}
	if t, err := time.ParseInLocation(day, from, loc); err == nil {
		start = t
	}
	if t, err := time.ParseInLocation(day, to, loc); err == nil {
		// to 是闭区间的那一天，查询用的是开区间，往后推一天才把那天算全。
		end = t.AddDate(0, 0, 1)
	}
	return start, end
}

func (h usageHandler) summary(w http.ResponseWriter, r *http.Request) {
	res, err := h.ledger.Summary(r.Context(), usageScope(r), usageFilter(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, res)
}

func (h usageHandler) series(w http.ResponseWriter, r *http.Request) {
	bucket := r.URL.Query().Get("bucket")
	if bucket == "" {
		bucket = usage.BucketDay
	}
	rows, err := h.ledger.Series(r.Context(), usageScope(r), usageFilter(r), bucket)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, newPage(rows))
}

func (h usageHandler) breakdown(w http.ResponseWriter, r *http.Request) {
	by := r.URL.Query().Get("by")
	if by == "" {
		by = usage.ByProject
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := h.ledger.Breakdown(r.Context(), usageScope(r), usageFilter(r), by, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, newPage(rows))
}

func (h usageHandler) errors(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	res, err := h.ledger.Errors(r.Context(), usageScope(r), usageFilter(r), limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, res)
}

// backfill 照转录重算全部历史账目。owner 专属（在前缀表里）。
func (h usageHandler) backfill(w http.ResponseWriter, r *http.Request) {
	res, err := h.ledger.Backfill(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, res)
}
