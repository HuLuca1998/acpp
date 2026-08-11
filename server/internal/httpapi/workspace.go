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
