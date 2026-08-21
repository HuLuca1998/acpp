package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"acpp/server/internal/model"
	"acpp/server/internal/transcript"
)

// 写一轮完整对话（prompt → chunk → 响应）进转录文件。
func appendTurn(t *testing.T, store *transcript.Store, key string, id int, ask, answer string) {
	t.Helper()
	store.Append(key, "send", []byte(promptFrame(id, ask)))
	store.Append(key, "recv", []byte(chunkFrame(answer)))
	store.Append(key, "recv", []byte(resultFrame(id)))
}

// Messages 的重建结果带缓存（键在转录文件的 size/mtime 上），契约是：
// 转录追加后必须立刻反映新内容——缓存只许省掉重复重建，不许吞掉新数据。
func TestMessagesReflectsTranscriptAppend(t *testing.T) {
	store, err := transcript.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := NewChatService(nil, nil, nil, store, nil)
	const sessionID = 42
	key := sessionKey(sessionID)

	appendTurn(t, store, key, 1, "第一问", "第一答")

	msgs, total, err := svc.Messages(sessionID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || total != 2 {
		t.Fatalf("got %d messages (total %d), want 2", len(msgs), total)
	}

	// 命中缓存的第二次读必须与第一次一致。
	again, total2, err := svc.Messages(sessionID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 2 || total2 != 2 {
		t.Fatalf("cached read: got %d messages (total %d), want 2", len(again), total2)
	}

	appendTurn(t, store, key, 2, "第二问", "第二答")

	msgs, total, err = svc.Messages(sessionID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 4 || total != 4 {
		t.Fatalf("after append: got %d messages (total %d), want 4", len(msgs), total)
	}
	if msgs[3].Content != "第二答" {
		t.Errorf("last message = %q, want 第二答", msgs[3].Content)
	}
}

// 尾部分页：limit 取最新一段，before 以最早 id 为游标向前翻。
func TestMessagesTailPaging(t *testing.T) {
	store, err := transcript.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := NewChatService(nil, nil, nil, store, nil)
	const sessionID = 7
	key := sessionKey(sessionID)

	for i := 1; i <= 3; i++ {
		appendTurn(t, store, key, i, "问", "答")
	}

	tail, total, err := svc.Messages(sessionID, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 2 || total != 6 {
		t.Fatalf("got %d messages (total %d), want 2 (total 6)", len(tail), total)
	}

	earlier, total, err := svc.Messages(sessionID, 2, tail[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(earlier) != 2 || total != 6 {
		t.Fatalf("earlier page: got %d messages (total %d), want 2 (total 6)", len(earlier), total)
	}
	if earlier[1].ID >= tail[0].ID {
		t.Errorf("earlier page overlaps tail: %d >= %d", earlier[1].ID, tail[0].ID)
	}

	// 没有转录的新会话：空列表不报错。
	empty, total, err := svc.Messages(999, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 || total != 0 {
		t.Fatalf("empty session: got %d messages (total %d), want 0", len(empty), total)
	}
}

func toolCallFrame(id, rawInput, rawOutput string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"tool_call","toolCallId":%q,"title":"run","kind":"execute","status":"completed","rawInput":%s,"rawOutput":%s}}}`, id, rawInput, rawOutput)
}

// 超过上限的工具输出下发时截断并打标记；带 sql 的调用（结果表格要
// 完整 JSON 才解析得出来）不截。转录里的原文不受影响。
func TestMessagesTruncatesHugeRawOutput(t *testing.T) {
	store, err := transcript.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := NewChatService(nil, nil, nil, store, nil)
	const sessionID = 5
	key := sessionKey(sessionID)

	huge, err := json.Marshal(strings.Repeat("长输出x", 20_000)) // ~200KB
	if err != nil {
		t.Fatal(err)
	}
	store.Append(key, "send", []byte(promptFrame(1, "跑一下")))
	store.Append(key, "recv", []byte(toolCallFrame("t1", `{"command":"cat big"}`, string(huge))))
	store.Append(key, "recv", []byte(toolCallFrame("t2", `{"sql":"select 1"}`, string(huge))))
	store.Append(key, "recv", []byte(resultFrame(1)))

	msgs, _, err := svc.Messages(sessionID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	var execCall, sqlCall *model.Message
	for i := range msgs {
		if msgs[i].Kind != model.KindToolCall {
			continue
		}
		switch msgs[i].Payload["toolCallId"] {
		case "t1":
			execCall = &msgs[i]
		case "t2":
			sqlCall = &msgs[i]
		}
	}
	if execCall == nil || sqlCall == nil {
		t.Fatalf("tool calls missing from rebuild: %+v", msgs)
	}

	slim, ok := execCall.Payload["rawOutput"].(json.RawMessage)
	if !ok || len(slim) > rawOutputLimit+1024 {
		t.Errorf("exec rawOutput not truncated: %d bytes", len(slim))
	}
	if execCall.Payload["rawOutputTruncated"] != true {
		t.Error("exec call missing rawOutputTruncated marker")
	}
	// 截断后仍须是合法 JSON 字符串。
	var text string
	if err := json.Unmarshal(slim, &text); err != nil {
		t.Errorf("truncated rawOutput is invalid JSON: %v", err)
	}

	if sqlRaw, _ := sqlCall.Payload["rawOutput"].(json.RawMessage); len(sqlRaw) != len(huge) {
		t.Errorf("sql rawOutput must stay intact: got %d, want %d", len(sqlRaw), len(huge))
	}
	if _, marked := sqlCall.Payload["rawOutputTruncated"]; marked {
		t.Error("sql call must not be marked truncated")
	}
}
