package acp

import (
	"encoding/json"
	"testing"
)

// 下面的 wire 片段全部来自 2026-08-19 对 claude-agent-acp 0.63.0 /
// codex-acp 1.1.7 的真机实测（真派了子代理抓的 session/update），不是手编的。
// 改这里前先重跑一遍探针，别照着想象改。

// 契约：两端「启动了子代理」的那次工具调用都要被认出来，各自的私有形状
// （claude 的 claudeCode.subagent、codex 的 codex.subagent）都不能漏。
func TestSessionUpdate_SubagentLaunch(t *testing.T) {
	tests := []struct {
		name       string
		wire       string
		wantLaunch bool
		wantThread string
		wantPath   string
	}{
		{
			name: "claude 的 Agent 工具调用",
			wire: `{"sessionUpdate":"tool_call","toolCallId":"toolu_01Gdi","status":"pending",
			        "_meta":{"claudeCode":{"toolName":"Agent","subagent":true}}}`,
			wantLaunch: true,
		},
		{
			name: "codex 的 subAgentActivity",
			wire: `{"sessionUpdate":"tool_call","toolCallId":"item_7","title":"Start subagent project_inventory",
			        "_meta":{"codex":{"subagent":{"threadId":"01a01803-8e03-7d33-8e73-44140947a73d",
			        "path":"/root/project_inventory","activity":"started"}}}}`,
			wantLaunch: true,
			wantThread: "01a01803-8e03-7d33-8e73-44140947a73d",
			wantPath:   "/root/project_inventory",
		},
		{
			name: "子代理干活时的普通工具调用不算启动",
			wire: `{"sessionUpdate":"tool_call","toolCallId":"toolu_01P554",
			        "_meta":{"claudeCode":{"toolName":"Bash","parentToolUseId":"toolu_01Gdi"}}}`,
			wantLaunch: false,
		},
		{
			name:       "没有 _meta 的普通工具调用",
			wire:       `{"sessionUpdate":"tool_call","toolCallId":"call_1"}`,
			wantLaunch: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var u SessionUpdate
			if err := json.Unmarshal([]byte(tt.wire), &u); err != nil {
				t.Fatalf("解析 wire 失败: %v", err)
			}
			if got := u.IsSubagentLaunch(); got != tt.wantLaunch {
				t.Errorf("IsSubagentLaunch() = %v, 期望 %v", got, tt.wantLaunch)
			}
			thread, path := u.CodexSubagentThread()
			if thread != tt.wantThread || path != tt.wantPath {
				t.Errorf("CodexSubagentThread() = (%q, %q), 期望 (%q, %q)",
					thread, path, tt.wantThread, tt.wantPath)
			}
		})
	}
}

// 契约：子代理干出来的事件必须认领到它所挂的 Agent 调用上。嵌套子代理
// （子代理再派子代理）的启动调用自身也带 parentToolUseId，父子链靠它串起来
// ——实测两层嵌套，第二层 Agent 卡片的 parent 正是第一层的卡片 id。
func TestSessionUpdate_SubagentOf(t *testing.T) {
	tests := []struct {
		name string
		wire string
		want string
	}{
		{
			name: "子代理的工具调用认领到父 Agent 调用",
			wire: `{"sessionUpdate":"tool_call","toolCallId":"toolu_01P554",
			        "_meta":{"claudeCode":{"toolName":"Bash","parentToolUseId":"toolu_01Gdi"}}}`,
			want: "toolu_01Gdi",
		},
		{
			name: "嵌套：第二层 Agent 调用挂在第一层上",
			wire: `{"sessionUpdate":"tool_call","toolCallId":"toolu_01Rwus",
			        "_meta":{"claudeCode":{"toolName":"Agent","subagent":true,
			        "parentToolUseId":"toolu_01Bzp4"}}}`,
			want: "toolu_01Bzp4",
		},
		{
			name: "主流事件没有归属",
			wire: `{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hi"}}`,
			want: "",
		},
		{
			// 这不是 bug 而是 agent 的真实行为：同一次工具调用的部分后续 update
			// 会把 parentToolUseId 连同 subagent 标记一起漏掉。解析层如实反映，
			// 记忆归属是上层按 ToolCallID 合并时的责任。
			name: "agent 漏带归属时如实返回空",
			wire: `{"sessionUpdate":"tool_call_update","toolCallId":"toolu_01P554","status":"completed",
			        "_meta":{"claudeCode":{"toolName":"Bash"}}}`,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var u SessionUpdate
			if err := json.Unmarshal([]byte(tt.wire), &u); err != nil {
				t.Fatalf("解析 wire 失败: %v", err)
			}
			if got := u.SubagentOf(); got != tt.want {
				t.Errorf("SubagentOf() = %q, 期望 %q", got, tt.want)
			}
		})
	}
}
