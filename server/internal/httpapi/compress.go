package httpapi

import (
	"bufio"
	"compress/gzip"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
)

// 响应压缩。
//
// 为什么值得做：这套界面的两个大头都是高度可压缩的文本——前端产物
// （几 MB 的 JS/CSS）与消息列表 JSON（一屏历史几十 KB，工具卡的入出参
// 全是重复结构）。局域网访客（adr-007）走的是真实网络，不压缩等于把
// 打开会话的时间浪费在传输上；本机 WebView 也省下同等的内存拷贝。
//
// 三条不能压的边界，写在最前面，因为压错比不压严重得多：
//   - **SSE**：text/event-stream 靠逐条 flush 保证实时性，套一层 gzip
//     会让分片攒在压缩窗口里，第一个 token 迟到几百毫秒。
//   - **Range 请求**：日志面板每 2 秒带 `bytes=N-` 尾随读转录，媒体预览
//     靠 Range 拖进度条。压缩后的字节数与原文对不上，偏移就全乱了。
//   - **WebSocket 升级**：连接要被 Hijack 走，中间不能有任何缓冲层。

// compressMinBytes 是启用压缩的最小响应体。比这更小的响应，gzip 头尾
// 本身就抵掉了收益，还平白多一次内存分配——健康检查那 68 字节压完更大。
const compressMinBytes = 1400

// compressibleTypes 是值得压的响应类型前缀。白名单而不是黑名单：新增的
// 二进制类型（字体、音视频、zip 下载）默认不压才是安全的默认值。
var compressibleTypes = []string{
	"application/json",
	"application/javascript",
	"application/x-ndjson",
	"application/xml",
	"image/svg+xml",
	"text/",
}

var gzipPool = sync.Pool{
	New: func() any {
		// 用标准默认档（6）而不是 BestSpeed。实测本项目的两类大响应：
		// 175KB 的前端主分片 69KB→60KB（多花 2.5ms，而它带 immutable
		// 缓存，一次更新只付一遍）；54KB 的消息列表 14.2KB→12.8KB
		// （多花 0.25ms）。两边的代价都在噪声里，省下的字节是实的。
		w, _ := gzip.NewWriterLevel(nil, gzip.DefaultCompression)
		return w
	},
}

// withCompression 给可压缩的文本响应套 gzip。
func withCompression(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !acceptsGzip(r) || r.Header.Get("Range") != "" ||
			strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			next.ServeHTTP(w, r)
			return
		}
		cw := &compressWriter{ResponseWriter: w, status: http.StatusOK}
		defer cw.finish()
		next.ServeHTTP(cw, r)
	})
}

func acceptsGzip(r *http.Request) bool {
	for part := range strings.SplitSeq(r.Header.Get("Accept-Encoding"), ",") {
		if name, _, _ := strings.Cut(strings.TrimSpace(part), ";"); name == "gzip" {
			return true
		}
	}
	return false
}

// compressWriter 推迟「压不压」的决定，直到看见响应类型与足够多的字节。
//
// 推迟是必须的：Content-Type 由 handler 在第一次 Write 之前设定，而响应
// 大小要写出来才知道。决定之前的字节攒在 buf 里，决定之后直通。
type compressWriter struct {
	http.ResponseWriter

	status  int
	written bool // 头是否已真正发出
	decided bool
	gz      *gzip.Writer
	buf     []byte
}

func (w *compressWriter) WriteHeader(status int) {
	if w.decided || w.written {
		return
	}
	w.status = status
	// 这些状态码要么没有响应体，要么带着与原文对齐的字节范围，一律直通。
	if status < http.StatusOK || status == http.StatusNoContent ||
		status == http.StatusNotModified || status == http.StatusPartialContent {
		w.passthrough()
	}
}

func (w *compressWriter) Write(p []byte) (int, error) {
	if w.decided {
		if w.gz != nil {
			return w.gz.Write(p)
		}
		return w.ResponseWriter.Write(p)
	}
	w.buf = append(w.buf, p...)
	if len(w.buf) >= compressMinBytes {
		w.decide()
	}
	return len(p), nil
}

// Flush 是实时性的信号：调用方要求这些字节现在就上路（SSE、日志尾随）。
// 还没决定就当场决定——攒着等够 1400 字节正是它想避免的事。
func (w *compressWriter) Flush() {
	if !w.decided {
		w.decide()
	}
	if w.gz != nil {
		_ = w.gz.Flush()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack 转发给底层：工作区终端的 ws 升级要拿走裸连接。
func (w *compressWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("underlying ResponseWriter does not support hijacking")
}

// Unwrap 让 http.ResponseController 找得到底层实现（超时控制等）。
func (w *compressWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// decide 依据响应类型与已攒字节数决定压不压，并把攒下的内容放行。
func (w *compressWriter) decide() {
	if w.decided {
		return
	}
	if !w.compressible() {
		w.passthrough()
		return
	}

	w.decided = true
	header := w.Header()
	header.Set("Content-Encoding", "gzip")
	header.Add("Vary", "Accept-Encoding")
	// 长度是压缩前的，留着会让客户端在读满原始长度前就断开。
	header.Del("Content-Length")
	// 强校验的 ETag 描述的是未压缩实体，压缩后必须弱化，否则条件请求会
	// 把 gzip 内容当成原文来拼。
	if tag := header.Get("ETag"); tag != "" && !strings.HasPrefix(tag, "W/") {
		header.Set("ETag", "W/"+tag)
	}
	w.writeHeaderOnce()

	gz := gzipPool.Get().(*gzip.Writer)
	gz.Reset(w.ResponseWriter)
	w.gz = gz
	if len(w.buf) > 0 {
		_, _ = gz.Write(w.buf)
		w.buf = nil
	}
}

// compressible 判断这条响应值不值得压。
func (w *compressWriter) compressible() bool {
	// 已经有编码了（handler 自己压过）就别再套一层。
	if w.Header().Get("Content-Encoding") != "" {
		return false
	}
	ct := w.Header().Get("Content-Type")
	if ct == "" {
		// 与 net/http 同样的嗅探规则，免得 handler 没设类型就整体不压。
		ct = http.DetectContentType(w.buf)
		w.Header().Set("Content-Type", ct)
	}
	ct = strings.ToLower(ct)
	// SSE 必须逐条上路，压缩会把分片攒进窗口里。
	if strings.HasPrefix(ct, "text/event-stream") {
		return false
	}
	if len(w.buf) < compressMinBytes {
		return false
	}
	for _, prefix := range compressibleTypes {
		if strings.HasPrefix(ct, prefix) {
			return true
		}
	}
	return false
}

// passthrough 放弃压缩，把攒下的字节原样发出。
func (w *compressWriter) passthrough() {
	w.decided = true
	w.writeHeaderOnce()
	if len(w.buf) > 0 {
		_, _ = w.ResponseWriter.Write(w.buf)
		w.buf = nil
	}
}

func (w *compressWriter) writeHeaderOnce() {
	if w.written {
		return
	}
	w.written = true
	w.ResponseWriter.WriteHeader(w.status)
}

// finish 收尾：还没决定的（小响应）直通发出，压缩流关闭并归还。
func (w *compressWriter) finish() {
	if !w.decided {
		w.passthrough()
	}
	if w.gz != nil {
		_ = w.gz.Close()
		gzipPool.Put(w.gz)
		w.gz = nil
	}
}
