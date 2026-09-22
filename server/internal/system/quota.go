package system

import (
	"context"
	"fmt"
	"sync"
	"time"

	"acpp/server/internal/service"
)

// 套餐用量（plan quota）：本机登录的 claude / codex 账号在**订阅套餐**上的
// 限额水位——5 小时窗用了几成、本周用了几成、什么时候重置。它与 usage 包
// 的账本是两件事：账本记的是这台机器花了多少 token，水位是账号在服务端
// 还剩多少额度，后者只有服务端知道，本地怎么算都算不出来。
//
// 两家的取数路径不同，取舍见 docs/adr-025：
//   - claude 借 CLI 自己的 /usage 数据面（quota_claude.go），令牌过期由
//     CLI 按官方路径刷新，本项目不碰钥匙串；
//   - codex 直接打 ChatGPT 后台的 usage 接口（quota_codex.go），凭证用
//     codex 自己的 auth.json。

// quotaTTL 是水位的缓存时长。面板一点开就要，但限额一分钟内不会有质变，
// 而 claude 那条路每次要拉一个 CLI 进程（实测 2.5s），不该被连点打爆。
const quotaTTL = time.Minute

// quotaTimeout 是单次取数的上限：CLI 拉起 + 一次远端请求，正常几秒内完。
const quotaTimeout = 30 * time.Second

// PlanQuota 是一个 agent 方言的套餐水位快照。
type PlanQuota struct {
	Flavor string `json:"flavor"`
	// Status 说明这份数据能不能用：ok 有数据；expired 是本地令牌过期
	//（跑一次对应 CLI 就续上）；not_logged_in 是没登录；unavailable 是
	// 这种登录方式没有套餐额度（API key、第三方 provider）；error 是取数
	// 失败，Error 带原因。非 ok 时 Windows 为空。
	Status string `json:"status"`
	// Plan 是套餐名（claude 的 subscription_type、codex 的 plan_type），原样透传。
	Plan string `json:"plan,omitempty"`
	// Windows 是各限额窗口，按服务端给的顺序；永远是数组（空时 []）。
	Windows []QuotaWindow `json:"windows"`
	// Credits 是 codex 的额度余额（套餐窗口之外按量计费的那部分），没有就不出现。
	Credits *QuotaCredits `json:"credits,omitempty"`
	// Extra 是 claude 的额外用量（套餐之外的付费额度），没开就不出现。
	Extra *QuotaExtra `json:"extra,omitempty"`
	// Provider 非空表示 acpp 里的 codex 走的是第三方 provider——那它消耗
	// 的就不是这份额度，界面要说明白，免得用户对着 100% 纳闷为什么还能发。
	Provider  string    `json:"provider,omitempty"`
	FetchedAt time.Time `json:"fetchedAt"`
	Error     string    `json:"error,omitempty"`
}

// QuotaWindow 是一个限额窗口。
type QuotaWindow struct {
	// Kind 是窗口种类：session（5 小时滚动窗）、weekly（每周全模型）、
	// weekly_model（每周按模型，Model 给型号名）；认不出的窗口给 window，
	// 由 WindowSeconds 说明长度。
	Kind  string `json:"kind"`
	Model string `json:"model,omitempty"`
	// WindowSeconds 是窗口长度（codex 给秒数；claude 不给，按 Kind 推）。
	WindowSeconds int `json:"windowSeconds,omitempty"`
	// Percent 是已用比例 0–100。
	Percent float64 `json:"percent"`
	// ResetsAt 是这个窗口下次归零的时刻；服务端没给就没有。
	ResetsAt *time.Time `json:"resetsAt,omitempty"`
	// Active 为真表示这是当前真正卡着的那个窗口（claude 的 is_active）。
	Active bool `json:"active,omitempty"`
}

// QuotaCredits 是 codex 的额度余额。Balance 是服务端给的字符串，不换算。
type QuotaCredits struct {
	Balance   string `json:"balance"`
	Unlimited bool   `json:"unlimited"`
}

// QuotaExtra 是 claude 的额外用量：这个月付费额度用了几成。
type QuotaExtra struct {
	Percent  float64 `json:"percent"`
	Used     float64 `json:"used"`
	Limit    float64 `json:"limit"`
	Currency string  `json:"currency,omitempty"`
}

// quotaCache 按方言缓存水位。两家各自一把锁：同一方言的并发请求只跑一次
// 取数（同时点开两个面板不该起两个 claude 进程），两家之间互不阻塞。
type quotaCache struct {
	mu      sync.Mutex
	entries map[string]*quotaEntry
	// claudeRun 可注入以便测试：默认真拉 CLI，测试给一段现成的 /usage 载荷。
	claudeRun func(ctx context.Context) ([]byte, error)
	// codexAPI 可注入以便测试，默认 chatgpt.com 的 usage 接口。
	codexAPI string
}

type quotaEntry struct {
	mu  sync.Mutex
	at  time.Time
	val PlanQuota
}

func (c *quotaCache) entry(flavor string) *quotaEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]*quotaEntry{}
	}
	e, ok := c.entries[flavor]
	if !ok {
		e = &quotaEntry{}
		c.entries[flavor] = e
	}
	return e
}

// PlanQuota 返回一个方言的套餐水位，一分钟内复用缓存；refresh 为真时绕过。
//
// 取数用的 context 刻意与请求脱钩：面板刚点开就关上，请求被取消，但那
// 2.5 秒的 CLI 已经在跑了——让它跑完把结果缓存下来，下次点开就是现成的。
func (s *Service) PlanQuota(ctx context.Context, flavor string, refresh bool) (PlanQuota, error) {
	switch flavor {
	case "claude", "codex":
	default:
		return PlanQuota{}, fmt.Errorf("%w: flavor 只认 claude / codex", service.ErrInvalid)
	}
	entry := s.quota.entry(flavor)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if !refresh && !entry.at.IsZero() && time.Since(entry.at) < quotaTTL {
		return entry.val, nil
	}

	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), quotaTimeout)
	defer cancel()
	var q PlanQuota
	if flavor == "claude" {
		q = s.fetchClaudeQuota(fetchCtx)
	} else {
		q = s.fetchCodexQuota(fetchCtx)
	}
	q.Flavor = flavor
	q.FetchedAt = time.Now()
	if q.Windows == nil {
		q.Windows = []QuotaWindow{}
	}
	entry.val, entry.at = q, q.FetchedAt
	return q, nil
}

// windowKind 按窗口长度归类：5 小时上下的是滚动会话窗，7 天上下的是周窗，
// 其余原样给 window 让界面按秒数说。codex 只给秒数，claude 只给名字，
// 归到同一套 Kind 界面才能用一套文案。
func windowKind(seconds int) string {
	switch {
	case seconds > 0 && seconds <= 6*3600:
		return "session"
	case seconds >= 6*24*3600 && seconds <= 8*24*3600:
		return "weekly"
	default:
		return "window"
	}
}
