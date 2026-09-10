package github

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Query 是 issue 列表的筛选、排序与分页条件。零值不过滤。
type Query struct {
	// Repos 限定仓库（必须在关注清单内）；空表示全部关注的仓库。
	Repos []string
	// Keyword 对标题做子串匹配；纯数字（可带 #）也匹配编号。
	Keyword string
	// Assignee 是 "me"（默认，按 Login 筛）或 "all"。
	Assignee string
	// Login 是「me」实际对应的 GitHub 用户名；空时 "me" 退化为不过滤。
	Login string
	// State 是 open（默认）/ closed / all。
	State string
	// Board 是看板列筛选："active"（默认，排除做完 / 取消的列）、"all"，
	// 或某个具体列名。
	Board string
	// Priority / Label 精确匹配一个档 / 一个标签。
	Priority string
	Label    string
	// Sort 是 priority（默认）或 updated；Desc 控制方向。
	Sort string
	Desc bool
	// Refresh 为 true 时先刷新缓存再查。
	Refresh  bool
	Page     int
	PageSize int
}

// doneStatusRe 认出「做完了」的看板列：这些列上的 issue 默认不进列表。
// 用户的看板列名是自定义的，这里按常见写法认——中文的已完成 / 已取消，
// 英文的 Done / Closed / Cancel(l)ed。
var doneStatusRe = regexp.MustCompile(`(?i)完成|取消|关闭|^done$|^closed$|cancel`)

// IsDone 判断一个看板列是不是「做完了」的列。
func IsDone(status string) bool {
	return status != "" && doneStatusRe.MatchString(status)
}

// apply 过滤、排序并切页。priorityRank 是优先级名 → 位次（0 最紧急）。
func (q Query) apply(all []Issue, priorityRank map[string]int) ([]Issue, int) {
	out := make([]Issue, 0, len(all))
	for _, is := range all {
		if q.match(is) {
			out = append(out, is)
		}
	}
	q.sortIssues(out, priorityRank)

	total := len(out)
	page, size := q.Page, q.PageSize
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	start := (page - 1) * size
	if start >= total {
		return []Issue{}, total
	}
	return out[start:min(start+size, total)], total
}

func (q Query) match(is Issue) bool {
	switch strings.ToLower(q.State) {
	case "", "open":
		if is.State != "OPEN" {
			return false
		}
	case "closed":
		if is.State != "CLOSED" {
			return false
		}
	}

	if q.Assignee != "all" && q.Login != "" {
		found := false
		for _, a := range is.Assignees {
			if strings.EqualFold(a, q.Login) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	switch q.Board {
	case "", "active":
		// 没挂看板的 issue 没有列可判，只看它本身关没关。
		if IsDone(is.Status) {
			return false
		}
	case "all":
	default:
		if is.Status != q.Board {
			return false
		}
	}

	if q.Priority != "" && is.Priority != q.Priority {
		return false
	}
	if q.Label != "" {
		found := false
		for _, l := range is.Labels {
			if l.Name == q.Label {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		if n, err := strconv.Atoi(strings.TrimPrefix(kw, "#")); err == nil && is.Number == n {
			return true
		}
		if !strings.Contains(strings.ToLower(is.Title), strings.ToLower(kw)) {
			return false
		}
	}
	return true
}

// sortIssues 排序。优先级排序里没有优先级的排最后；同档按更新时间新的在前。
// 稳定排序，保证翻页时同一条不会在两页间跳。
func (q Query) sortIssues(list []Issue, priorityRank map[string]int) {
	rank := func(is Issue) int {
		if r, ok := priorityRank[is.Priority]; ok && is.Priority != "" {
			return r
		}
		return len(priorityRank) + 1
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if q.Sort == "updated" {
			if q.Desc {
				return a.UpdatedAt.After(b.UpdatedAt)
			}
			return a.UpdatedAt.Before(b.UpdatedAt)
		}
		ra, rb := rank(a), rank(b)
		if ra != rb {
			// Desc 在优先级语义里是「紧急的在前」，也是默认方向。
			if q.Desc || q.Sort == "" {
				return ra < rb
			}
			return ra > rb
		}
		return a.UpdatedAt.After(b.UpdatedAt)
	})
}
