package httpapi

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 压缩中间件的契约。三条边界（SSE / Range / 小响应）比「能压」本身更要紧：
// 压错一条就是实时性没了或字节偏移全乱，而那两种故障在界面上都表现成
// 「偶尔坏一下」，很难回溯到这里。

func gzipRequest(t *testing.T, h http.Handler, method, target string, headers map[string]string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	withCompression(h).ServeHTTP(rec, req)
	return rec.Result()
}

func TestCompressionShrinksLargeJSON(t *testing.T) {
	body := `{"items":[` + strings.Repeat(`{"kind":"tool_call","status":"completed"},`, 200) + `{}]}`
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, body)
	})

	res := gzipRequest(t, h, http.MethodGet, "/api/sessions/1/messages", nil)
	if got := res.Header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := res.Header.Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Errorf("Vary = %q, want it to mention Accept-Encoding", got)
	}
	if res.Header.Get("Content-Length") != "" {
		t.Error("Content-Length 必须去掉：它是压缩前的长度，客户端会读不满就断开")
	}

	zr, err := gzip.NewReader(res.Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	got, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(got) != body {
		t.Fatal("解压后的内容与原文不一致")
	}
}

func TestCompressionSkipsEventStream(t *testing.T) {
	// SSE：Content-Type 定了之后逐条 flush，中间件不能攒也不能压。
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for range 300 {
			_, _ = io.WriteString(w, "data: {\"kind\":\"message_chunk\",\"text\":\"hello\"}\n\n")
			w.(http.Flusher).Flush()
		}
	})

	res := gzipRequest(t, h, http.MethodGet, "/api/sessions/1/events", nil)
	if got := res.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("SSE 被压了（Content-Encoding=%q）：分片会攒在压缩窗口里，首个 token 迟到", got)
	}
	body, _ := io.ReadAll(res.Body)
	if !bytes.HasPrefix(body, []byte("data: ")) {
		t.Fatal("SSE 正文应原样直通")
	}
}

func TestCompressionSkipsRangeRequest(t *testing.T) {
	// 日志面板每 2 秒带 Range 尾随读转录：压缩后的字节数与原文对不上，
	// 前端按字节推进的偏移就全错了。
	payload := strings.Repeat("{\"dir\":\"recv\"}\n", 500)
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, payload)
	})

	res := gzipRequest(t, h, http.MethodGet, "/api/sessions/1/transcript",
		map[string]string{"Range": "bytes=100-"})
	if got := res.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("Range 请求被压了（Content-Encoding=%q）：字节偏移会全乱", got)
	}
	body, _ := io.ReadAll(res.Body)
	if string(body) != payload {
		t.Fatal("Range 响应应原样直通")
	}
}

func TestCompressionSkipsTinyAndBinary(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        string
	}{
		// 健康检查这类小响应压完反而更大，还多一次分配。
		{"tiny json", "application/json", `{"status":"ok"}`},
		// 下载走原样字节（zip/图片/字体），压了白费 CPU。
		{"binary", "application/zip", strings.Repeat("\x00\x01\x02\x03", 2000)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				_, _ = io.WriteString(w, tc.body)
			})
			res := gzipRequest(t, h, http.MethodGet, "/api/x", nil)
			if got := res.Header.Get("Content-Encoding"); got != "" {
				t.Fatalf("Content-Encoding = %q, want 空", got)
			}
			body, _ := io.ReadAll(res.Body)
			if string(body) != tc.body {
				t.Fatal("正文应原样直通")
			}
		})
	}
}

func TestCompressionSkippedWithoutAcceptEncoding(t *testing.T) {
	body := strings.Repeat("text ", 1000)
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, body)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	// 刻意只声明 br：客户端不认 gzip 时发 gzip 等于发一堆乱码。
	req.Header.Set("Accept-Encoding", "br")
	rec := httptest.NewRecorder()
	withCompression(h).ServeHTTP(rec, req)

	res := rec.Result()
	if got := res.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want 空", got)
	}
	got, _ := io.ReadAll(res.Body)
	if string(got) != body {
		t.Fatal("正文应原样直通")
	}
}

func TestCompressionPreservesStatusCode(t *testing.T) {
	// 错误响应也要压（它们可能带一长串 git 原话），但状态码不能被吃掉。
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"`+strings.Repeat("session 1: not found. ", 100)+`"}`)
	})

	res := gzipRequest(t, h, http.MethodGet, "/api/sessions/1", nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", res.StatusCode)
	}
	if got := res.Header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
}

func TestCompressionKeepsNoContent(t *testing.T) {
	// 转录尾随读「没有新内容」走 204：不能有响应体，也不该带编码头。
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	res := gzipRequest(t, h, http.MethodGet, "/api/sessions/1/transcript", nil)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", res.StatusCode)
	}
	if got := res.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("204 不该带 Content-Encoding，得到 %q", got)
	}
}
