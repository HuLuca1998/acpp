package service

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
	"acpp/server/internal/stream"
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

// @ 数据库引用只以 resource 块进 prompt（正文之前），**不允许**出现裸
// text 块——重建会把所有 text 块拼成用户正文，注入的说明会显示成用户
// 自己说的话（历史教训见 rebuild.go 的 legacyDBGuidance）。
func TestAppendDBReferences(t *testing.T) {
	base := []acp.ContentBlock{acp.TextBlock("这两张表什么关系？")}

	t.Run("引用是 resource 块且正文原样殿后", func(t *testing.T) {
		blocks, payload := AppendDBReferences(base, nil, []DBReference{
			{URI: "mysql://pp-game/local/users", Text: "用户引用了数据源……"},
		}, true)

		if len(blocks) != 2 {
			t.Fatalf("blocks = %d 个，期望 2（引用 + 正文）", len(blocks))
		}
		if blocks[0].Type != "resource" {
			t.Errorf("第一块应是引用内容，实际 %+v", blocks[0])
		}
		if blocks[1].Type != "text" || blocks[1].Text != "这两张表什么关系？" {
			t.Errorf("正文必须原样留在最后，实际 %+v", blocks[1])
		}
		for _, b := range blocks[:1] {
			if b.Type == "text" {
				t.Errorf("引用不许产生裸 text 块，实际 %+v", b)
			}
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

func TestAppendServerReferences(t *testing.T) {
	base := []acp.ContentBlock{acp.TextBlock("这台机器磁盘还剩多少？")}

	t.Run("引用是 resource 块且正文原样殿后", func(t *testing.T) {
		blocks, payload := AppendServerReferences(base, nil, []ServerReference{
			{URI: ServerRefScheme + "pp-game-live", Text: "用户引用了服务器……"},
		}, true)

		if len(blocks) != 2 {
			t.Fatalf("blocks = %d 个，期望 2（引用 + 正文）", len(blocks))
		}
		if blocks[0].Type != "resource" {
			t.Errorf("第一块应是引用内容，实际 %+v", blocks[0])
		}
		if blocks[1].Type != "text" || blocks[1].Text != "这台机器磁盘还剩多少？" {
			t.Errorf("正文必须原样留在最后，实际 %+v", blocks[1])
		}
		// **裸 text 块是这里最要命的错**：转录重建会把所有 text 块拼进用户
		// 正文，注入的说明就成了「用户气泡里冒出系统文案」。
		if blocks[0].Type == "text" {
			t.Errorf("引用不许产生裸 text 块，实际 %+v", blocks[0])
		}
		if uris, _ := payload["servers"].([]string); len(uris) != 1 {
			t.Errorf("servers = %v，期望记下被引用的 URI", payload["servers"])
		}
	})

	t.Run("没引用时不加任何东西", func(t *testing.T) {
		blocks, payload := AppendServerReferences(base, nil, nil, true)
		if len(blocks) != 1 || payload != nil {
			t.Fatalf("blocks = %+v payload = %v，期望原样返回", blocks, payload)
		}
	})

	t.Run("与数据库引用同时存在时两种 payload 都在", func(t *testing.T) {
		blocks, payload := AppendDBReferences(base, nil, []DBReference{
			{URI: "mysql://pp-game/pre/pp_game", Text: "库"},
		}, true)
		blocks, payload = AppendServerReferences(blocks, payload, []ServerReference{
			{URI: ServerRefScheme + "box", Text: "机器"},
		}, true)

		if len(blocks) != 3 {
			t.Fatalf("blocks = %d，期望 3（两个引用 + 正文）", len(blocks))
		}
		if blocks[len(blocks)-1].Type != "text" {
			t.Errorf("正文仍要殿后，实际 %+v", blocks[len(blocks)-1])
		}
		if len(payload["datasources"].([]string)) != 1 || len(payload["servers"].([]string)) != 1 {
			t.Errorf("两种引用的 payload 都要在: %+v", payload)
		}
	})
}

// agentTitleFixture 建一条「标题还是首句派生值」的会话现场——
// 这正是 agent 推标题时的真实状态。
func agentTitleFixture(t *testing.T, title string) (*ChatService, uint, *stream.Broker) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "titles.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.Agent{}, &model.Session{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	agent := model.Agent{Name: "claude", Command: "claude-agent-acp"}
	if err := gdb.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}
	session := model.Session{AgentID: agent.ID, Title: title, State: "active"}
	if err := gdb.Create(&session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	svc := NewChatService(gdb, NewSessionService(gdb), nil, nil, nil)
	return svc, session.ID, stream.NewBroker()
}

// titleOf 读会话在库里的现值。
func titleOf(t *testing.T, s *ChatService, sessionID uint) string {
	t.Helper()
	var got string
	if err := s.db.Model(&model.Session{}).Where("id = ?", sessionID).
		Pluck("title", &got).Error; err != nil {
		t.Fatalf("read title: %v", err)
	}
	return got
}

// agent 推来的标题要不要收，取决于它比首句派生值多带了信息。
//
// 这是两端行为差异的收口处：claude 推 AI 概括（收），codex 把首条消息原文
// 抄回来（不收，留给 titler 兜底）。
func TestAdoptAgentTitle(t *testing.T) {
	const prompt = "https://github.com/BDBGAME2024/pp-game/tree/live  有更新，将我本地的代码更新到和远端一致"
	derived := DeriveTitle(prompt) // "https://github.…"

	t.Run("claude 的 AI 概括落库并广播", func(t *testing.T) {
		svc, id, br := agentTitleFixture(t, derived)
		svc.rememberDerivedTitle(id, derived)
		events, unsub := br.Subscribe()
		defer unsub()

		svc.adoptAgentTitle(id, br, "更新本地代码与远端保持一致")

		if got := titleOf(t, svc, id); got != "更新本地代码与远端保持一致" {
			t.Errorf("库里标题 = %q，期望换成 agent 推来的概括", got)
		}
		select {
		case ev := <-events:
			if ev.Kind != "session_title" || ev.Title != "更新本地代码与远端保持一致" {
				t.Errorf("事件 = %+v，期望 session_title 带新标题", ev)
			}
		default:
			t.Error("没广播 session_title，侧边栏不会刷新")
		}
		// 记账已用掉：后面几轮不该再尝试覆盖。
		if svc.takeDerivedTitle(id) != "" {
			t.Error("采用后派生记录应清掉")
		}
	})

	t.Run("codex 抄回原文时不收，留给 titler", func(t *testing.T) {
		svc, id, br := agentTitleFixture(t, derived)
		svc.rememberDerivedTitle(id, derived)

		svc.adoptAgentTitle(id, br, prompt)

		if got := titleOf(t, svc, id); got != derived {
			t.Errorf("库里标题 = %q，期望仍是首句派生值", got)
		}
		// 派生记录必须还在，否则本轮末 titler 升级完标题也发不出通知。
		if svc.takeDerivedTitle(id) != derived {
			t.Error("拒收后派生记录应保留")
		}
	})

	t.Run("没有派生记录时一概不动", func(t *testing.T) {
		// 进程重启后的旧会话：无从判断当前标题是派生值还是人手改的，
		// 宁可不换。
		svc, id, br := agentTitleFixture(t, "我自己起的名字")

		svc.adoptAgentTitle(id, br, "AI 起的名字")

		if got := titleOf(t, svc, id); got != "我自己起的名字" {
			t.Errorf("库里标题 = %q，期望不动", got)
		}
	})

	t.Run("用户中途改了名就不覆盖", func(t *testing.T) {
		svc, id, br := agentTitleFixture(t, derived)
		svc.rememberDerivedTitle(id, derived)
		// agent 推标题与用户改名同期发生：改名先落库。
		if err := svc.db.Model(&model.Session{}).Where("id = ?", id).
			Update("title", "我自己起的名字").Error; err != nil {
			t.Fatalf("rename: %v", err)
		}

		svc.adoptAgentTitle(id, br, "AI 起的名字")

		if got := titleOf(t, svc, id); got != "我自己起的名字" {
			t.Errorf("库里标题 = %q，期望保住用户改的名", got)
		}
	})

	t.Run("空标题直接忽略", func(t *testing.T) {
		svc, id, br := agentTitleFixture(t, derived)
		svc.rememberDerivedTitle(id, derived)

		svc.adoptAgentTitle(id, br, "   ")

		if got := titleOf(t, svc, id); got != derived {
			t.Errorf("库里标题 = %q，期望不动", got)
		}
	})
}
