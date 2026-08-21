package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

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

	// 目录打包：边写边发，大目录不占内存。头必须在写第一个字节前发完。
	if r.URL.Query().Get("archive") == "1" {
		name := filepath.Base(filepath.Clean(path))
		if name == "." || name == string(filepath.Separator) {
			name = "workspace"
		}
		w.Header().Set("Content-Type", "application/zip")
		setAttachment(w, name+".zip")
		if _, err := service.WorkspaceZip(cwd, path, w); err != nil {
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
