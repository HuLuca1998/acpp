package httpapi

import (
	"net/http"

	"acpp/server/internal/service"
)

// workspaceHandler 提供工作区面板的数据面：文件树与文件预览。
// 一切路径以会话 cwd 为边界，canonical guard 在 service 层。
type workspaceHandler struct {
	sessions *service.SessionService
}

func (h workspaceHandler) tree(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	session, err := h.sessions.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	listing, err := service.WorkspaceTree(r.Context(), session.Cwd, r.URL.Query().Get("path"), queryInt(r, "depth", 1))
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
	session, err := h.sessions.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	view, err := service.WorkspaceFile(session.Cwd, r.URL.Query().Get("path"))
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
	session, err := h.sessions.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	overview, err := service.WorkspaceGitOverview(r.Context(), session.Cwd)
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
	session, err := h.sessions.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	view, err := service.WorkspaceGitDiff(r.Context(), session.Cwd, r.URL.Query().Get("path"))
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
	session, err := h.sessions.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	detail, diff, err := service.WorkspaceGitCommit(
		r.Context(), session.Cwd, r.PathValue("sha"), r.URL.Query().Get("path"))
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
