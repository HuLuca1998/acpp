package httpapi

import (
	"net/http"

	"acpp/server/internal/model"
	"acpp/server/internal/remote"
)

// serverHandler 是远程服务器的管理面（owner 专属，前缀已在 isOwnerOnly
// 覆盖——这些记录里躺着生产机的 SSH 凭证）。
type serverHandler struct {
	servers *remote.Service
}

// list 不分页：服务器是个位数量级的配置，翻页只会让前端多一层状态。
func (h serverHandler) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.servers.List(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, page[model.Server]{
		Items:    items,
		Total:    int64(len(items)),
		Page:     1,
		PageSize: len(items),
	})
}

func (h serverHandler) create(w http.ResponseWriter, r *http.Request) {
	var in remote.Input
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	srv, err := h.servers.Create(r.Context(), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusCreated, srv)
}

func (h serverHandler) get(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	srv, err := h.servers.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, srv)
}

func (h serverHandler) update(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	var in remote.Input
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	srv, err := h.servers.Update(r.Context(), id, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, srv)
}

func (h serverHandler) remove(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	if err := h.servers.Delete(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// probe 测一份还没保存的配置（新建对话框里的「测试连接」）。
func (h serverHandler) probe(w http.ResponseWriter, r *http.Request) {
	var in remote.Input
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	h.writeTest(w, r, 0, in)
}

// test 测一条已存的记录，body 里的改动会先合并进去——编辑到一半就想
// 验一下的场景，不该逼用户先保存。
func (h serverHandler) test(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	var in remote.Input
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	h.writeTest(w, r, id, in)
}

func (h serverHandler) writeTest(w http.ResponseWriter, r *http.Request, id uint, in remote.Input) {
	banner, err := h.servers.Test(r.Context(), id, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"version": banner})
}
