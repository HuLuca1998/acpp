package service

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"acpp/server/internal/model"
	"acpp/server/internal/transcript"
)

// wire 拼一条转录条目；msg 直接给 JSON 文本。
func wire(t *testing.T, dir, msg string) transcript.Entry {
	t.Helper()
	if !json.Valid([]byte(msg)) {
		t.Fatalf("invalid wire json: %s", msg)
	}
	return transcript.Entry{TS: time.Now(), Dir: dir, Msg: json.RawMessage(msg)}
}

func promptFrame(id int, text string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"session/prompt","params":{"sessionId":"s","prompt":[{"type":"text","text":%q}]}}`, id, text)
}

func chunkFrame(text string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":%q}}}}`, text)
}

func resultFrame(id int) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"stopReason":"end_turn"}}`, id)
}

// steering（插话注入当前轮）的用户消息必须重建成一条 user 消息，
// 且不打断当前轮——agent 正文仍在轮末统一落成一条消息。
func TestRebuildMessagesSteering(t *testing.T) {
	entries := []transcript.Entry{
		wire(t, "send", promptFrame(1, "第一问")),
		wire(t, "recv", chunkFrame("回答前半。")),
		wire(t, "send", `{"jsonrpc":"2.0","id":2,"method":"_session/steering","params":{"sessionId":"s","prompt":[{"type":"text","text":"插话内容"}]}}`),
		wire(t, "recv", chunkFrame("回答后半。")),
		wire(t, "recv", resultFrame(1)),
	}

	got := RebuildMessages(7, entries)

	want := []struct {
		role    model.MessageRole
		content string
	}{
		{model.RoleUser, "第一问"},
		{model.RoleUser, "插话内容"},
		{model.RoleAgent, "回答前半。回答后半。"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d messages, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Role != w.role || got[i].Content != w.content {
			t.Errorf("message[%d] = (%s, %q), want (%s, %q)", i, got[i].Role, got[i].Content, w.role, w.content)
		}
	}
}

// claude 的 promptQueueing（引导插话）：turn 中再发的 prompt 排队为独立轮。
// 排队 prompt 只产出 user 消息不打断原轮；原轮响应后排队轮的分片
// 必须落进新轮，不能因 turn 为空被丢弃。
func TestRebuildMessagesPromptQueueing(t *testing.T) {
	entries := []transcript.Entry{
		wire(t, "send", promptFrame(8, "长任务")),
		wire(t, "recv", chunkFrame("原轮前半。")),
		wire(t, "send", promptFrame(9, "引导插话 J")),
		wire(t, "recv", chunkFrame("原轮后半。")),
		wire(t, "recv", resultFrame(8)),
		wire(t, "recv", chunkFrame("这是对 J 的答复。")),
		wire(t, "recv", resultFrame(9)),
	}

	got := RebuildMessages(7, entries)

	want := []struct {
		role    model.MessageRole
		content string
	}{
		{model.RoleUser, "长任务"},
		{model.RoleUser, "引导插话 J"},
		{model.RoleAgent, "原轮前半。原轮后半。"},
		{model.RoleAgent, "这是对 J 的答复。"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d messages, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Role != w.role || got[i].Content != w.content {
			t.Errorf("message[%d] = (%s, %q), want (%s, %q)", i, got[i].Role, got[i].Content, w.role, w.content)
		}
	}
}

// 普通两轮对话的基线：每轮 prompt 产出 user 消息，轮末落 agent 正文。
func TestRebuildMessagesTwoTurns(t *testing.T) {
	entries := []transcript.Entry{
		wire(t, "send", promptFrame(1, "你好")),
		wire(t, "recv", chunkFrame("嗨。")),
		wire(t, "recv", resultFrame(1)),
		wire(t, "send", promptFrame(2, "再见")),
		wire(t, "recv", chunkFrame("回见。")),
		wire(t, "recv", resultFrame(2)),
	}

	got := RebuildMessages(7, entries)
	if len(got) != 4 {
		t.Fatalf("got %d messages, want 4: %+v", len(got), got)
	}
	if got[1].Content != "嗨。" || got[3].Content != "回见。" {
		t.Errorf("agent contents = %q, %q", got[1].Content, got[3].Content)
	}
}
