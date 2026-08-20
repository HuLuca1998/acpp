package service

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
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

// 大文件 @ 引用不再全文内嵌：超过阈值改发 resource_link（agent 按需读取），
// 小文件维持 resource 内嵌；payload 记录 linkedFiles 子集供芯片标注。
func TestBuildPromptBlocksResourceLink(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small.txt")
	if err := os.WriteFile(small, []byte("短内容"), 0o644); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(dir, "big.log")
	if err := os.WriteFile(big, bytes.Repeat([]byte("x"), resourceLinkThreshold+1), 0o644); err != nil {
		t.Fatal(err)
	}

	blocks, payload, err := BuildPromptBlocks(dir, SendInput{
		Content: "看看这两个文件",
		Files:   []string{small, big},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 3 {
		t.Fatalf("blocks = %d 个，期望 3（resource + resource_link + text）", len(blocks))
	}
	if blocks[0].Type != "resource" || blocks[0].Resource == nil {
		t.Errorf("小文件应内嵌，实际 %+v", blocks[0])
	}
	if blocks[1].Type != "resource_link" || blocks[1].URI != "file://"+big || blocks[1].Size <= resourceLinkThreshold {
		t.Errorf("大文件应发 resource_link，实际 %+v", blocks[1])
	}
	linked, _ := payload["linkedFiles"].([]string)
	if len(linked) != 1 || linked[0] != big {
		t.Errorf("linkedFiles = %v，期望只含大文件", payload["linkedFiles"])
	}
	files, _ := payload["files"].([]string)
	if len(files) != 2 {
		t.Errorf("files = %v，期望两个都在", payload["files"])
	}
}

// 数据库用法约定只跟着 @ 引用走：有引用时插在引用之后、正文之前；
// 没引用时一个字都不加（会话开场不主动提数据库）。
func TestAppendDBReferencesGuidance(t *testing.T) {
	base := []acp.ContentBlock{acp.TextBlock("这两张表什么关系？")}

	t.Run("有引用时给约定", func(t *testing.T) {
		blocks, payload := AppendDBReferences(base, nil, []DBReference{
			{URI: "mysql://pp-game/local/users", Text: "CREATE TABLE users ..."},
		}, true)

		if len(blocks) != 3 {
			t.Fatalf("blocks = %d 个，期望 3（引用 + 约定 + 正文）", len(blocks))
		}
		if blocks[0].Type != "resource" {
			t.Errorf("第一块应是引用内容，实际 %+v", blocks[0])
		}
		if blocks[1].Type != "text" || blocks[1].Text != dbReferenceGuidance {
			t.Errorf("第二块应是用法约定，实际 %+v", blocks[1])
		}
		if blocks[2].Text != "这两张表什么关系？" {
			t.Errorf("正文必须留在最后，实际 %+v", blocks[2])
		}
		if uris, _ := payload["datasources"].([]string); len(uris) != 1 {
			t.Errorf("datasources = %v，期望记下被引用的 URI", payload["datasources"])
		}
	})

	t.Run("没引用时不加任何东西", func(t *testing.T) {
		blocks, payload := AppendDBReferences(base, nil, nil, true)
		if len(blocks) != 1 || payload != nil {
			t.Fatalf("blocks = %+v payload = %v，期望原样返回", blocks, payload)
		}
	})
}
