package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"acpp/server/internal/datasource"
	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

// datasourceHandler 是数据库数据源的管理面（owner 专属，前缀已在
// isOwnerOnly 覆盖）与查询面。
type datasourceHandler struct {
	sources *datasource.Service
	// cwdOf 解析会话工作目录，会话侧的项目过滤靠它。
	cwdOf func(*http.Request) (string, error)
}

func (h datasourceHandler) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.sources.List(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (h datasourceHandler) create(w http.ResponseWriter, r *http.Request) {
	var in datasource.Input
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	src, err := h.sources.Create(r.Context(), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusCreated, src)
}

func (h datasourceHandler) get(w http.ResponseWriter, r *http.Request) {
	src, err := h.byID(r)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, src)
}

func (h datasourceHandler) update(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	var in datasource.Input
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	src, err := h.sources.Update(r.Context(), id, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, src)
}

func (h datasourceHandler) remove(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	if err := h.sources.Delete(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"deleted": true})
}

// test 拨一次连接确认配置可用。
func (h datasourceHandler) test(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	version, err := h.sources.Test(r.Context(), id)
	if err != nil {
		// 连不上是配置问题不是服务故障，让前端拿到原话展示给用户。
		writeData(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeData(w, http.StatusOK, map[string]any{"ok": true, "version": version})
}

func (h datasourceHandler) databases(w http.ResponseWriter, r *http.Request) {
	src, err := h.byID(r)
	if err != nil {
		writeError(w, err)
		return
	}
	list, err := datasource.Databases(r.Context(), src)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, list)
}

func (h datasourceHandler) tables(w http.ResponseWriter, r *http.Request) {
	src, err := h.byID(r)
	if err != nil {
		writeError(w, err)
		return
	}
	list, err := datasource.Tables(r.Context(), src, r.URL.Query().Get("database"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, list)
}

func (h datasourceHandler) schema(w http.ResponseWriter, r *http.Request) {
	src, err := h.byID(r)
	if err != nil {
		writeError(w, err)
		return
	}
	detail, err := datasource.Describe(r.Context(), src,
		r.URL.Query().Get("database"), r.URL.Query().Get("table"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, detail)
}

// query 执行一段可含多条语句的 SQL。
func (h datasourceHandler) query(w http.ResponseWriter, r *http.Request) {
	src, err := h.byID(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var req struct {
		Database string `json:"database"`
		SQL      string `json:"sql"`
		MaxRows  int    `json:"maxRows"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	// 界面上的 SQL 控制台按数据源的只读开关放行：连接配成只读时，
	// 人在这里也跑不了写语句——一个开关管住所有入口。
	res, err := datasource.Execute(r.Context(), src, req.Database, req.SQL, req.MaxRows, !src.ReadOnly)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, res)
}

// sessionList 是会话侧的数据源清单：只给当前工作目录所属项目的那几条。
// 斜杠命令读它——界面看到的范围与 AI 能操作的范围必须是同一个。
func (h datasourceHandler) sessionList(w http.ResponseWriter, r *http.Request) {
	sources, err := h.sessionSources(r)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, sources)
}

func (h datasourceHandler) sessionDatabases(w http.ResponseWriter, r *http.Request) {
	src, err := h.sessionSource(r)
	if err != nil {
		writeError(w, err)
		return
	}
	list, err := datasource.Databases(r.Context(), src)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, list)
}

func (h datasourceHandler) sessionTables(w http.ResponseWriter, r *http.Request) {
	src, err := h.sessionSource(r)
	if err != nil {
		writeError(w, err)
		return
	}
	list, err := datasource.Tables(r.Context(), src, r.URL.Query().Get("database"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, list)
}

// mcp 是 agent 子进程回连的数据库工具端点（token 即凭证，见 isPublicPath）。
// 形状与编排端点一致：POST 走 JSON-RPC，DELETE 是 session 终止通知，
// 其余方法一律不受理（我们没有 server 主动通知要发，不实现 GET 事件流）。
func (h datasourceHandler) mcp(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
	case http.MethodDelete:
		w.WriteHeader(http.StatusOK)
		return
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	resp, hasResp := h.sources.HandleMCP(r.Context(), r.PathValue("token"), raw)
	if !hasResp {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		// 连接已断（长查询时 client 放弃等待是常态），只能放弃响应。
		return
	}
}

func (h datasourceHandler) byID(r *http.Request) (*model.DataSource, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	return h.sources.Get(r.Context(), id)
}

// sessionSources 取会话所在项目的数据源。
func (h datasourceHandler) sessionSources(r *http.Request) ([]model.DataSource, error) {
	// 租户拿不到任何数据源：数据库连接是 owner 的资产（adr-007 的 owner
	// 专属面），不因为会话开在哪个目录而外借。
	if !identityOf(r).owner {
		return []model.DataSource{}, nil
	}
	cwd, err := h.cwdOf(r)
	if err != nil {
		return nil, err
	}
	sources, err := h.sources.ForCwd(r.Context(), cwd, false)
	if err != nil {
		return nil, err
	}
	if sources == nil {
		sources = []model.DataSource{}
	}
	return sources, nil
}

// sessionSource 在会话可见的数据源里按 id 定位一条——项目之外的 id
// 一律按「不存在」处理，会话侧不存在绕过项目边界的读法。
func (h datasourceHandler) sessionSource(r *http.Request) (*model.DataSource, error) {
	sources, err := h.sessionSources(r)
	if err != nil {
		return nil, err
	}
	raw := r.PathValue("dsid")
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: bad id %q", service.ErrInvalid, raw)
	}
	for i := range sources {
		if sources[i].ID == uint(id) {
			// 清单里的记录不带密码，连接要用完整记录。
			return h.sources.Get(r.Context(), uint(id))
		}
	}
	return nil, fmt.Errorf("%w: datasource %d", service.ErrNotFound, id)
}
