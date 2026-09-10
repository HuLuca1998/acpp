package github

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

func newService(t *testing.T) *Service {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "gh.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.GithubWatch{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewService(gdb)
}

// 契约：关注清单按身份各存各的，owner（TenantID 0）与租户互不串；重复与
// 空白去掉、按名字排序；非法仓库名整批拒绝。
func TestWatched_PerIdentity(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	owner := service.OwnerScope()
	tenant := service.TenantScope(7, t.TempDir())

	got, err := s.Watched(ctx, owner)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty watch: %v %v", got, err)
	}

	// 预先塞进缓存，SetWatched 不会真去跑 gh。
	s.cache.repos["b/y"] = &snapshot{}
	s.cache.repos["a/x"] = &snapshot{}
	saved, err := s.SetWatched(ctx, owner, []string{"b/y", " a/x ", "b/y", ""})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if len(saved) != 2 || saved[0] != "a/x" || saved[1] != "b/y" {
		t.Fatalf("saved = %v", saved)
	}
	if got, _ := s.Watched(ctx, tenant); len(got) != 0 {
		t.Fatalf("tenant should not see owner's watch: %v", got)
	}
	s.cache.repos["c/z"] = &snapshot{}
	if _, err := s.SetWatched(ctx, tenant, []string{"c/z"}); err != nil {
		t.Fatalf("set tenant: %v", err)
	}
	if got, _ := s.Watched(ctx, owner); len(got) != 2 {
		t.Fatalf("owner watch changed: %v", got)
	}
	// 覆盖而不是追加。
	if got, _ := s.SetWatched(ctx, owner, []string{"a/x"}); len(got) != 1 {
		t.Fatalf("overwrite: %v", got)
	}
	if _, err := s.SetWatched(ctx, owner, []string{"a/x", "bad name"}); err == nil {
		t.Fatal("invalid repo name accepted")
	}
	if all := s.watchedRepos(ctx); len(all) != 2 {
		t.Fatalf("union of watches = %v", all)
	}
}

func day(n int) time.Time { return time.Date(2026, 9, n, 0, 0, 0, 0, time.UTC) }

func sample() []Issue {
	return []Issue{
		{Repo: "o/a", Number: 1, Title: "Alpha 登录报错", State: "OPEN", Assignees: []string{"luca"}, Priority: "Low", Status: "待处理", UpdatedAt: day(1), Labels: []Label{{Name: "bug"}}},
		{Repo: "o/a", Number: 2, Title: "Beta", State: "OPEN", Assignees: []string{"luca"}, Priority: "Urgent", Status: "进行中", UpdatedAt: day(2)},
		{Repo: "o/a", Number: 3, Title: "Gamma", State: "OPEN", Assignees: []string{"someone"}, Priority: "High", Status: "待处理", UpdatedAt: day(3)},
		{Repo: "o/a", Number: 4, Title: "Delta", State: "OPEN", Assignees: []string{"luca"}, Status: "已完成", UpdatedAt: day(4)},
		{Repo: "o/a", Number: 5, Title: "Epsilon", State: "CLOSED", Assignees: []string{"luca"}, Priority: "High", Status: "已取消", UpdatedAt: day(5)},
		{Repo: "o/a", Number: 6, Title: "Zeta 无优先级", State: "OPEN", Assignees: []string{"luca"}, UpdatedAt: day(6)},
		{Repo: "o/a", Number: 7, Title: "Eta", State: "OPEN", Assignees: []string{"luca"}, Priority: "High", Status: "Done", UpdatedAt: day(7)},
	}
}

var rank = map[string]int{"Urgent": 0, "High": 1, "Medium": 2, "Low": 3}

func numbers(list []Issue) []int {
	out := make([]int, 0, len(list))
	for _, is := range list {
		out = append(out, is.Number)
	}
	return out
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 契约：零值查询 = 分配给我 + open + 排除做完 / 取消的看板列 + 按优先级
// 紧急在前、同档更新时间新的在前、没优先级的垫底。
func TestQuery_Defaults(t *testing.T) {
	got, total := Query{Login: "luca"}.apply(sample(), rank)
	want := []int{2, 1, 6}
	if total != 3 || !equalInts(numbers(got), want) {
		t.Fatalf("got %v (total %d), want %v", numbers(got), total, want)
	}
}

func TestQuery_Filters(t *testing.T) {
	cases := []struct {
		name string
		q    Query
		want []int
	}{
		{"全部人", Query{Login: "luca", Assignee: "all"}, []int{2, 3, 1, 6}},
		{"没配用户名就不按人筛", Query{}, []int{2, 3, 1, 6}},
		{"看板全部", Query{Login: "luca", Board: "all"}, []int{2, 7, 1, 6, 4}},
		{"指定看板列", Query{Login: "luca", Board: "待处理"}, []int{1}},
		{"closed", Query{Login: "luca", State: "closed", Board: "all"}, []int{5}},
		{"all 状态", Query{Login: "luca", State: "all", Board: "all"}, []int{2, 7, 5, 1, 6, 4}},
		{"优先级", Query{Login: "luca", Priority: "Low"}, []int{1}},
		{"标签", Query{Login: "luca", Label: "bug"}, []int{1}},
		{"关键词标题", Query{Login: "luca", Keyword: "登录"}, []int{1}},
		{"关键词编号", Query{Login: "luca", Keyword: "#6"}, []int{6}},
		{"按更新时间倒序", Query{Login: "luca", Sort: "updated", Desc: true}, []int{6, 2, 1}},
		{"按更新时间正序", Query{Login: "luca", Sort: "updated"}, []int{1, 2, 6}},
		{"优先级反向", Query{Login: "luca", Sort: "priority", Desc: false}, []int{6, 1, 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := tc.q.apply(sample(), rank)
			if !equalInts(numbers(got), tc.want) {
				t.Fatalf("got %v, want %v", numbers(got), tc.want)
			}
		})
	}
}

func TestQuery_Paging(t *testing.T) {
	q := Query{Login: "luca", Board: "all", Page: 2, PageSize: 2}
	got, total := q.apply(sample(), rank)
	if total != 5 || !equalInts(numbers(got), []int{1, 6}) {
		t.Fatalf("page 2 = %v (total %d)", numbers(got), total)
	}
	got, _ = Query{Login: "luca", Board: "all", Page: 9, PageSize: 2}.apply(sample(), rank)
	if len(got) != 0 {
		t.Fatalf("past the end should be empty, got %v", numbers(got))
	}
}

// 契约：GraphQL 回复里 issue 侧栏的 Priority 与所属看板的 Status 都要
// 读出来；看板列定义（顺序 + 颜色）取自 issue 所在 Project 的 Status 字段，
// 优先级档取自仓库的 issueFields。回复形状是对真实仓库实测的。
func TestParseFields(t *testing.T) {
	raw := []byte(`{"data":{"repository":{
	  "issueFields":{"nodes":[
	    {"name":"Priority","options":[{"name":"Urgent","color":"PINK"},{"name":"High","color":"RED"},{"name":"Medium","color":"YELLOW"},{"name":"Low","color":"GREEN"}]},
	    {"name":"Start date"},
	    {"name":"Effort","options":[{"name":"High","color":"RED"}]}]},
	  "i0":{"issueFieldValues":{"nodes":[]},"projectItems":{"nodes":[]}},
	  "i1":{"issueFieldValues":{"nodes":[{"name":"Urgent","color":"PINK","field":{"name":"Priority"}}]},
	        "projectItems":{"nodes":[{"project":{"number":11,"field":{"options":[{"name":"待处理","color":"GREEN"},{"name":"进行中","color":"YELLOW"},{"name":"待验收","color":"BLUE"},{"name":"已完成","color":"PURPLE"},{"name":"已取消","color":"GRAY"}]}},
	        "fieldValues":{"nodes":[{},{},{"name":"待处理","color":"GREEN","field":{"name":"Status"}}]}}]}}
	}}}`)
	out := &repoFields{byNumber: map[int]issueMeta{}}
	if err := parseFields(raw, []int{49, 124}, out); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(out.priorities) != 4 || out.priorities[0].Name != "Urgent" || out.priorities[3].Color != "GREEN" {
		t.Fatalf("priorities = %+v", out.priorities)
	}
	if len(out.statuses) != 5 || out.statuses[2].Name != "待验收" || out.statuses[4].Color != "GRAY" {
		t.Fatalf("statuses = %+v", out.statuses)
	}
	if m := out.byNumber[124]; m.priority != "Urgent" || m.priorityColor != "PINK" || m.status != "待处理" || m.statusColor != "GREEN" {
		t.Fatalf("issue 124 meta = %+v", m)
	}
	if m := out.byNumber[49]; m.priority != "" || m.status != "" {
		t.Fatalf("issue 49 should be blank, got %+v", m)
	}

	if err := parseFields([]byte(`{"errors":[{"message":"Could not resolve to a Repository"}]}`), nil, out); err == nil {
		t.Fatal("graphql error should surface")
	}
}

func TestIsDone(t *testing.T) {
	for s, want := range map[string]bool{"已完成": true, "已取消": true, "Done": true, "Cancelled": true, "closed": true, "待处理": false, "进行中": false, "待验收": false, "": false} {
		if IsDone(s) != want {
			t.Errorf("IsDone(%q) = %v", s, !want)
		}
	}
}
