package acp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// 权限裁决与交互式提问都阻塞在真人身上，一分钟根本不够：实测局域网访客
// 常常隔几分钟才看到卡片，而超时会被当成 cancelled 回给 agent——那一步
// 工具调用随即失败，用户回来只看到「被中止了」，卡片早已不在。
func TestReverseTimeoutWaitsForHumans(t *testing.T) {
	cases := []struct {
		method string
		want   time.Duration
	}{
		{"session/request_permission", humanCallTimeout},
		{"elicitation/create", humanCallTimeout},
		{"fs/read_text_file", reverseCallTimeout},
		{"fs/write_text_file", reverseCallTimeout},
	}
	for _, c := range cases {
		if got := reverseTimeout(c.method); got != c.want {
			t.Errorf("reverseTimeout(%q) = %v，期望 %v", c.method, got, c.want)
		}
	}
	if humanCallTimeout <= reverseCallTimeout {
		t.Errorf("等真人的时限 %v 不该短于机器应答的 %v", humanCallTimeout, reverseCallTimeout)
	}
}

// 契约：agent 报错时，data 里的细节要跟着错误文本一起上浮。
//
// 实测形态（claude-agent-acp 0.79）：message 是一句没有信息量的
// "Internal error"，真正说明白问题的在 data.details 里。只把 message 交给
// 上层，用户在界面上看到的就是「内部错误」，既不知道是认证、模型还是配置
// 的问题，也无从下手。
func TestRPCError_Error_CarriesDataDetails(t *testing.T) {
	err := &rpcError{
		Code:    -32603,
		Message: "Internal error",
		Data:    json.RawMessage(`{"details":"Unable to validate model: Could not resolve authentication method."}`),
	}

	got := err.Error()

	if !strings.Contains(got, "Internal error") {
		t.Fatalf("error text lost the message: %q", got)
	}
	if !strings.Contains(got, "Unable to validate model") {
		t.Fatalf("error text lost data.details: %q", got)
	}
}

// 契约：认不出形状的 data 也要带上——一句陌生的 JSON 仍然比「内部错误」
// 有用，而且能让人把它贴给我们。
func TestRPCError_Error_CarriesUnknownDataShape(t *testing.T) {
	err := &rpcError{Code: -32000, Message: "Auth required", Data: json.RawMessage(`["login first"]`)}

	got := err.Error()

	if !strings.Contains(got, "login first") {
		t.Fatalf("error text lost unknown-shaped data: %q", got)
	}
}

// 契约：没有 data 时错误文本保持原样，不多出冒号之类的噪音。
func TestRPCError_Error_WithoutDataStaysClean(t *testing.T) {
	err := &rpcError{Code: -32601, Message: "Method not found"}

	if got := err.Error(); got != "Method not found" {
		t.Fatalf("error text = %q, want the bare message", got)
	}
}
