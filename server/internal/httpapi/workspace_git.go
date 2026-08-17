package httpapi

import (
	"net/http"

	"acpp/server/internal/service"
)

// git 写操作的 handler（提交/推送/拉取/合并/分支增删/丢弃改动）。
//
// 与读数据面分文件：读是「画面板」，写是「动仓库」——后者每一条都要能
// 把 git 的原话带回界面，失败原因（推送被拒、合并冲突）本身就是下一步
// 该做什么的说明。

// gitOp 把「解析入参 → 调 service → 回结果」这段样板收成一处：
// 六个写操作只差中间那一句。
func (h workspaceHandler) gitOp(
	w http.ResponseWriter,
	r *http.Request,
	run func(cwd string) (*service.GitOpResult, error),
) {
	cwd, err := h.cwdOf(r)
	if err != nil {
		writeError(w, err)
		return
	}
	result, err := run(cwd)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, result)
}

// commit 提交工作区全部改动（含未跟踪文件）。
func (h workspaceHandler) commit(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Message string `json:"message"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	h.gitOp(w, r, func(cwd string) (*service.GitOpResult, error) {
		return service.WorkspaceGitCommitAll(r.Context(), cwd, in.Message)
	})
}

// push 推送当前分支（没有 upstream 时顺手建立跟踪）。
func (h workspaceHandler) push(w http.ResponseWriter, r *http.Request) {
	h.gitOp(w, r, func(cwd string) (*service.GitOpResult, error) {
		return service.WorkspaceGitPush(r.Context(), cwd)
	})
}

// pull 拉取当前分支，只接受快进。
func (h workspaceHandler) pull(w http.ResponseWriter, r *http.Request) {
	h.gitOp(w, r, func(cwd string) (*service.GitOpResult, error) {
		return service.WorkspaceGitPull(r.Context(), cwd)
	})
}

// merge 把指定 ref 合并进当前分支。
func (h workspaceHandler) merge(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Ref string `json:"ref"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	h.gitOp(w, r, func(cwd string) (*service.GitOpResult, error) {
		return service.WorkspaceGitMerge(r.Context(), cwd, in.Ref)
	})
}

// createBranch 新建分支（`{name, from?, checkout?}`）。
func (h workspaceHandler) createBranch(w http.ResponseWriter, r *http.Request) {
	var in service.BranchInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	h.gitOp(w, r, func(cwd string) (*service.GitOpResult, error) {
		return service.WorkspaceGitCreateBranch(r.Context(), cwd, in)
	})
}

// deleteBranch 删本地分支（`?force=1` 才用 -D）。
func (h workspaceHandler) deleteBranch(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	force := r.URL.Query().Get("force") == "1"
	h.gitOp(w, r, func(cwd string) (*service.GitOpResult, error) {
		return service.WorkspaceGitDeleteBranch(r.Context(), cwd, name, force)
	})
}

// discard 丢弃改动：`{paths}` 为空表示整个工作区。
func (h workspaceHandler) discard(w http.ResponseWriter, r *http.Request) {
	cwd, err := h.cwdOf(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var in struct {
		Paths []string `json:"paths"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	if err := service.WorkspaceGitDiscard(r.Context(), cwd, in.Paths); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, nil)
}
