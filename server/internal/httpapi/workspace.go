package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"acpp/server/internal/fswatch"
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
	// 文件变动监视器（见本文件末尾的 watch）。缺席时监视流直接回
	// unavailable，其余数据面照常工作。
	watcher watcherHub
}

func (h workspaceHandler) tree(w http.ResponseWriter, r *http.Request) {
	cwd, err := h.cwdOf(r)
	if err != nil {
		writeError(w, err)
		return
	}
	listing, err := service.WorkspaceTree(cwd, r.URL.Query().Get("path"), queryInt(r, "depth", 1))
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

// table 把 csv/tsv/xlsx 解析成表格视图（多页工作簿一次给全）。
//
// 与 fs/file 分开是因为它们是两件事：那个给的是「文件的字节」，这个给的
// 是「摊平后的行列」——同一个 csv，源码视图与表格视图都要能看。
func (h workspaceHandler) table(w http.ResponseWriter, r *http.Request) {
	cwd, err := h.cwdOf(r)
	if err != nil {
		writeError(w, err)
		return
	}
	view, err := service.WorkspaceTable(cwd, r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, view)
}

// download 原样下发一个工作区文件（浏览器另存为）。预览接口是给「看」的
// （文本、截断、二进制只标记），下载要的是原始字节。
func (h workspaceHandler) download(w http.ResponseWriter, r *http.Request) {
	cwd, err := h.cwdOf(r)
	if err != nil {
		writeError(w, err)
		return
	}
	path := r.URL.Query().Get("path")

	// 打包下载：边写边发，大目录不占内存。头必须在写第一个字节前发完。
	// path 可以给多个（?path=a&path=b），文件树多选批量下载走这条。
	if r.URL.Query().Get("archive") == "1" {
		paths := r.URL.Query()["path"]
		// 包名在写第一个字节之前就得定下来（Content-Disposition 要先发），
		// 所以从参数算，而不是等打包函数回报。
		name := "files"
		if len(paths) == 1 {
			base := filepath.Base(filepath.Clean(paths[0]))
			if base != "." && base != string(filepath.Separator) {
				name = base
			} else {
				name = "workspace"
			}
		}
		w.Header().Set("Content-Type", "application/zip")
		setAttachment(w, name+".zip")
		if err := service.WorkspaceZip(cwd, paths, w); err != nil {
			// 已经开始写 body 的话改不了状态码了——错误只能落日志，
			// 客户端会看到一个不完整的 zip。头还没发时正常报错。
			writeError(w, err)
		}
		return
	}

	target, err := service.WorkspaceFilePath(cwd, path)
	if err != nil {
		writeError(w, err)
		return
	}
	name := filepath.Base(target)

	// inline=1 是「在浏览器里直接看」：给出真实类型并让浏览器自己渲染
	// ——PDF、图片、音视频、纯文本它全都会画，比在面板里各写一个渲染器
	// 划算得多。认不出的类型照旧走另存为：浏览器打不开的东西，摆出一个
	// 空白标签页不如老老实实给文件。
	if ctype := inlineContentType(name); ctype != "" && r.URL.Query().Get("inline") == "1" {
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Content-Disposition",
			fmt.Sprintf("inline; filename=%q; filename*=UTF-8''%s",
				name, url.PathEscape(name)))
		// 会执行脚本的文档才上沙箱。这些字节来自工作目录，可能是 agent 刚
		// 下载或生成的 HTML/SVG——它们与本应用同源，不设防就能在页面里跑
		// 脚本、读走身份 cookie（adr-007 的凭证就在那儿）。
		//
		// 反过来，**不能**给所有类型一律套沙箱：浏览器的内建 PDF 查看器在
		// 沙箱文档里用不了，结果是本该看得见的 PDF 变成一个下载框（实测
		// Chrome 就是这样）。图片、音视频本身不执行脚本，不需要这道防线。
		if sandboxedTypes[strings.ToLower(filepath.Ext(name))] {
			w.Header().Set("Content-Security-Policy", "sandbox")
		}
		// 类型是我们按扩展名说了算的，不许浏览器再去嗅探内容改主意。
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeFile(w, r, target)
		return
	}

	setAttachment(w, name)
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, target)
}

// inlineContentType 给浏览器能自己渲染的文件类型；返回空串表示「让它下载」。
//
// 白名单而不是 mime.TypeByExtension 全放行：系统 mime 表里有一堆浏览器
// 根本不认的类型（.doc、.zip……），报出去只会得到一个空白标签页。
var inlineTypes = map[string]string{
	".txt": "text/plain; charset=utf-8", ".log": "text/plain; charset=utf-8",
	".md": "text/plain; charset=utf-8", ".csv": "text/plain; charset=utf-8",
	".tsv": "text/plain; charset=utf-8", ".json": "application/json; charset=utf-8",
	".xml": "text/plain; charset=utf-8", ".yaml": "text/plain; charset=utf-8",
	".yml":  "text/plain; charset=utf-8",
	".html": "text/html; charset=utf-8", ".htm": "text/html; charset=utf-8",
	".pdf": "application/pdf",
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".avif": "image/avif",
	".bmp": "image/bmp", ".ico": "image/x-icon", ".svg": "image/svg+xml",
	".mp4": "video/mp4", ".webm": "video/webm", ".mov": "video/quicktime",
	".mp3": "audio/mpeg", ".wav": "audio/wav", ".ogg": "audio/ogg",
	".m4a": "audio/mp4", ".flac": "audio/flac",
}

// sandboxedTypes 是「可能自带脚本」的类型：inline 下发时必须落进无源沙箱。
var sandboxedTypes = map[string]bool{
	".html": true, ".htm": true, ".svg": true, ".xml": true,
}

func inlineContentType(name string) string {
	return inlineTypes[strings.ToLower(filepath.Ext(name))]
}

// setAttachment 让浏览器走「另存为」。filename* 用 RFC 5987 编码，中文名
// 与空格才不会在下载时变成乱码或被截断。
func setAttachment(w http.ResponseWriter, name string) {
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s",
			name, url.PathEscape(name)))
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

// ---- 文件变动监视流 ----

// 工作区监视流的心跳间隔。与全局事件流同理：空闲长连接要定期有字节流动，
// 否则服务端发现不了对端已经走了，中间层也可能按空闲超时掐断。
const watchHeartbeat = 25 * time.Second

// watch 是工作目录的文件变动流（SSE）。
//
// 面板此前只在 agent 干完一件事之后才重读；用户自己在编辑器里改的、
// 命令行里跑出来的，界面一概不知道。这条流补上那一半：改动来自谁都算数。
//
// 事件只有一种、也不带路径——面板本来就要整片重读（文件树、git 汇总、
// 正在看的那个文件），逐条路径对它们没有用处。合帧在 fswatch 里做完了，
// 这里只管转发。
//
// 这棵树太大不适合监视时（依赖与产物没排干净的巨型目录），回一条
// `unavailable` 就收线，客户端据此不再重连、退回手动刷新——比让它对着
// 一条永远不来事件的流干等强。
func (h workspaceHandler) watch(w http.ResponseWriter, r *http.Request) {
	cwd, err := h.cwdOf(r)
	if err != nil {
		writeError(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, envelope{Error: "streaming unsupported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	var (
		events <-chan struct{}
		cancel func()
	)
	if h.watcher != nil {
		events, cancel, err = h.watcher.Subscribe(cwd)
	} else {
		err = fswatch.ErrUnavailable
	}
	if err != nil {
		if _, err := fmt.Fprint(w, "data: {\"kind\":\"unavailable\"}\n\n"); err == nil {
			flusher.Flush()
		}
		return
	}
	defer cancel()

	ticker := time.NewTicker(watchHeartbeat)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-events:
			if !ok {
				return
			}
			if _, err := fmt.Fprint(w, "data: {\"kind\":\"fs_changed\"}\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			// SSE 注释行：只保活，不触发前端的 onmessage。
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// watcherHub 是 workspaceHandler 依赖的监视器共用层。用接口而不是直接
// 拿 *fswatch.Hub，是为了让「没有监视器」也是一种合法装配（测试、或者
// 将来某个不需要它的部署形态）——handler 不必到处判空。
type watcherHub interface {
	Subscribe(root string) (<-chan struct{}, func(), error)
}

var _ watcherHub = (*fswatch.Hub)(nil)
