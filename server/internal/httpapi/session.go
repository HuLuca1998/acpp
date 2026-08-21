package httpapi

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

type sessionHandler struct {
	sessions *service.SessionService
	chat     *service.ChatService
}

func (h sessionHandler) list(w http.ResponseWriter, r *http.Request) {
	var agentID uint
	if raw := r.URL.Query().Get("agentId"); raw != "" {
		id, err := parseID(raw)
		if err != nil {
			writeError(w, err)
			return
		}
		agentID = id
	}
	pageNum, pageSize := pageParams(r)

	sort := sortParams(r, "id", "title", "agent_id", "tenant_id", "message_count", "state", "updated_at", "created_at")
	sessions, total, err := h.sessions.List(r.Context(), scopeOf(r), agentID, pageNum, pageSize, sort.OrderBy(""))
	if err != nil {
		writeError(w, err)
		return
	}
	// 消息数走缓存列；这里只补实时的进程状态。
	for i := range sessions {
		sessions[i].Running = h.chat.Running(sessions[i].ID)
	}
	writeData(w, http.StatusOK, page[service.SessionView]{
		Items:    sessions,
		Total:    total,
		Page:     pageNum,
		PageSize: pageSize,
	})
}

// overview 是概览页的聚合统计。单独一个端点而不是让前端拿分页列表自己算
// ——前 50 条算出来的「最近两周趋势」是错的（见 service.OverviewStats）。
func (h sessionHandler) overview(w http.ResponseWriter, r *http.Request) {
	stats, err := h.sessions.Overview(r.Context(), scopeOf(r), queryInt(r, "days", 14))
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, stats)
}

func (h sessionHandler) get(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	// 归属先行：不属于当前身份的会话当作不存在（adr-007）。
	if _, err := h.sessions.Get(r.Context(), scopeOf(r), id); err != nil {
		writeError(w, err)
		return
	}

	// Peek 不拉进程：查看记录是零成本读操作。
	session, err := h.chat.Peek(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, session)
}

func (h sessionHandler) create(w http.ResponseWriter, r *http.Request) {
	var in service.SessionInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}

	session, err := h.sessions.Create(r.Context(), scopeOf(r), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusCreated, session)
}

func (h sessionHandler) remove(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	if _, err := h.sessions.Get(r.Context(), scopeOf(r), id); err != nil {
		writeError(w, err)
		return
	}

	// 先收子进程再删记录，否则 agent 会变成没人认领的孤儿进程。
	if err := h.chat.Destroy(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	if err := h.sessions.Delete(r.Context(), scopeOf(r), id); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, nil)
}

// listMessages 从转录重建消息列表；会话必须存在。
// ?limit= 取尾部 N 条（默认全量），?before=<id> 是「加载更早」的游标。
func (h sessionHandler) listMessages(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	if _, err := h.sessions.Get(r.Context(), scopeOf(r), id); err != nil {
		writeError(w, err)
		return
	}

	limit := queryInt(r, "limit", 0)
	before := uint(queryInt(r, "before", 0))
	messages, total, err := h.chat.Messages(id, limit, before)
	if err != nil {
		writeError(w, err)
		return
	}
	if messages == nil {
		// 空转录是合法状态（会话刚建、首发还没落）：契约是 items 数组，
		// 不能让 nil 切片序列化成 null 砸到前端。
		messages = []model.Message{}
	}
	writeData(w, http.StatusOK, page[model.Message]{
		Items:    messages,
		Total:    int64(total),
		Page:     1,
		PageSize: len(messages),
	})
}

// outline 返回会话的提问索引：全部用户提问的锚点与文案，供界面在对话
// 左侧排成一列可跳转的刻度。
//
// 不分页也没有游标——它抽的是「整条会话有哪些提问」，本来就该一次看全，
// 而且服务端是在已缓存的重建结果上遍历，成本与消息列表的一页相当。
func (h sessionHandler) outline(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	if _, err := h.sessions.Get(r.Context(), scopeOf(r), id); err != nil {
		writeError(w, err)
		return
	}

	outline, err := h.chat.Outline(id)
	if err != nil {
		writeError(w, err)
		return
	}
	if outline.Items == nil {
		// 没有提问是合法状态（会话刚建）：契约是数组，不能序列化成 null。
		outline.Items = []service.OutlineEntry{}
	}
	writeData(w, http.StatusOK, outline)
}

// toolOutput 返回一次工具调用的完整入出参——列表里超大字段默认截成
// 预览（rawInputTruncated / rawOutputTruncated 标记），展开工具卡时走这里。
func (h sessionHandler) toolOutput(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	if _, err := h.sessions.Get(r.Context(), scopeOf(r), id); err != nil {
		writeError(w, err)
		return
	}

	detail, err := h.chat.ToolOutput(id, r.PathValue("toolCallId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, detail)
}

// transcript 原样下发会话的 JSONL 转录，供导出与调试。
func (h sessionHandler) transcript(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	if _, err := h.sessions.Get(r.Context(), scopeOf(r), id); err != nil {
		writeError(w, err)
		return
	}

	// 日志面板每 2 秒带 Range 尾随读一次转录。「已经读到末尾」是这条路径上
	// 最常见的结果，不是错误——交给 ServeFile 它会回 416，浏览器把那当失败
	// 资源在控制台刷红字，一分钟三十条，真正的报错反而被淹掉。
	path := h.chat.TranscriptPath(id)
	switch tailState(r, path) {
	case tailEmpty:
		// 没有新内容（或转录还没落盘）。204 才是这件事的名字。
		w.WriteHeader(http.StatusNoContent)
		return
	case tailRewound:
		// 文件比偏移还短，说明被重建过。摘掉 Range 让 ServeFile 回全量，
		// 前端的 reset 分支会整体替换而不是接着往后追加。
		r.Header.Del("Range")
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	http.ServeFile(w, r, path)
}

// 尾随读的三种局面。
type tailKind int

const (
	// tailNormal：有内容可读，照常交给 ServeFile。
	tailNormal tailKind = iota
	// tailEmpty：偏移已在末尾，或者转录还没落盘。
	tailEmpty
	// tailRewound：文件比偏移还短，被重建过，得重新给全量。
	tailRewound
)

func tailState(r *http.Request, path string) tailKind {
	info, err := os.Stat(path)
	if err != nil {
		// 会话本身已经校验过存在，那这里就是「还没写过转录」。
		return tailEmpty
	}
	start, ok := rangeStart(r.Header.Get("Range"))
	if !ok {
		return tailNormal
	}
	switch {
	case start == info.Size():
		return tailEmpty
	case start > info.Size():
		return tailRewound
	default:
		return tailNormal
	}
}

// rangeStart 取 `bytes=N-` 里的 N。只认这一种写法——转录是 append-only，
// 前端只会「从某个偏移读到末尾」；别的写法交回 ServeFile 按标准处理。
func rangeStart(header string) (int64, bool) {
	spec, ok := strings.CutPrefix(strings.TrimSpace(header), "bytes=")
	if !ok {
		return 0, false
	}
	digits, ok := strings.CutSuffix(spec, "-")
	if !ok {
		return 0, false
	}
	start, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || start < 0 {
		return 0, false
	}
	return start, true
}
