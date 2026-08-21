package service

import (
	"strings"
	"testing"

	"acpp/server/internal/transcript"
)

// 索引的核心契约：每条用户提问一格，锚点 id 与消息列表同源——界面拿它
// 直接滚到对应消息，对不上索引就是死的。agent 的回答不进索引。
func TestOutlineAnchorsMatchMessages(t *testing.T) {
	store, err := transcript.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := NewChatService(nil, nil, nil, store, nil)
	const sessionID = 11
	key := sessionKey(sessionID)

	appendTurn(t, store, key, 1, "第一问", "第一答")
	appendTurn(t, store, key, 2, "第二问", "第二答")

	outline, err := svc.Outline(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(outline.Items) != 2 {
		t.Fatalf("got %d entries, want 2", len(outline.Items))
	}

	msgs, _, err := svc.Messages(sessionID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[uint]string, len(msgs))
	for _, m := range msgs {
		byID[m.ID] = m.Content
	}
	for i, entry := range outline.Items {
		content, ok := byID[entry.MessageID]
		if !ok {
			t.Fatalf("entry %d: 锚点 %d 在消息列表里不存在", i, entry.MessageID)
		}
		if content != entry.Text {
			t.Errorf("entry %d: text = %q, want %q", i, entry.Text, content)
		}
	}
}

// 没有转录的新会话：空索引不报错，契约与消息列表一致。
func TestOutlineEmptySession(t *testing.T) {
	store, err := transcript.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := NewChatService(nil, nil, nil, store, nil)

	outline, err := svc.Outline(404)
	if err != nil {
		t.Fatal(err)
	}
	if len(outline.Items) != 0 || outline.Pending != 0 {
		t.Fatalf("got %d entries (pending %d), want 0", len(outline.Items), outline.Pending)
	}
}

// 没装外部模型时，长提问回落到首行截断且不标记 Digested——索引必须在
// ollama 不可用时照常能用。Pending 如实报有多少条还等着精简。
func TestOutlineFallsBackWithoutTitler(t *testing.T) {
	store, err := transcript.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := NewChatService(nil, nil, nil, store, nil)
	const sessionID = 12
	key := sessionKey(sessionID)

	// 首行是那句人话，后面跟着一大段贴进来的日志。
	long := "帮我看看这个报错怎么修\n" + strings.Repeat("panic: runtime error 堆栈行\n", 20)
	appendTurn(t, store, key, 1, long, "好的")

	outline, err := svc.Outline(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(outline.Items) != 1 {
		t.Fatalf("got %d entries, want 1", len(outline.Items))
	}
	entry := outline.Items[0]
	if entry.Digested {
		t.Error("没有 titler 却标成了模型摘要")
	}
	if entry.Text != "帮我看看这个报错怎么修" {
		t.Errorf("text = %q, want 首个非空行", entry.Text)
	}
	if outline.Pending != 1 {
		t.Errorf("pending = %d, want 1", outline.Pending)
	}
}

// 同一段提问的指纹稳定且与首尾空白无关（缓存靠它命中），不同提问不撞。
func TestPromptFingerprint(t *testing.T) {
	a := PromptFingerprint("修一下登录超时")
	b := PromptFingerprint("  修一下登录超时\n")
	if a != b {
		t.Errorf("空白差异改变了指纹: %s != %s", a, b)
	}
	if a == PromptFingerprint("修一下登录超时的重试") {
		t.Error("不同提问撞了同一个指纹")
	}
}
