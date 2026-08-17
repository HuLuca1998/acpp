package httpapi

import (
	"net/http"

	"acpp/server/internal/service"
)

// cwdResolver 按会话 id 解析工作目录——普通会话与编排会话的唯一差异
// 就是记录存在哪张表里，工作区数据面本身只关心 cwd。
//
// 取整个请求而不只是 context：解析时要顺带做归属校验（会话不属于当前
// 身份就当作不存在），而身份是从请求里读出来的。工作区的全部数据面因此
// 共用同一道闸，不用每个 handler 自己记得校验（adr-007）。
type cwdResolver func(r *http.Request, id uint) (string, error)

// workspaceHandler 提供工作区面板的数据面：文件树与文件预览。
// 一切路径以会话 cwd 为边界，canonical guard 在 service 层。
type workspaceHandler struct {
	cwdOf cwdResolver
}

func (h workspaceHandler) tree(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	cwd, err := h.cwdOf(r, id)
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
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	cwd, err := h.cwdOf(r, id)
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
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	cwd, err := h.cwdOf(r, id)
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
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	cwd, err := h.cwdOf(r, id)
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

// gitCommit 带 ?path= 时返回该文件在这条提交前后的全文，否则返回提交详情。
func (h workspaceHandler) gitCommit(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	cwd, err := h.cwdOf(r, id)
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
