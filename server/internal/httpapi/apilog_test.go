package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"acpp/server/internal/apilog"
	"acpp/server/internal/model"
)

func newAPILogService(t *testing.T) *apilog.Service {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "log.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.APILog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return apilog.NewService(gdb)
}

// waitLogs 等异步落库：记录在 goroutine 里写，测试要等它落地。
func waitLogs(t *testing.T, svc *apilog.Service, want int) []model.APILog {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		rows, total, err := svc.List(context.Background(), apilog.Filter{}, 1, 50, "")
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if int(total) == want {
			return rows
		}
		if time.Now().After(deadline) {
			t.Fatalf("want %d logs, got %d", want, total)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// 契约：普通 JSON 请求记全：方法 / 路径 / 查询串 / 状态 / 身份 / 对方 IP / 来源 /
// 请求正文（handler 仍读到完整正文）/ 响应正文 / 凭证头抹掉；SSE 与 /api/logs 不记。
func TestWithAPILog(t *testing.T) {
	svc := newAPILogService(t)
	var seenBody string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/events":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "data: hi\n\n")
		case "/api/logs":
			writeData(w, http.StatusOK, nil)
		default:
			raw, _ := io.ReadAll(r.Body)
			seenBody = string(raw)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"data":{"id":1}}`)
		}
	})
	h := withAPILog(svc, inner)

	do := func(method, target, body string, hdr map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.RemoteAddr = "192.168.1.9:5555"
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}

	rr := do("POST", "/api/sessions?x=1", `{"title":"a"}`, map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer secret",
		"Origin":        "http://localhost:45173",
	})
	if rr.Code != http.StatusCreated || seenBody != `{"title":"a"}` {
		t.Fatalf("handler must see full body and status: code=%d body=%q", rr.Code, seenBody)
	}
	do("GET", "/api/events", "", nil)
	do("GET", "/api/logs?page=1", "", nil)

	rows := waitLogs(t, svc, 1)
	got := rows[0]
	if got.Method != "POST" || got.Path != "/api/sessions" || got.Query != "x=1" || got.Status != 201 {
		t.Fatalf("basic fields: %+v", got)
	}
	if got.RemoteAddr != "192.168.1.9" || got.Origin != "http://localhost:45173" || got.Identity != "anonymous" {
		t.Fatalf("who/where: %+v", got)
	}
	full, err := svc.Get(context.Background(), got.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if full.RequestBody != `{"title":"a"}` || full.ResponseBody != `{"data":{"id":1}}` {
		t.Fatalf("bodies: req=%q resp=%q", full.RequestBody, full.ResponseBody)
	}
	if strings.Contains(full.RequestHeaders, "secret") || !strings.Contains(full.RequestHeaders, `"Authorization":"[redacted]"`) {
		t.Fatalf("credentials must be redacted: %s", full.RequestHeaders)
	}
	if full.RequestSize != int64(len(`{"title":"a"}`)) || full.ResponseSize != int64(len(`{"data":{"id":1}}`)) {
		t.Fatalf("sizes: %d / %d", full.RequestSize, full.ResponseSize)
	}
}
