package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"acpp/server/internal/model"
)

// 契约：/api/ask 是别的 AI 的同步问答面（adr-022），owner 专属。这里只测不用真起
// agent 就能验的部分——入参校验、agent 按名字寻址、租户拒之门外。
// 真跑一轮的行为靠真机验证（skill 的 ask.sh 双向各跑一遍）。

func TestAsk_RejectsBadInput(t *testing.T) {
	env := newFlowEnv(t)
	cases := []struct {
		name string
		body string
		code int
	}{
		{"缺 prompt", `{"agent":"claude","cwd":"/tmp"}`, http.StatusBadRequest},
		{"新会话缺 agent", `{"cwd":"/tmp","prompt":"hi"}`, http.StatusBadRequest},
		{"新会话缺 cwd", `{"agent":"claude","prompt":"hi"}`, http.StatusBadRequest},
		{"权限档不认识", `{"agent":"claude","cwd":"/tmp","prompt":"hi","level":"yolo"}`, http.StatusBadRequest},
		{"agent 不存在", `{"agent":"gemini","cwd":"/tmp","prompt":"hi"}`, http.StatusNotFound},
		{"未知字段", `{"agent":"claude","cwd":"/tmp","prompt":"hi","model":"x"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.as(t, nil, http.MethodPost, "/api/ask", tc.body)
			if rec.Code != tc.code {
				t.Fatalf("POST /api/ask = %d, want %d: %s", rec.Code, tc.code, rec.Body.String())
			}
		})
	}
}

func TestAsk_OwnerOnly(t *testing.T) {
	env := newFlowEnv(t)
	// owner 先开一条会话。
	rec := env.as(t, nil, http.MethodPost, "/api/sessions", `{"agentId":1,"cwd":"/tmp"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("owner create session = %d: %s", rec.Code, rec.Body.String())
	}
	sess := decodeData[model.Session](t, rec)
	if sess.Origin != "" {
		t.Fatalf("界面建的会话不该带来源标记，得到 %q", sess.Origin)
	}

	// 整个面 owner 专属：租户连门都进不了（403），更别说拿 owner 的 thread 续聊。
	body := fmt.Sprintf(`{"thread":%d,"prompt":"hi"}`, sess.ID)
	rec = env.as(t, env.alice, http.MethodPost, "/api/ask", body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("alice POST /api/ask = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	rec = env.as(t, env.alice, http.MethodPost, "/api/ask", `{"agent":"claude","cwd":"/tmp","prompt":"hi"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("alice new ask = %d, want 403", rec.Code)
	}
	// 请求体里自己声明 origin 也不认（字段未知即 400）。
	rec = env.as(t, nil, http.MethodPost, "/api/sessions", `{"agentId":1,"cwd":"/tmp","origin":"ask"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("session create with origin = %d, want 400", rec.Code)
	}
}
