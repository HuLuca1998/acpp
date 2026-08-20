package service

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"acpp/server/internal/acp"
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

// steering（插话注入当前轮）的用户消息必须重建成一条 user 消息，且落在
// 时间线上自己的位置：插话之前的正文归前一条，之后的另起一条——插话是
// 货真价实的打断。落到轮首（agent 全部内容之前）是错的。
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
		{model.RoleAgent, "回答前半。"},
		{model.RoleUser, "插话内容"},
		{model.RoleAgent, "回答后半。"},
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
		{model.RoleAgent, "原轮前半。"},
		{model.RoleUser, "引导插话 J"},
		{model.RoleAgent, "原轮后半。"},
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

// agent 进程死在 prompt 在途时，重连后 session/load 会重放全部历史。
// 真实事故（会话 64）：僵尸轮不收尾，重放的整段历史被攒进它，下一轮
// 响应到达时把「全部历史 + 新轮」揉成一坨落在同一时刻；pendingPrompts
// 同时泄漏，此后每个 prompt 都被误判成 promptQueueing。重建器必须把
// send session/new|load 当成连接边界：僵尸轮按 cancelled 收尾，重放不
// 产出消息，新轮从零开始。
func TestRebuildMessagesConnRestartBoundary(t *testing.T) {
	entries := []transcript.Entry{
		// 第一段连接：一轮正常，一轮死在途中。
		wire(t, "send", `{"jsonrpc":"2.0","id":1,"method":"session/new","params":{"cwd":"/w"}}`),
		wire(t, "send", promptFrame(2, "第一问")),
		wire(t, "recv", chunkFrame("第一答。")),
		wire(t, "recv", resultFrame(2)),
		wire(t, "send", promptFrame(3, "第二问")),
		wire(t, "recv", chunkFrame("答到一半——")),
		// 进程死了：id=3 的响应永远不来。重连,load 重放历史。
		wire(t, "send", `{"jsonrpc":"2.0","id":1,"method":"session/load","params":{"sessionId":"s","cwd":"/w"}}`),
		wire(t, "recv", chunkFrame("第一答。")),
		wire(t, "recv", chunkFrame("答到一半——")),
		wire(t, "recv", resultFrame(1)),
		// 用户重发,新轮正常收尾。
		wire(t, "send", promptFrame(2, "第二问")),
		wire(t, "recv", chunkFrame("这次答完了。")),
		wire(t, "recv", resultFrame(2)),
	}

	got := RebuildMessages(7, entries)

	want := []struct {
		role    model.MessageRole
		content string
	}{
		{model.RoleUser, "第一问"},
		{model.RoleAgent, "第一答。"},
		{model.RoleUser, "第二问"},
		{model.RoleAgent, "答到一半——"}, // 僵尸轮的内容保留,按 cancelled 收尾
		{model.RoleUser, "第二问"},       // 用户重发是真实动作,照实产出
		{model.RoleAgent, "这次答完了。"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d messages, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Role != w.role || got[i].Content != w.content {
			t.Errorf("message[%d] = (%s, %q), want (%s, %q)", i, got[i].Role, got[i].Content, w.role, w.content)
		}
	}
	// 僵尸轮要带上中断标志,界面才能就地说明这轮为什么断。
	if reason, _ := got[3].Payload["stopReason"].(string); reason != "cancelled" {
		t.Errorf("僵尸轮 stopReason = %v，期望 cancelled", got[3].Payload["stopReason"])
	}
	// load 的应答（新连接 id=1）不能被旧连接的认领表误认成 prompt 收尾。
	if got[5].Payload != nil {
		t.Errorf("正常轮不该带 stopReason payload: %+v", got[5].Payload)
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

// plan 事件每次都是全量，轮末应落一条最终快照（取末次），空计划不产出。
func TestRebuildMessagesPlanSnapshot(t *testing.T) {
	entries := []transcript.Entry{
		wire(t, "send", promptFrame(1, "做个计划")),
		wire(t, "recv", `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"plan","entries":[{"content":"步骤一","status":"in_progress"},{"content":"步骤二","status":"pending"}]}}}`),
		wire(t, "recv", chunkFrame("干活中。")),
		wire(t, "recv", `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"plan","entries":[{"content":"步骤一","status":"completed"},{"content":"步骤二","status":"completed"}]}}}`),
		wire(t, "recv", resultFrame(1)),
	}
	msgs := RebuildMessages(1, entries)

	var plans []model.Message
	for _, m := range msgs {
		if m.Kind == model.KindPlan {
			plans = append(plans, m)
		}
	}
	if len(plans) != 1 {
		t.Fatalf("plan 快照 = %d 条，期望 1 条：%+v", len(plans), msgs)
	}
	raw, err := json.Marshal(plans[0].Payload["entries"])
	if err != nil {
		t.Fatalf("plan payload 序列化失败: %v", err)
	}
	var got []struct {
		Content string `json:"content"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("plan entries 解析失败: %v", err)
	}
	if len(got) != 2 || got[0].Status != "completed" || got[1].Status != "completed" {
		t.Errorf("plan 快照应是末次全量（全部 completed），实际 %+v", got)
	}
}

// 权限请求配对：agent 的 session/request_permission 与我们的答复配对后
// 落成一条裁决记录，带选项名与结果；ExitPlanMode 的计划全文也进 payload。
func TestRebuildMessagesPermissionVerdict(t *testing.T) {
	entries := []transcript.Entry{
		wire(t, "send", promptFrame(1, "删掉临时文件")),
		wire(t, "recv", `{"jsonrpc":"2.0","id":100,"method":"session/request_permission","params":{"sessionId":"s","toolCall":{"toolCallId":"t1","kind":"execute","status":"pending","title":"rm -rf tmp/"},"options":[{"optionId":"allow","name":"允许一次","kind":"allow_once"},{"optionId":"reject","name":"拒绝","kind":"reject_once"}]}}`),
		wire(t, "send", `{"jsonrpc":"2.0","id":100,"result":{"outcome":{"outcome":"selected","optionId":"allow"}}}`),
		wire(t, "recv", chunkFrame("已删除。")),
		wire(t, "recv", resultFrame(1)),
	}
	msgs := RebuildMessages(1, entries)

	var perms []model.Message
	for _, m := range msgs {
		if m.Kind == model.KindPermissionRequest {
			perms = append(perms, m)
		}
	}
	if len(perms) != 1 {
		t.Fatalf("裁决记录 = %d 条，期望 1 条：%+v", len(perms), msgs)
	}
	p := perms[0]
	if p.Content != "rm -rf tmp/" {
		t.Errorf("content = %q，期望工具标题", p.Content)
	}
	if p.Payload["choice"] != "允许一次" || p.Payload["outcome"] != "selected" || p.Payload["toolKind"] != "execute" {
		t.Errorf("payload = %+v，期望带 choice/outcome/toolKind", p.Payload)
	}
}

// 取消的权限请求（cancel 或超时）落 outcome=cancelled，没有 choice。
func TestRebuildMessagesPermissionCancelled(t *testing.T) {
	entries := []transcript.Entry{
		wire(t, "send", promptFrame(1, "问一下")),
		wire(t, "recv", `{"jsonrpc":"2.0","id":101,"method":"session/request_permission","params":{"sessionId":"s","toolCall":{"toolCallId":"t2","kind":"edit","status":"pending"},"options":[{"optionId":"allow","name":"允许","kind":"allow_once"}]}}`),
		wire(t, "send", `{"jsonrpc":"2.0","id":101,"result":{"outcome":{"outcome":"cancelled"}}}`),
		wire(t, "recv", resultFrame(1)),
	}
	msgs := RebuildMessages(1, entries)

	var perm *model.Message
	for i := range msgs {
		if msgs[i].Kind == model.KindPermissionRequest {
			perm = &msgs[i]
		}
	}
	if perm == nil {
		t.Fatalf("没有裁决记录：%+v", msgs)
	}
	if perm.Payload["outcome"] != "cancelled" {
		t.Errorf("outcome = %v，期望 cancelled", perm.Payload["outcome"])
	}
	if _, ok := perm.Payload["choice"]; ok {
		t.Errorf("取消的请求不该有 choice：%+v", perm.Payload)
	}
}

// prompt 响应带 usage 时，本轮计量挂在最后一段正文的 payload 上。
func TestRebuildMessagesTurnUsage(t *testing.T) {
	entries := []transcript.Entry{
		wire(t, "send", promptFrame(1, "算一下")),
		wire(t, "recv", chunkFrame("答案是 42。")),
		wire(t, "send", `{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"sessionId":"s","prompt":[{"type":"text","text":"第二问"}]}}`),
	}
	// 第一轮的响应带 usage。
	entries = append(entries[:2:2],
		wire(t, "recv", `{"jsonrpc":"2.0","id":1,"result":{"stopReason":"end_turn","usage":{"inputTokens":120,"outputTokens":45,"cachedReadTokens":100,"totalTokens":165}}}`),
	)
	msgs := RebuildMessages(1, entries)

	var text *model.Message
	for i := range msgs {
		if msgs[i].Role == model.RoleAgent && msgs[i].Kind == model.KindText {
			text = &msgs[i]
		}
	}
	if text == nil {
		t.Fatalf("没有正文消息：%+v", msgs)
	}
	usage, ok := text.Payload["turnUsage"].(*acp.Usage)
	if !ok || usage.InputTokens != 120 || usage.OutputTokens != 45 {
		t.Errorf("turnUsage = %#v，期望挂上本轮计量", text.Payload["turnUsage"])
	}
}

// 插话是本轮时间线的最后一段（插完 agent 没再说话）时，轮末计量仍要挂在
// agent 正文上——挂到用户自己的气泡上，hover 出来的就是一句用户消息带着
// 本轮 token 数，语义直接错了。
func TestRebuildMessagesTurnUsageSkipsInterjection(t *testing.T) {
	entries := []transcript.Entry{
		wire(t, "send", promptFrame(1, "长任务")),
		wire(t, "recv", chunkFrame("我开始干了。")),
		wire(t, "send", `{"jsonrpc":"2.0","id":2,"method":"_session/steering","params":{"sessionId":"s","prompt":[{"type":"text","text":"顺手把 b 也做了"}]}}`),
		wire(t, "recv", `{"jsonrpc":"2.0","id":1,"result":{"stopReason":"end_turn","usage":{"inputTokens":10,"outputTokens":20,"totalTokens":30}}}`),
	}

	msgs := RebuildMessages(1, entries)

	for _, m := range msgs {
		if m.Role != model.RoleUser {
			continue
		}
		if _, bad := m.Payload["turnUsage"]; bad {
			t.Fatalf("用户消息 %q 被挂上了本轮计量：%+v", m.Content, m.Payload)
		}
	}
	var agentText *model.Message
	for i := range msgs {
		if msgs[i].Role == model.RoleAgent && msgs[i].Kind == model.KindText {
			agentText = &msgs[i]
		}
	}
	if agentText == nil {
		t.Fatalf("没有 agent 正文：%+v", msgs)
	}
	usage, ok := agentText.Payload["turnUsage"].(*acp.Usage)
	if !ok || usage.TotalTokens != 30 {
		t.Errorf("turnUsage = %#v，期望挂在 agent 正文上", agentText.Payload["turnUsage"])
	}
}

// codex 的权限请求不带 Title，命令在 rawInput.command 里（值自带一层
// 引号）——裁决记录必须能从那里取出主语，否则历史里只剩一个 "execute"。
func TestRebuildMessagesPermissionCodexShape(t *testing.T) {
	entries := []transcript.Entry{
		wire(t, "send", promptFrame(1, "写个文件")),
		wire(t, "recv", `{"jsonrpc":"2.0","id":200,"method":"session/request_permission","params":{"sessionId":"s","toolCall":{"toolCallId":"exec-1","kind":"execute","status":"pending","rawInput":{"command":"\"printf 'x' > /tmp/a.txt\"","cwd":"/repo"}},"options":[{"optionId":"approve","name":"Allow Once","kind":"allow_once"}]}}`),
		wire(t, "send", `{"jsonrpc":"2.0","id":200,"result":{"outcome":{"outcome":"selected","optionId":"approve"}}}`),
		wire(t, "recv", resultFrame(1)),
	}
	msgs := RebuildMessages(1, entries)

	var perm *model.Message
	for i := range msgs {
		if msgs[i].Kind == model.KindPermissionRequest {
			perm = &msgs[i]
		}
	}
	if perm == nil {
		t.Fatalf("没有裁决记录：%+v", msgs)
	}
	if perm.Content != "printf 'x' > /tmp/a.txt" {
		t.Errorf("content = %q，期望取出 rawInput.command 并剥掉外层引号", perm.Content)
	}
	if perm.Payload["choice"] != "Allow Once" || perm.Payload["toolKind"] != "execute" {
		t.Errorf("payload = %+v", perm.Payload)
	}
}
