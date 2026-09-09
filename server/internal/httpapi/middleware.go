package httpapi

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"acpp/server/internal/apilog"
	"acpp/server/internal/model"
)

// statusRecorder 记录实际写出的状态码，供日志中间件使用。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Flush 必须转发下去，否则包装会吃掉 http.Flusher，SSE 就攒着不发了。
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack 同理：包装会吃掉 http.Hijacker，websocket 升级（工作区终端）
// 就会 501。委托给底层连接。
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := r.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("underlying ResponseWriter does not support hijacking")
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"dur", time.Since(start).String(),
		)
	})
}

// withRecover 兜住 handler 里的 panic，避免整个进程退出。
func withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered", "path", r.URL.Path, "panic", rec)
				writeJSON(w, http.StatusInternalServerError, envelope{
					Error: "internal server error",
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// withCORS 只对白名单来源下发跨域头；origins 为空时直接透传。
func withCORS(origins []string, next http.Handler) http.Handler {
	if len(origins) == 0 {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && slices.Contains(origins, origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Vary", "Origin")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

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

// withAPILog 把每个 API 请求记进日志库：方法、路径、身份、请求头与正文、
// 响应状态与正文、耗时。挂在身份中间件之内、压缩之外——身份已经解析好，
// 响应体还是明文。
//
// 三类请求不记：
//   - SSE（响应头是 text/event-stream）：一条连接挂几小时，「耗时」没有意义，
//     正文是无穷的；
//   - WebSocket 升级（工作区终端）：连接被 Hijack 走，中间件看不到之后的事；
//   - 日志页自己的读取（/api/logs）：不然翻一页日志就多一条日志，越翻越多。
//
// 落库放在 goroutine 里：写一条记录几毫秒，不该算进请求的响应时间。
func withAPILog(logs *apilog.Service, next http.Handler) http.Handler {
	if logs == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if skipAPILog(r) {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()

		// 请求正文：只截前 BodyLimit 字节，剩下的原样流给 handler。
		reqBody := &bodyTap{limit: apilog.BodyLimit, textual: apilog.IsTextual(r.Header.Get("Content-Type"))}
		if r.Body != nil {
			r.Body = reqBody.wrap(r.Body)
		}
		rec := &apiLogRecorder{ResponseWriter: w, status: http.StatusOK, limit: apilog.BodyLimit}

		next.ServeHTTP(rec, r)

		if rec.stream {
			return
		}
		entry := model.APILog{
			Method:          r.Method,
			Path:            r.URL.Path,
			Query:           r.URL.RawQuery,
			Status:          rec.status,
			DurationMs:      time.Since(start).Milliseconds(),
			RemoteAddr:      remoteIP(r),
			Origin:          requestOrigin(r),
			UserAgent:       r.UserAgent(),
			Identity:        identityLabel(identityOf(r)),
			RequestHeaders:  apilog.HeadersJSON(r.Header),
			ResponseHeaders: apilog.HeadersJSON(rec.Header()),
			RequestBody:     reqBody.text(),
			RequestSize:     reqBody.size,
			ResponseBody:    rec.text(),
			ResponseSize:    rec.size,
		}
		// 请求的 context 在响应写完后就取消了，落库用独立的。
		go logs.Record(context.Background(), entry)
	})
}

func skipAPILog(r *http.Request) bool {
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return true
	}
	return strings.HasPrefix(r.URL.Path, "/api/logs")
}

// identityLabel 把身份写成人话：owner / 租户名 / anonymous。
func identityLabel(id identity) string {
	switch {
	case id.owner:
		return "owner"
	case id.tenant != nil:
		return id.tenant.Name
	default:
		return "anonymous"
	}
}

// remoteIP 去掉端口只留地址；反代场景下优先信 X-Forwarded-For 的第一跳。
func remoteIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// requestOrigin 是请求发自哪个页面：浏览器跨源/非简单请求带 Origin，同源导航
// 带 Referer；CLI（别的 AI 经 /api/ask）两者都没有，留空。
func requestOrigin(r *http.Request) string {
	if o := r.Header.Get("Origin"); o != "" {
		return o
	}
	return r.Header.Get("Referer")
}

// bodyTap 旁路抄一份请求正文的开头，不影响 handler 读到的内容。
type bodyTap struct {
	limit   int
	textual bool
	buf     bytes.Buffer
	size    int64
}

func (t *bodyTap) wrap(rc io.ReadCloser) io.ReadCloser {
	return &tapReader{ReadCloser: rc, tap: t}
}

func (t *bodyTap) write(p []byte) {
	t.size += int64(len(p))
	if !t.textual {
		return
	}
	if room := t.limit - t.buf.Len(); room > 0 {
		if len(p) > room {
			p = p[:room]
		}
		t.buf.Write(p)
	}
}

func (t *bodyTap) text() string {
	if t.size > int64(t.limit) && t.textual {
		return t.buf.String() + "\n…（已截断）"
	}
	return t.buf.String()
}

type tapReader struct {
	io.ReadCloser
	tap *bodyTap
}

func (r *tapReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		r.tap.write(p[:n])
	}
	return n, err
}

// apiLogRecorder 记状态码并旁路抄一份响应正文的开头。
// Flush / Hijack 必须转发下去，否则 SSE 攒着不发、websocket 升级 501。
type apiLogRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	stream      bool
	textual     bool
	limit       int
	buf         bytes.Buffer
	size        int64
}

func (r *apiLogRecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.wroteHeader = true
		r.status = status
		ct := r.Header().Get("Content-Type")
		r.stream = strings.HasPrefix(strings.ToLower(ct), "text/event-stream")
		r.textual = apilog.IsTextual(ct)
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *apiLogRecorder) Write(p []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	r.size += int64(len(p))
	if r.textual && !r.stream {
		if room := r.limit - r.buf.Len(); room > 0 {
			if len(p) > room {
				r.buf.Write(p[:room])
			} else {
				r.buf.Write(p)
			}
		}
	}
	return r.ResponseWriter.Write(p)
}

func (r *apiLogRecorder) text() string {
	if r.size > int64(r.limit) && r.textual {
		return r.buf.String() + "\n…（已截断）"
	}
	return r.buf.String()
}

func (r *apiLogRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *apiLogRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := r.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("underlying ResponseWriter does not support hijacking")
}
