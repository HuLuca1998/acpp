package github

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"acpp/server/internal/ghcli"
)

// refreshEvery 是后台刷新的间隔。issue 不是秒级变化的东西，三分钟一轮
// 对 GitHub API 配额也友好（每仓库两次请求：列表 + GraphQL）。
const refreshEvery = 3 * time.Minute

// issueLimit 是单仓库拉取的上限。关注的是「我的 issue」这种量级，几百条
// 封顶；超过的仓库早该拆了。
const issueLimit = 300

// gqlBatch 是一次 GraphQL 里查多少条 issue 的字段：alias 拼多了查询本身
// 会超长，GitHub 也会按节点数限流。
const gqlBatch = 50

// snapshot 是一个仓库的缓存：一次拉取的全部 issue 与它们的看板 / 优先级
// 词汇表。err 记最近一次失败原因，失败不清空上一次的数据。
type snapshot struct {
	issues     []Issue
	statuses   []Option
	priorities []Option
	fetchedAt  time.Time
	err        string
}

// cache 按仓库存 snapshot，并串行化对同一仓库的拉取。
type cache struct {
	mu    sync.Mutex
	repos map[string]*snapshot
	// inflight 防止同一仓库被并发拉两遍（页面刷新 + 后台轮到）。
	inflight map[string]chan struct{}
}

func newCache() *cache {
	return &cache{repos: map[string]*snapshot{}, inflight: map[string]chan struct{}{}}
}

func (c *cache) get(repo string) *snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.repos[repo]
}

// ensure 保证这些仓库都有缓存：没有的同步拉一次（首次打开页面要等一下，
// 但等到的是真数据），有的不动。
func (c *cache) ensure(ctx context.Context, repos []string) {
	var missing []string
	c.mu.Lock()
	for _, r := range repos {
		if _, ok := c.repos[r]; !ok {
			missing = append(missing, r)
		}
	}
	c.mu.Unlock()
	if len(missing) > 0 {
		c.refresh(ctx, missing)
	}
}

// refresh 同步刷新这些仓库，仓库之间并行。
func (c *cache) refresh(ctx context.Context, repos []string) {
	var wg sync.WaitGroup
	for _, r := range repos {
		wg.Add(1)
		go func(repo string) {
			defer wg.Done()
			c.fetchOne(ctx, repo)
		}(r)
	}
	wg.Wait()
}

// fetchOne 拉一个仓库；同仓库并发到达的调用等第一个完成即可。
func (c *cache) fetchOne(ctx context.Context, repo string) {
	c.mu.Lock()
	if done, ok := c.inflight[repo]; ok {
		c.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
		}
		return
	}
	done := make(chan struct{})
	c.inflight[repo] = done
	c.mu.Unlock()

	snap, err := fetchRepo(ctx, repo)

	c.mu.Lock()
	prev := c.repos[repo]
	if err != nil {
		if prev == nil {
			prev = &snapshot{}
		}
		prev.err = err.Error()
		c.repos[repo] = prev
		slog.Warn("github fetch", "repo", repo, "err", err)
	} else {
		c.repos[repo] = snap
	}
	delete(c.inflight, repo)
	close(done)
	c.mu.Unlock()
}

// watchedRepos 取所有身份关注的仓库并集：后台刷新的对象。
func (s *Service) watchedRepos(ctx context.Context) []string {
	var rows []struct{ Repos string }
	if err := s.db.WithContext(ctx).Table("github_watches").Select("repos").Scan(&rows).Error; err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, row := range rows {
		var repos []string
		if json.Unmarshal([]byte(row.Repos), &repos) != nil {
			continue
		}
		for _, r := range repos {
			if !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
	}
	return out
}

func (s *Service) refreshLoop(ctx context.Context) {
	ticker := time.NewTicker(refreshEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cache.refresh(ctx, s.watchedRepos(ctx))
		}
	}
}

// fetchRepo 拉一个仓库：`gh issue list` 拿列表，再用 GraphQL 补每条的
// Priority 与看板 Status。GraphQL 失败不算整体失败——列表本身还是好的，
// 只是少了两列。
func fetchRepo(ctx context.Context, repo string) (*snapshot, error) {
	raw, err := ghcli.Run(ctx, "issue", "list", "--repo", repo, "--state", "all",
		"--limit", fmt.Sprint(issueLimit),
		"--json", "number,title,url,state,labels,assignees,author,updatedAt")
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Number    int       `json:"number"`
		Title     string    `json:"title"`
		URL       string    `json:"url"`
		State     string    `json:"state"`
		Labels    []Label   `json:"labels"`
		UpdatedAt time.Time `json:"updatedAt"`
		Assignees []struct {
			Login string `json:"login"`
		} `json:"assignees"`
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("parse gh issue list: %w", err)
	}

	snap := &snapshot{fetchedAt: time.Now(), issues: make([]Issue, 0, len(rows))}
	numbers := make([]int, 0, len(rows))
	for _, r := range rows {
		is := Issue{
			Repo: repo, Number: r.Number, Title: r.Title, URL: r.URL, State: r.State,
			Labels: r.Labels, Author: r.Author.Login, UpdatedAt: r.UpdatedAt,
			Assignees: make([]string, 0, len(r.Assignees)),
		}
		if is.Labels == nil {
			is.Labels = []Label{}
		}
		for _, a := range r.Assignees {
			is.Assignees = append(is.Assignees, a.Login)
		}
		snap.issues = append(snap.issues, is)
		numbers = append(numbers, r.Number)
	}

	fields, err := fetchFields(ctx, repo, numbers)
	if err != nil {
		snap.err = "fields: " + err.Error()
		return snap, nil
	}
	snap.statuses = fields.statuses
	snap.priorities = fields.priorities
	for i := range snap.issues {
		if m, ok := fields.byNumber[snap.issues[i].Number]; ok {
			snap.issues[i].Priority, snap.issues[i].PriorityColor = m.priority, m.priorityColor
			snap.issues[i].Status, snap.issues[i].StatusColor = m.status, m.statusColor
		}
	}
	return snap, nil
}

// issueMeta 是一条 issue 在 GitHub 上的优先级与看板列。
type issueMeta struct {
	priority, priorityColor string
	status, statusColor     string
}

// repoFields 是一个仓库的字段词汇表与每条 issue 的取值。
type repoFields struct {
	statuses   []Option
	priorities []Option
	byNumber   map[int]issueMeta
}

const (
	priorityField = "Priority"
	statusField   = "Status"
)

// fetchFields 批量读指定 issue 的 Priority 与看板 Status。两者都只有
// GraphQL 读得到：Priority 在 issue 侧栏的 Fields 里，Status 在 issue
// 所属 Project 的字段里——从 issue 这头反查，不需要用户配置看板编号。
// 看板列的全集与顺序取自 Project 的 Status 字段定义，优先级档取自仓库
// 的 issueFields 定义。
func fetchFields(ctx context.Context, repo string, numbers []int) (*repoFields, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok {
		return nil, fmt.Errorf("bad repo name %q", repo)
	}
	out := &repoFields{byNumber: map[int]issueMeta{}}
	for off := 0; off < len(numbers); off += gqlBatch {
		batch := numbers[off:min(off+gqlBatch, len(numbers))]
		raw, err := ghcli.Run(ctx, "api", "graphql", "-f", "query="+fieldsQuery(owner, name, batch, off == 0))
		if err != nil {
			return nil, err
		}
		if err := parseFields(raw, batch, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// fieldsQuery 拼一批 issue 的 GraphQL：仓库级的优先级字段定义只在第一批取。
func fieldsQuery(owner, name string, numbers []int, withDefs bool) string {
	var q strings.Builder
	fmt.Fprintf(&q, "query { repository(owner: %q, name: %q) {\n", owner, name)
	if withDefs {
		q.WriteString("  issueFields(first: 20) { nodes { " +
			"... on IssueFieldCommon { name } " +
			"... on IssueFieldSingleSelect { options { name color } } } }\n")
	}
	for i, n := range numbers {
		fmt.Fprintf(&q, "  i%d: issue(number: %d) {\n"+
			"    issueFieldValues(first: 10) { nodes { "+
			"... on IssueFieldSingleSelectValue { name color "+
			"field { ... on IssueFieldCommon { name } } } } }\n"+
			"    projectItems(first: 3) { nodes { "+
			"project { number field(name: %q) { ... on ProjectV2SingleSelectField { options { name color } } } } "+
			"fieldValues(first: 20) { nodes { "+
			"... on ProjectV2ItemFieldSingleSelectValue { name color "+
			"field { ... on ProjectV2FieldCommon { name } } } } } } }\n"+
			"  }\n", i, n, statusField)
	}
	q.WriteString("} }")
	return q.String()
}

// selectValue 是单选字段值的公共形状（issue Fields 与 Project 字段同形）。
type selectValue struct {
	Name  string `json:"name"`
	Color string `json:"color"`
	Field struct {
		Name string `json:"name"`
	} `json:"field"`
}

// parseFields 把一批 GraphQL 回复合进 out。
func parseFields(raw []byte, numbers []int, out *repoFields) error {
	var resp struct {
		Data struct {
			Repository map[string]json.RawMessage `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("parse graphql: %w", err)
	}
	if resp.Data.Repository == nil {
		if len(resp.Errors) > 0 {
			return fmt.Errorf("graphql: %s", resp.Errors[0].Message)
		}
		return fmt.Errorf("graphql: empty repository")
	}
	if defs, ok := resp.Data.Repository["issueFields"]; ok {
		var fields struct {
			Nodes []struct {
				Name    string   `json:"name"`
				Options []Option `json:"options"`
			} `json:"nodes"`
		}
		if json.Unmarshal(defs, &fields) == nil {
			for _, f := range fields.Nodes {
				if f.Name == priorityField {
					out.priorities = f.Options
				}
			}
		}
	}
	for i, n := range numbers {
		node, ok := resp.Data.Repository[fmt.Sprintf("i%d", i)]
		if !ok {
			continue
		}
		var item struct {
			Values struct {
				Nodes []selectValue `json:"nodes"`
			} `json:"issueFieldValues"`
			ProjectItems struct {
				Nodes []struct {
					Project struct {
						Field struct {
							Options []Option `json:"options"`
						} `json:"field"`
					} `json:"project"`
					FieldValues struct {
						Nodes []selectValue `json:"nodes"`
					} `json:"fieldValues"`
				} `json:"nodes"`
			} `json:"projectItems"`
		}
		if json.Unmarshal(node, &item) != nil {
			continue
		}
		m := issueMeta{}
		for _, v := range item.Values.Nodes {
			if v.Field.Name == priorityField && v.Name != "" {
				m.priority, m.priorityColor = v.Name, v.Color
			}
		}
		// 一个 issue 可能挂在多个看板上，取第一个给出 Status 的；
		// 看板列定义也取自它。
		for _, pi := range item.ProjectItems.Nodes {
			for _, v := range pi.FieldValues.Nodes {
				if v.Field.Name == statusField && v.Name != "" && m.status == "" {
					m.status, m.statusColor = v.Name, v.Color
					if out.statuses == nil && len(pi.Project.Field.Options) > 0 {
						out.statuses = pi.Project.Field.Options
					}
				}
			}
		}
		out.byNumber[n] = m
	}
	return nil
}

// ghRepo 是仓库清单里的一项。
type ghRepo struct {
	name    string
	private bool
}

// listRepos 列出登录账号能看到的全部仓库（个人 + 组织 + 协作）。
func listRepos(ctx context.Context) ([]ghRepo, error) {
	raw, err := ghcli.Run(ctx, "api", "-H", "Accept: application/vnd.github+json", "--paginate",
		"/user/repos?affiliation=owner,organization_member,collaborator&sort=updated&per_page=100")
	if err != nil {
		return nil, err
	}
	// --paginate 把多页 JSON 数组直接连着输出，逐个 decode。
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	var out []ghRepo
	for dec.More() {
		var page []struct {
			FullName string `json:"full_name"`
			Private  bool   `json:"private"`
		}
		if err := dec.Decode(&page); err != nil {
			return nil, fmt.Errorf("parse gh repo list: %w", err)
		}
		for _, r := range page {
			out = append(out, ghRepo{name: r.FullName, private: r.Private})
		}
	}
	return out, nil
}

// ghLogin 取 gh 登录名；拿不到返回空。
func ghLogin(ctx context.Context) string {
	raw, err := ghcli.Run(ctx, "api", "user", "--jq", ".login")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}
