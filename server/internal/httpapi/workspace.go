package httpapi

import (
	"net/http"

	"acpp/server/internal/service"
)

// cwdResolver 解析这次请求要看哪个目录。三种来源共用同一批 handler：
// 普通会话、编排会话（记录存在哪张表的差别）、以及**草稿态**——会话还
// 没建，目录由请求直接给（`?cwd=`）。
//
// 取整个请求而不只是 context：解析时要顺带做归属校验与路径闸，而身份是
// 从请求里读出来的。工作区的全部数据面因此共用同一道闸，不用每个 handler
// 自己记得校验（adr-007）。
type cwdResolver func(r *http.Request) (string, error)

// workspaceHandler 提供工作区面板的数据面：文件树与文件预览。
// 一切路径以会话 cwd 为边界，canonical guard 在 service 层。
type workspaceHandler struct {
	cwdOf cwdResolver
}

func (h workspaceHandler) tree(w http.ResponseWriter, r *http.Request) {
	cwd, err := h.cwdOf(r)
	if err != nil {
		writeError(w, err)
		return
	}
	listing, err := service.WorkspaceTree(r.Context(), cwd, r.URL.Query().Get("path"), queryInt(r, "depth", 1))
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, listing)
}

func (h workspaceHandler) file(w http.ResponseWriter, r *http.Request) {
	cwd, err := h.cwdOf(r)
	if err != nil {
		writeError(w, err)
		return
	}
	view, err := service.WorkspaceFile(cwd, r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, view)
}

func (h workspaceHandler) gitOverview(w http.ResponseWriter, r *http.Request) {
	cwd, err := h.cwdOf(r)
	if err != nil {
		writeError(w, err)
		return
	}
	overview, err := service.WorkspaceGitOverview(r.Context(), cwd)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, overview)
}

func (h workspaceHandler) gitDiff(w http.ResponseWriter, r *http.Request) {
	cwd, err := h.cwdOf(r)
	if err != nil {
		writeError(w, err)
		return
	}
	view, err := service.WorkspaceGitDiff(r.Context(), cwd, r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, view)
}

// history 是提交链路面板的一页（?ref= 按分支/标签过滤，?limit=&offset= 翻页）。
func (h workspaceHandler) history(w http.ResponseWriter, r *http.Request) {
	cwd, err := h.cwdOf(r)
	if err != nil {
		writeError(w, err)
		return
	}
	history, err := service.WorkspaceGitHistory(
		r.Context(),
		cwd,
		r.URL.Query().Get("ref"),
		queryInt(r, "limit", 50),
		queryInt(r, "offset", 0),
	)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, history)
}

// compare 对比两个 ref：head 相对 base 多出的提交与文件变更。
func (h workspaceHandler) compare(w http.ResponseWriter, r *http.Request) {
	cwd, err := h.cwdOf(r)
	if err != nil {
		writeError(w, err)
		return
	}
	compare, err := service.WorkspaceGitCompare(
		r.Context(), cwd, r.URL.Query().Get("base"), r.URL.Query().Get("head"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, compare)
}

// branches 是会话底部分支控件的数据：当前分支、本地/远端分支、worktree 清单。
func (h workspaceHandler) branches(w http.ResponseWriter, r *http.Request) {
	cwd, err := h.cwdOf(r)
	if err != nil {
		writeError(w, err)
		return
	}
	view, err := service.WorkspaceGitBranches(r.Context(), cwd)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, view)
}

// checkout 切换分支（可新建），返回切换后的分支视图。
func (h workspaceHandler) checkout(w http.ResponseWriter, r *http.Request) {
	cwd, err := h.cwdOf(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var in service.CheckoutInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	view, err := service.WorkspaceGitCheckout(r.Context(), cwd, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, view)
}

// createWorktree 在会话仓库下开一个隔离工作区，返回它的路径——
// 从这里开新会话就是「在 worktree 里干活」。
func (h workspaceHandler) createWorktree(w http.ResponseWriter, r *http.Request) {
	cwd, err := h.cwdOf(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var in service.WorktreeInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	path, err := service.CreateWorktree(r.Context(), scopeOf(r), cwd, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusCreated, map[string]string{"path": path})
}

// removeWorktree 拆掉一个 worktree（分支保留）。
func (h workspaceHandler) removeWorktree(w http.ResponseWriter, r *http.Request) {
	if _, err := h.cwdOf(r); err != nil {
		writeError(w, err)
		return
	}
	var in struct {
		Path string `json:"path"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	if err := service.RemoveWorktree(r.Context(), scopeOf(r), in.Path); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, nil)
}

// gitCommit 带 ?path= 时返回该文件在这条提交前后的全文，否则返回提交详情。
func (h workspaceHandler) gitCommit(w http.ResponseWriter, r *http.Request) {
	cwd, err := h.cwdOf(r)
	if err != nil {
		writeError(w, err)
		return
	}
	detail, diff, err := service.WorkspaceGitCommit(
		r.Context(), cwd, r.PathValue("sha"), r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, err)
		return
	}
	if diff != nil {
		writeData(w, http.StatusOK, diff)
		return
	}
	writeData(w, http.StatusOK, detail)
}
