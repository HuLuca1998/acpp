// Package github 是 GitHub issue 页的业务面：每个身份关注一批仓库，页面
// 汇总这些仓库里的 issue，并附上 GitHub Project 看板列与 issue 侧栏的
// Priority 字段（都只有 GraphQL 读得到）。
//
// 数据全部经本机 gh CLI 拉取（owner 的登录态），租户没有自己的凭证——
// 「分配给我」靠访客记录上的 GitHub 用户名筛。issue 本身不落库：按仓库
// 缓存在内存里，后台定时刷新；过滤 / 排序 / 分页都在内存里做，一个仓库
// 几百条 issue 对这套操作而言不算数据量。
package github

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

// Label 是 issue 标签，颜色是 GitHub 的六位 hex（不带 #）。
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Issue 是聚合后的单条 issue。Priority / Status 来自 GraphQL，读不到时为空；
// 颜色是 GitHub 的色名枚举（GRAY / BLUE / GREEN / YELLOW / ORANGE / RED /
// PINK / PURPLE），前端按名字映射。
type Issue struct {
	Repo      string    `json:"repo"`
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	State     string    `json:"state"` // OPEN / CLOSED
	Labels    []Label   `json:"labels"`
	Assignees []string  `json:"assignees"`
	Author    string    `json:"author"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Priority 是 issue 侧栏 Fields 里的优先级（Urgent / High / …）。
	Priority      string `json:"priority"`
	PriorityColor string `json:"priorityColor"`
	// Status 是所属 Project 看板的列名（待处理 / 进行中 / …）。
	Status      string `json:"status"`
	StatusColor string `json:"statusColor"`
}

// Option 是一个单选字段的选项：看板列或优先级档。顺序就是 GitHub 上声明
// 的顺序——看板列按流程排、优先级按高低排，界面直接照这个顺序画。
type Option struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Repo 是一个可关注的仓库。
type Repo struct {
	Name    string `json:"name"`
	Private bool   `json:"private"`
	Watched bool   `json:"watched"`
}

// Service 是 GitHub 页的业务面。
type Service struct {
	db    *gorm.DB
	cache *cache
	// login 是 gh 登录账号，懒取一次（并发首访只跑一次 gh）。owner 的
	// 「分配给我」按它筛。
	login     string
	loginOnce sync.Once
}

func NewService(gdb *gorm.DB) *Service {
	return &Service{db: gdb, cache: newCache()}
}

// Start 起后台刷新：启动先把所有被关注的仓库预热一遍（进程重启后第一个
// 打开页面的人不用干等二十秒），之后每 refreshEvery 拉一次。ctx 取消即停。
func (s *Service) Start(ctx context.Context) {
	go func() {
		s.cache.ensure(ctx, s.watchedRepos(ctx))
		s.refreshLoop(ctx)
	}()
}

// Watched 返回一个身份关注的仓库清单；没设置过就是空。
func (s *Service) Watched(ctx context.Context, scope service.Scope) ([]string, error) {
	// Limit(1).Find 而不是 First：没设置过是常态，不该在日志里刷「record not found」。
	var rows []model.GithubWatch
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", scope.TenantID).Limit(1).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("get github watch: %w", err)
	}
	if len(rows) == 0 || rows[0].Repos == nil {
		return []string{}, nil
	}
	return rows[0].Repos, nil
}

// repoNameRe 是 `<owner>/<repo>` 的合法形状——它会原样进 gh 的参数。
var repoNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// SetWatched 覆盖一个身份的关注清单，并立刻在后台把新加的仓库拉起来——
// 用户勾完仓库回到列表，期望的是马上看到 issue，不是等下一轮刷新；但
// 拉一个大仓库要十几秒，保存请求不该被它挂住，列表请求会等到它完成。
func (s *Service) SetWatched(ctx context.Context, scope service.Scope, repos []string) ([]string, error) {
	clean := make([]string, 0, len(repos))
	seen := map[string]bool{}
	for _, r := range repos {
		r = strings.TrimSpace(r)
		if r == "" || seen[r] {
			continue
		}
		if !repoNameRe.MatchString(r) {
			return nil, fmt.Errorf("%w: invalid repo name %q", service.ErrInvalid, r)
		}
		seen[r] = true
		clean = append(clean, r)
	}
	sort.Strings(clean)

	w := model.GithubWatch{TenantID: scope.TenantID, Repos: clean}
	err := s.db.WithContext(ctx).
		Where("tenant_id = ?", scope.TenantID).
		Assign(model.GithubWatch{Repos: clean}).
		FirstOrCreate(&w).Error
	if err != nil {
		return nil, fmt.Errorf("save github watch: %w", err)
	}
	go s.cache.ensure(context.Background(), clean)
	return clean, nil
}

// Repos 列出 gh 登录账号能看到的全部仓库（含个人名下的——issue 页与
// 克隆不同，自己的仓库正是最常关注的），并标出当前身份已关注的。
func (s *Service) Repos(ctx context.Context, scope service.Scope) ([]Repo, error) {
	all, err := listRepos(ctx)
	if err != nil {
		return nil, err
	}
	watched, err := s.Watched(ctx, scope)
	if err != nil {
		return nil, err
	}
	isWatched := map[string]bool{}
	for _, r := range watched {
		isWatched[r] = true
	}
	out := make([]Repo, 0, len(all)+len(watched))
	for _, r := range all {
		out = append(out, Repo{Name: r.name, Private: r.private, Watched: isWatched[r.name]})
		delete(isWatched, r.name)
	}
	// 关注了但清单里没有（权限被收回、仓库改名）的也列出来，不然用户
	// 连取消关注的入口都找不到。
	for name := range isWatched {
		out = append(out, Repo{Name: name, Watched: true})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Result 是一次 issue 查询的结果：一页 issue，外加画筛选器要用的词汇表
// 与缓存新鲜度。
type Result struct {
	Items    []Issue `json:"items"`
	Total    int     `json:"total"`
	Page     int     `json:"page"`
	PageSize int     `json:"pageSize"`
	// Statuses / Priorities 是关注仓库上出现过的看板列与优先级档，按
	// GitHub 声明顺序。
	Statuses   []Option `json:"statuses"`
	Priorities []Option `json:"priorities"`
	Labels     []string `json:"labels"`
	// Watched 是当前身份的关注清单：前端的仓库筛选项直接取它，不用再问一次。
	Watched []string `json:"watched"`
	// Login 是「分配给我」实际用的 GitHub 用户名；空表示这个身份没配。
	Login string `json:"login"`
	// FetchedAt 是最旧的那个仓库缓存的拉取时间；零值表示还没拉到过。
	FetchedAt *time.Time `json:"fetchedAt,omitempty"`
	// Errors 是拉取失败的仓库及原因；部分失败不拦住其余仓库的展示。
	Errors []string `json:"errors,omitempty"`
}

// Issues 按查询条件返回一页 issue。login 是当前身份的 GitHub 用户名：
// 租户传访客记录上的，owner 传空（用 gh 登录账号）。
func (s *Service) Issues(ctx context.Context, scope service.Scope, login string, q Query) (*Result, error) {
	watched, err := s.Watched(ctx, scope)
	if err != nil {
		return nil, err
	}
	if scope.Owner && login == "" {
		login = s.currentLogin(ctx)
	}
	q.Login = login

	repos := watched
	if len(q.Repos) > 0 {
		repos = intersect(watched, q.Repos)
	}
	if q.Refresh {
		s.cache.refresh(ctx, repos)
	} else {
		s.cache.ensure(ctx, repos)
	}

	var all []Issue
	var errs []string
	var oldest *time.Time
	statuses := newOptionSet()
	priorities := newOptionSet()
	labels := map[string]bool{}
	for _, repo := range repos {
		snap := s.cache.get(repo)
		if snap == nil {
			continue
		}
		if snap.err != "" {
			errs = append(errs, repo+": "+snap.err)
		}
		if !snap.fetchedAt.IsZero() && (oldest == nil || snap.fetchedAt.Before(*oldest)) {
			t := snap.fetchedAt
			oldest = &t
		}
		all = append(all, snap.issues...)
		statuses.add(snap.statuses...)
		priorities.add(snap.priorities...)
		for _, is := range snap.issues {
			for _, l := range is.Labels {
				labels[l.Name] = true
			}
		}
	}

	items, total := q.apply(all, priorities.rank())
	labelList := make([]string, 0, len(labels))
	for l := range labels {
		labelList = append(labelList, l)
	}
	sort.Strings(labelList)

	return &Result{
		Items:      items,
		Total:      total,
		Page:       q.Page,
		PageSize:   q.PageSize,
		Statuses:   statuses.list(),
		Priorities: priorities.list(),
		Labels:     labelList,
		Watched:    watched,
		Login:      login,
		FetchedAt:  oldest,
		Errors:     errs,
	}, nil
}

// currentLogin 懒取 gh 登录名；拿不到就返回空（「分配给我」退化为不过滤）。
func (s *Service) currentLogin(ctx context.Context) string {
	s.loginOnce.Do(func() { s.login = ghLogin(ctx) })
	return s.login
}

func intersect(a, b []string) []string {
	in := map[string]bool{}
	for _, x := range b {
		in[x] = true
	}
	out := make([]string, 0, len(a))
	for _, x := range a {
		if in[x] {
			out = append(out, x)
		}
	}
	return out
}

// optionSet 按首次出现的顺序去重收集选项：多个仓库挂在同一块看板上时
// 列名相同，顺序取第一次见到的那份。
type optionSet struct {
	order []Option
	seen  map[string]bool
}

func newOptionSet() *optionSet { return &optionSet{seen: map[string]bool{}} }

func (o *optionSet) add(opts ...Option) {
	for _, opt := range opts {
		if opt.Name == "" || o.seen[opt.Name] {
			continue
		}
		o.seen[opt.Name] = true
		o.order = append(o.order, opt)
	}
}

func (o *optionSet) list() []Option {
	if o.order == nil {
		return []Option{}
	}
	return o.order
}

// rank 把选项名映射成位次（0 最靠前），排序用。
func (o *optionSet) rank() map[string]int {
	r := make(map[string]int, len(o.order))
	for i, opt := range o.order {
		r[opt.Name] = i
	}
	return r
}
