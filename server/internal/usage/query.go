package usage

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"acpp/server/internal/model"
)

// Scope 是查询的可见范围。
//
// 刻意不复用 service.Scope：本包是叶子级业务包，service 反过来 import 了
// 它（会话轮末落账），再依赖回去就成环。翻译在 httpapi 那一层做，一行。
type Scope struct {
	// Owner 为 true 时看全部身份的账目。
	Owner bool
	// TenantID 是租户的归属值（owner 的会话记 0）。
	TenantID uint
}

// OwnerScope 是 owner 的全量范围。
func OwnerScope() Scope { return Scope{Owner: true} }

// TenantScope 是某个租户自己的范围。
func TenantScope(tenantID uint) Scope { return Scope{TenantID: tenantID} }

// Filter 是报表筛选条：页面上那一排选择器就是它。
// 零值表示不过滤。
type Filter struct {
	From    time.Time
	To      time.Time
	Tenant  *uint
	Flavor  string
	Project string
	Origin  string
	Model   string
	Session uint
}

// apply 把范围与筛选条一起写进查询本身。
//
// **隔离的执行点就在这里**：租户条件是查询的一部分，漏写等于查不到，
// 而不是变成越权（与 service.Scope 同一条原则）。
func (f Filter) apply(q *gorm.DB, scope Scope) *gorm.DB {
	if !scope.Owner {
		q = q.Where("tenant_id = ?", scope.TenantID)
	} else if f.Tenant != nil {
		q = q.Where("tenant_id = ?", *f.Tenant)
	}
	if !f.From.IsZero() {
		q = q.Where("started_at >= ?", f.From)
	}
	if !f.To.IsZero() {
		q = q.Where("started_at < ?", f.To)
	}
	if f.Flavor != "" {
		q = q.Where("flavor = ?", f.Flavor)
	}
	if f.Project != "" {
		q = q.Where("project = ?", f.Project)
	}
	if f.Origin != "" {
		q = q.Where("origin = ?", f.Origin)
	}
	if f.Model != "" {
		q = q.Where("model = ?", f.Model)
	}
	if f.Session != 0 {
		q = q.Where("session_id = ?", f.Session)
	}
	return q
}

// Totals 是一组账目的合计，报表里每一处「一行数字」都是它。
type Totals struct {
	Turns    int64 `json:"turns"`
	Sessions int64 `json:"sessions"`

	InputTokens      int64 `json:"inputTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	CacheReadTokens  int64 `json:"cacheReadTokens"`
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
	ThoughtTokens    int64 `json:"thoughtTokens"`
	TotalTokens      int64 `json:"totalTokens"`

	// CostMicro 是**实报 + 折算**的合计，ReportedMicro / EstimatedMicro
	// 分开给出来——界面要能说清哪部分是 agent 自己算的、哪部分是我们折的。
	CostMicro      int64 `json:"costMicro"`
	ReportedMicro  int64 `json:"reportedMicro"`
	EstimatedMicro int64 `json:"estimatedMicro"`
	// UnpricedTurns 是既没实报也没折算的轮数。**不按零算**：零和
	// 「不知道」在账目上是两件事，界面要如实说有多少轮没有价。
	UnpricedTurns int64 `json:"unpricedTurns"`

	// DurationMs 是这些轮占用的总时长（agent 真正在干活的时间）。
	DurationMs int64 `json:"durationMs"`
	// 异常三层：轮次没正常收尾的、agent 报错的、工具调用的。
	AbnormalTurns int64 `json:"abnormalTurns"`
	ErrorTurns    int64 `json:"errorTurns"`
	ToolCalls     int64 `json:"toolCalls"`
	ToolFailed    int64 `json:"toolFailed"`
}

// totalsSelect 是合计的选择式。挑出来是因为三个接口（summary / series /
// breakdown）要的是**同一组数字**，只是分组不同——分开写就会慢慢长歪。
const totalsSelect = `
	count(*) as turns,
	count(distinct session_id) as sessions,
	coalesce(sum(input_tokens),0) as input_tokens,
	coalesce(sum(output_tokens),0) as output_tokens,
	coalesce(sum(cache_read_tokens),0) as cache_read_tokens,
	coalesce(sum(cache_write_tokens),0) as cache_write_tokens,
	coalesce(sum(thought_tokens),0) as thought_tokens,
	coalesce(sum(total_tokens),0) as total_tokens,
	coalesce(sum(cost_micro),0) as cost_micro,
	coalesce(sum(case when cost_source = 'reported' then cost_micro else 0 end),0) as reported_micro,
	coalesce(sum(case when cost_source = 'estimated' then cost_micro else 0 end),0) as estimated_micro,
	coalesce(sum(case when cost_source = 'none' then 1 else 0 end),0) as unpriced_turns,
	coalesce(sum(duration_ms),0) as duration_ms,
	coalesce(sum(case when stop_reason != 'end_turn' then 1 else 0 end),0) as abnormal_turns,
	coalesce(sum(case when error_code != 0 then 1 else 0 end),0) as error_turns,
	coalesce(sum(tool_calls),0) as tool_calls,
	coalesce(sum(tool_failed),0) as tool_failed`

// Summary 是报表顶部那几张卡：一组合计 + 上一个等长周期的合计。
type Summary struct {
	Totals Totals `json:"totals"`
	// Previous 是紧邻的上一个等长周期，供界面显示环比。
	// 没有指定时间范围时为 nil——「与什么比」那时没有答案。
	Previous *Totals `json:"previous,omitempty"`
}

// Summary 算一组筛选条件下的合计。
func (l *Ledger) Summary(ctx context.Context, scope Scope, f Filter) (Summary, error) {
	cur, err := l.totals(ctx, scope, f)
	if err != nil {
		return Summary{}, err
	}
	out := Summary{Totals: cur}

	// 环比只在时间范围完整时才有意义：把窗口整体往前平移一个等长周期。
	if !f.From.IsZero() && !f.To.IsZero() && f.To.After(f.From) {
		span := f.To.Sub(f.From)
		prevFilter := f
		prevFilter.To = f.From
		prevFilter.From = f.From.Add(-span)
		prev, err := l.totals(ctx, scope, prevFilter)
		if err != nil {
			return Summary{}, err
		}
		out.Previous = &prev
	}
	return out, nil
}

func (l *Ledger) totals(ctx context.Context, scope Scope, f Filter) (Totals, error) {
	var t Totals
	q := f.apply(l.db.WithContext(ctx).Model(&model.TokenUsage{}), scope)
	if err := q.Select(totalsSelect).Scan(&t).Error; err != nil {
		return Totals{}, fmt.Errorf("usage: totals: %w", err)
	}
	return t, nil
}

// Bucket 是曲线上的一格。
type Bucket struct {
	// Date 是这一格的起点，按 bucket 粒度对齐（YYYY-MM-DD 或带小时）。
	Date   string `json:"date"`
	Totals Totals `json:"totals"`
}

// 曲线的粒度。
const (
	BucketHour = "hour"
	BucketDay  = "day"
)

// Series 算按时间分格的曲线，**补齐没有数据的格子**。
//
// 补格不是锦上添花：没有活动的那天如果整格消失，两周的曲线会被压成三个
// 点，看起来像天天都在烧钱。
func (l *Ledger) Series(ctx context.Context, scope Scope, f Filter, bucket string) ([]Bucket, error) {
	if f.From.IsZero() || f.To.IsZero() {
		return nil, fmt.Errorf("%w: 曲线要有时间范围", ErrInvalid)
	}
	// 分格按**本地时区**对齐，与 SQL 侧的 strftime(...,'localtime') 一致。
	// 用 time.Date 而不是 Truncate：后者按 UTC 绝对时间切，跨时区会把
	// 一天的边界切在半夜之外；按日推进也用 AddDate，夏令时那天才不会少一格。
	layout := "2006-01-02"
	expr := "strftime('%Y-%m-%d', started_at, 'localtime')"
	align := func(t time.Time) time.Time {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	}
	next := func(t time.Time) time.Time { return t.AddDate(0, 0, 1) }
	if bucket == BucketHour {
		layout = "2006-01-02 15"
		expr = "strftime('%Y-%m-%d %H', started_at, 'localtime')"
		align = func(t time.Time) time.Time {
			return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
		}
		next = func(t time.Time) time.Time { return t.Add(time.Hour) }
	}

	type scanned struct {
		Date string
		Totals
	}
	var rows []scanned
	q := f.apply(l.db.WithContext(ctx).Model(&model.TokenUsage{}), scope)
	if err := q.Select(expr + " as date," + totalsSelect).
		Group("date").Order("date").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("usage: series: %w", err)
	}

	got := make(map[string]Totals, len(rows))
	for _, r := range rows {
		got[r.Date] = r.Totals
	}

	out := make([]Bucket, 0, 32)
	// 从范围起点按格推进，空格补零。截止用 Before 而不是 !After：
	// To 是开区间（筛选用的是 started_at < To），多补一格就会凭空多出
	// 一根永远是 0 的柱子。
	for t := align(f.From); t.Before(f.To); t = next(t) {
		key := t.Format(layout)
		out = append(out, Bucket{Date: key, Totals: got[key]})
	}
	return out, nil
}

// GroupRow 是明细表里的一行：一个维度取值 + 它的合计。
type GroupRow struct {
	// Key 是分组值；空串表示「没有」（不属于任何项目、没有模型名）。
	Key    string `json:"key"`
	Totals Totals `json:"totals"`
}

// 可分组的维度。列成常量而不是让调用方传列名——那等于把 SQL 注入口
// 开在 query 参数上。
const (
	ByProject = "project"
	ByTenant  = "tenant"
	ByFlavor  = "flavor"
	ByModel   = "model"
	ByOrigin  = "origin"
	BySession = "session"
	ByDay     = "day"
)

// groupExpr 把维度名翻成安全的 SQL 表达式。
func groupExpr(by string) (string, bool) {
	switch by {
	case ByProject:
		return "project", true
	case ByTenant:
		return "tenant_id", true
	case ByFlavor:
		return "flavor", true
	case ByModel:
		return "model", true
	case ByOrigin:
		return "origin", true
	case BySession:
		return "session_id", true
	case ByDay:
		return "strftime('%Y-%m-%d', started_at, 'localtime')", true
	}
	return "", false
}

// Breakdown 按一个维度分组。六个维度是同一条 SQL 换 group by——
// 不是六个接口。
func (l *Ledger) Breakdown(ctx context.Context, scope Scope, f Filter, by string, limit int) ([]GroupRow, error) {
	expr, ok := groupExpr(by)
	if !ok {
		return nil, fmt.Errorf("%w: 认不出的分组维度 %q", ErrInvalid, by)
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	type scanned struct {
		Key string
		Totals
	}
	var rows []scanned
	q := f.apply(l.db.WithContext(ctx).Model(&model.TokenUsage{}), scope)
	// 按成本倒序，成本相同（比如整组都没有价）时退到 token 量——
	// 否则没有实报费用的 codex 分组会以随机顺序出现。
	if err := q.Select(expr + " as key," + totalsSelect).
		Group("key").Order("cost_micro desc, total_tokens desc").
		Limit(limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("usage: breakdown by %s: %w", by, err)
	}

	out := make([]GroupRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, GroupRow{Key: r.Key, Totals: r.Totals})
	}
	return out, nil
}

// ErrorClass 是一类 agent 报错的聚合。
type ErrorClass struct {
	// Code 是 JSON-RPC 错误码——**分类按码走**，两条 runtime 共用同一个
	// 错误库，报错清一色 -32603，文案只用于展示。
	Code int `json:"code"`
	// Kind 是从文案认出来的细分（过载 / 额度 / 登录 / 未分类）。
	Kind  string `json:"kind"`
	Count int64  `json:"count"`
	// Latest 是这一类最近一条的原文与时间，供界面直接显示。
	Latest    string    `json:"latest"`
	LatestAt  time.Time `json:"latestAt"`
	SessionID uint      `json:"sessionId"`
}

// 报错的细分。认不出来的落 ErrorOther——**分不出类不等于可以不显示**。
const (
	ErrorOverloaded = "overloaded"
	ErrorQuota      = "quota"
	ErrorAuth       = "auth"
	ErrorOther      = "other"
)

// classifyError 从错误文案认出细分。码不够用（全是 -32603），只能看文案，
// 所以这里只做**展示分类**，不承担任何判断逻辑。
func classifyError(msg string) string {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "overloaded"), strings.Contains(low, "529"):
		return ErrorOverloaded
	case strings.Contains(low, "limit"), strings.Contains(low, "quota"):
		return ErrorQuota
	case strings.Contains(low, "authenticate"), strings.Contains(low, "oauth"),
		strings.Contains(low, "unauthorized"):
		return ErrorAuth
	}
	return ErrorOther
}

// ErrorEvent 是一条 agent 报错的明细。
type ErrorEvent struct {
	SessionID uint      `json:"sessionId"`
	TurnSeq   int       `json:"turnSeq"`
	At        time.Time `json:"at"`
	Code      int       `json:"code"`
	Kind      string    `json:"kind"`
	Message   string    `json:"message"`
	Flavor    string    `json:"flavor"`
	Project   string    `json:"project"`
}

// Errors 是异常面板要的两样：按类聚合 + 最近的明细。
type Errors struct {
	Classes []ErrorClass `json:"classes"`
	Recent  []ErrorEvent `json:"recent"`
}

// Errors 取 agent 报错的聚合与最近明细。
//
// 只管第二层（agent 报错）。第一层（轮次收尾）与第三层（工具调用）本来
// 就在 Totals 里，不必单独跑一趟。
func (l *Ledger) Errors(ctx context.Context, scope Scope, f Filter, limit int) (Errors, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []model.TokenUsage
	q := f.apply(l.db.WithContext(ctx).Model(&model.TokenUsage{}), scope)
	if err := q.Where("error_code != 0").
		Order("started_at desc").Limit(limit).Find(&rows).Error; err != nil {
		return Errors{}, fmt.Errorf("usage: errors: %w", err)
	}

	// 两个切片都先给空的：没有报错时 JSON 里出现的是 []，不是 null——
	// 前端少一处「可能是 null」的判断。
	out := Errors{Classes: []ErrorClass{}, Recent: make([]ErrorEvent, 0, len(rows))}
	byKind := map[string]*ErrorClass{}
	for _, r := range rows {
		kind := classifyError(r.ErrorMsg)
		out.Recent = append(out.Recent, ErrorEvent{
			SessionID: r.SessionID, TurnSeq: r.TurnSeq, At: r.StartedAt,
			Code: r.ErrorCode, Kind: kind, Message: r.ErrorMsg,
			Flavor: r.Flavor, Project: r.Project,
		})
		cls := byKind[kind]
		if cls == nil {
			// rows 按时间倒序，第一次见到某一类时，那条就是它最近的一条。
			cls = &ErrorClass{
				Code: r.ErrorCode, Kind: kind,
				Latest: r.ErrorMsg, LatestAt: r.StartedAt, SessionID: r.SessionID,
			}
			byKind[kind] = cls
		}
		cls.Count++
	}

	for _, cls := range byKind {
		out.Classes = append(out.Classes, *cls)
	}
	sort.Slice(out.Classes, func(i, j int) bool {
		return out.Classes[i].Count > out.Classes[j].Count
	})
	return out, nil
}
