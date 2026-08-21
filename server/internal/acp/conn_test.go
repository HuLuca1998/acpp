package acp

import (
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
