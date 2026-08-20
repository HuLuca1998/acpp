package acp

import "testing"

// prompt 内容能力的提取：已知方言按实测兜底（image/embeddedContext 恒真，
// 声明漏了不误关既有功能），generic 严格按声明、不猜。
func TestSettingsPromptCapabilities(t *testing.T) {
	declared := PromptCapabilities{Image: false, Audio: true, EmbeddedContext: false}
	caps := Caps{Prompt: declared}

	tests := []struct {
		name    string
		adapter Adapter
		want    PromptCapabilities
	}{
		{"claude 兜底", claudeAdapter{}, PromptCapabilities{Image: true, Audio: true, EmbeddedContext: true}},
		{"codex 兜底", codexAdapter{}, PromptCapabilities{Image: true, Audio: true, EmbeddedContext: true}},
		{"generic 按声明", genericAdapter{}, declared},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.adapter.Settings(caps).Prompt; got != tt.want {
				t.Errorf("Prompt = %+v，期望 %+v", got, tt.want)
			}
		})
	}
}

// Caps 深拷贝必须带上 Prompt——它在握手时写入一次，之后不再有来源。
func TestCapsClonePrompt(t *testing.T) {
	c := Caps{Prompt: PromptCapabilities{Image: true}}
	if got := c.clone().Prompt; !got.Image {
		t.Errorf("clone 丢了 Prompt：%+v", got)
	}
}
