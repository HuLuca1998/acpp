package httpapi

import (
	"net/http"

	"acpp/server/internal/project"
)

// projectHandler 是工作区项目面：列表、新建、克隆、删除，以及克隆对话框
// 用的远端仓库清单。一切路径由 service.Scope 钉在身份的工作区根内。
type projectHandler struct {
	projects *project.Service
}

func (h projectHandler) list(w http.ResponseWriter, r *http.Request) {
	projects, err := h.projects.List(r.Context(), scopeOf(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, newPage(projects))
}

func (h projectHandler) create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	created, err := h.projects.Create(scopeOf(r), in.Name)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusCreated, created)
}

func (h projectHandler) remove(w http.ResponseWriter, r *http.Request) {
	if err := h.projects.Delete(scopeOf(r), r.PathValue("name")); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, nil)
}

// clone 起一个后台克隆任务，立即返回；进度由 clones 端点轮询。
func (h projectHandler) clone(w http.ResponseWriter, r *http.Request) {
	var in project.CloneInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	task, err := h.projects.Clone(scopeOf(r), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusAccepted, task)
}

func (h projectHandler) clones(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, newPage(h.projects.Clones(scopeOf(r))))
}

// repos 是克隆对话框的可选仓库清单（gh CLI，排除个人账号名下的仓库）。
func (h projectHandler) repos(w http.ResponseWriter, r *http.Request) {
	repos, err := project.Repos(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, newPage(repos))
}
