package httpapi

import (
	"net/http"
	"strings"

	"acpp/server/internal/github"
)

// githubHandler 是 GitHub issue 页的 HTTP 面。租户可用：数据是 owner 本机
// gh 登录态拉的公共视图，「分配给我」按访客记录上的 GitHub 用户名筛。
type githubHandler struct {
	github *github.Service
}

// issues 按条件返回一页 issue；查询参数与 github.Query 一一对应。
func (h githubHandler) issues(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pageNum, pageSize := pageParams(r)
	sort := sortParams(r, "priority", "updated")
	query := github.Query{
		Repos:    splitList(q.Get("repos")),
		Keyword:  q.Get("q"),
		Assignee: q.Get("assignee"),
		State:    q.Get("state"),
		Board:    q.Get("board"),
		Priority: q.Get("priority"),
		Label:    q.Get("label"),
		Sort:     sort.Column,
		Desc:     sort.Desc,
		Refresh:  q.Get("refresh") == "1",
		Page:     pageNum,
		PageSize: pageSize,
	}
	id := identityOf(r)
	login := ""
	if id.tenant != nil {
		login = id.tenant.GithubLogin
	}
	res, err := h.github.Issues(r.Context(), id.scope(), login, query)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, res)
}

// repos 列出可关注的仓库并标出已关注的。
func (h githubHandler) repos(w http.ResponseWriter, r *http.Request) {
	repos, err := h.github.Repos(r.Context(), scopeOf(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, repos)
}

// updateRepos 覆盖当前身份的关注清单。
func (h githubHandler) updateRepos(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Repos []string `json:"repos"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	saved, err := h.github.SetWatched(r.Context(), scopeOf(r), in.Repos)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, saved)
}

// splitList 把逗号分隔的查询参数拆成清单，空项丢掉。
func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
