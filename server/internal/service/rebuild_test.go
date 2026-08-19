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

// toolFrame 拼一条 tool_call/tool_call_update 的线级帧，meta 直接给 _meta 的 JSON。
func toolFrame(t *testing.T, kind, id, body, meta string) string {
	t.Helper()
	m := ""
	if meta != "" {
		m = `,"_meta":` + meta
	}
	return fmt.Sprintf(
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":%q,"toolCallId":%q%s%s}}}`,
		kind, id, body, m)
}

// 子代理归属必须落进重建出的 payload，历史会话才能还原出子代理列表。
// 关键在最后一帧：agent 实测会在后续 update 里把 subagent / parentToolUseId
// 标记一起漏掉，重建器绝不能让这种空值把已知归属冲掉。
func TestRebuildMessagesSubagentAttribution(t *testing.T) {
	entries := []transcript.Entry{
		wire(t, "send", promptFrame(1, "派个子代理")),
		// 启动子代理的 Agent 调用。
		wire(t, "recv", toolFrame(t, "tool_call", "toolu_agent",
			`,"title":"Task","status":"pending"`,
			`{"claudeCode":{"toolName":"Agent","subagent":true}}`)),
		// 子代理干活时的工具调用，认领到上面那次调用。
		wire(t, "recv", toolFrame(t, "tool_call", "toolu_bash",
			`,"title":"ls","status":"pending"`,
			`{"claudeCode":{"toolName":"Bash","parentToolUseId":"toolu_agent"}}`)),
		// 收尾帧漏带了归属标记（agent 的真实行为）。
		wire(t, "recv", toolFrame(t, "tool_call_update", "toolu_bash",
			`,"status":"completed"`, `{"claudeCode":{"toolName":"Bash"}}`)),
		wire(t, "recv", toolFrame(t, "tool_call_update", "toolu_agent",
			`,"status":"completed","rawOutput":"干完了"`, `{"claudeCode":{"toolName":"Agent"}}`)),
		wire(t, "recv", resultFrame(1)),
	}
	msgs := RebuildMessages(1, entries)

	byID := map[string]model.JSONMap{}
	for _, m := range msgs {
		if m.Kind != model.KindToolCall {
			continue
		}
		id, _ := m.Payload["toolCallId"].(string)
		byID[id] = m.Payload
	}
	if len(byID) != 2 {
		t.Fatalf("期望重建出 2 条工具调用，实际 %d 条", len(byID))
	}
	if got := byID["toolu_agent"]["isSubagent"]; got != true {
		t.Errorf("Agent 调用的 isSubagent = %v，期望 true", got)
	}
	if got := byID["toolu_bash"]["subagentOf"]; got != "toolu_agent" {
		t.Errorf("子代理工具调用的 subagentOf = %v，期望 toolu_agent（收尾帧的空值不能冲掉归属）", got)
	}
	if got := byID["toolu_agent"]["status"]; got != "completed" {
		t.Errorf("Agent 调用状态 = %v，期望 completed", got)
	}
}

// codex 的子代理是独立 thread：活动事件必须把 threadId 留在 payload 里，
// 否则界面拿不到转录（codex 侧只能靠它 session/load）。
func TestRebuildMessagesCodexSubagentThread(t *testing.T) {
	entries := []transcript.Entry{
		wire(t, "send", promptFrame(1, "起个子代理")),
		wire(t, "recv", toolFrame(t, "tool_call", "item_7",
			`,"title":"Start subagent project_inventory","status":"in_progress"`,
			`{"codex":{"subagent":{"threadId":"01a01803-8e03","path":"/root/project_inventory","activity":"started"}}}`)),
		wire(t, "recv", resultFrame(1)),
	}
	msgs := RebuildMessages(1, entries)
	var payload model.JSONMap
	for _, m := range msgs {
		if m.Kind == model.KindToolCall {
			payload = m.Payload
		}
	}
	if payload == nil {
		t.Fatal("没有重建出工具调用")
	}
	if got := payload["isSubagent"]; got != true {
		t.Errorf("isSubagent = %v，期望 true", got)
	}
	if got := payload["subagentThreadId"]; got != "01a01803-8e03" {
		t.Errorf("subagentThreadId = %v，期望 01a01803-8e03", got)
	}
	if got := payload["subagentPath"]; got != "/root/project_inventory" {
		t.Errorf("subagentPath = %v，期望 /root/project_inventory", got)
	}
}

// agent 的节奏是「说一句 → 干点活 → 再说一句」。正文被工具调用打断的地方
// 就是消息的断点——揉成一条会让两次发言首尾相接，读起来像一句话说到一半
// 突然换了话题（真实样本：派子代理前后各说一句）。
func TestRebuildMessagesSplitsTextAroundTools(t *testing.T) {
	entries := []transcript.Entry{
		wire(t, "send", promptFrame(1, "读一下文件")),
		wire(t, "recv", chunkFrame("我先派个子代理去读。")),
		wire(t, "recv", toolFrame(t, "tool_call", "t1",
			`,"title":"Task","status":"pending"`, "")),
		wire(t, "recv", toolFrame(t, "tool_call_update", "t1", `,"status":"completed"`, "")),
		wire(t, "recv", chunkFrame("子代理回来了，结果如下。")),
		wire(t, "recv", resultFrame(1)),
	}
	msgs := RebuildMessages(1, entries)

	var got []string
	for _, m := range msgs {
		got = append(got, string(m.Kind)+":"+m.Content)
	}
	want := []string{
		"text:读一下文件",
		"text:我先派个子代理去读。",
		"tool_call:Task",
		"text:子代理回来了，结果如下。",
	}
	if len(got) != len(want) {
		t.Fatalf("消息序列 = %v，期望 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 条 = %q，期望 %q", i, got[i], want[i])
		}
	}
}

// 同一段里的连续分片仍然合并——断点只由工具调用产生，不是每个 chunk 一条。
func TestRebuildMessagesMergesAdjacentChunks(t *testing.T) {
	entries := []transcript.Entry{
		wire(t, "send", promptFrame(1, "你好")),
		wire(t, "recv", chunkFrame("前半句")),
		wire(t, "recv", chunkFrame("后半句。")),
		wire(t, "recv", resultFrame(1)),
	}
	msgs := RebuildMessages(1, entries)
	var texts []string
	for _, m := range msgs {
		if m.Role == model.RoleAgent && m.Kind == model.KindText {
			texts = append(texts, m.Content)
		}
	}
	if len(texts) != 1 || texts[0] != "前半句后半句。" {
		t.Errorf("agent 正文 = %v，期望合并成一条「前半句后半句。」", texts)
	}
}
