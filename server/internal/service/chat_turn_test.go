package service

import (
	"errors"
	"testing"

	"acpp/server/internal/acp"
)

// 内容块按 agent 声明的能力收敛：不支持内嵌上下文时 resource 降级为
// text（内容不丢），不支持图片时报错（静默丢用户数据更糟）；
// 全支持时原样返回。
func TestAdaptBlocksToPromptCaps(t *testing.T) {
	blocks := []acp.ContentBlock{
		acp.ResourceBlock("file:///tmp/a.txt", "文件内容"),
		acp.TextBlock("正文"),
	}

	t.Run("全支持原样通过", func(t *testing.T) {
		got, err := adaptBlocksToPromptCaps(blocks, acp.PromptCapabilities{Image: true, EmbeddedContext: true})
		if err != nil || len(got) != 2 || got[0].Type != "resource" {
			t.Errorf("got %+v, err %v，期望原样返回", got, err)
		}
	})

	t.Run("不支持内嵌时降级为 text", func(t *testing.T) {
		got, err := adaptBlocksToPromptCaps(blocks, acp.PromptCapabilities{Image: true})
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if len(got) != 2 || got[0].Type != "text" {
			t.Fatalf("got %+v，期望 resource 降级为 text", got)
		}
		if got[0].Text == "" || got[0].Text == "正文" {
			t.Errorf("降级块应带来源与内容，实际 %q", got[0].Text)
		}
	})

	t.Run("不支持图片时报错", func(t *testing.T) {
		withImage := append([]acp.ContentBlock{acp.ImageBlock("xx", "image/png")}, blocks...)
		_, err := adaptBlocksToPromptCaps(withImage, acp.PromptCapabilities{EmbeddedContext: true})
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("err = %v，期望 ErrInvalid", err)
		}
	})
}
