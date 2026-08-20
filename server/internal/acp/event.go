package acp

import "encoding/json"

// EventKind 是推给上层的归一化事件类型。
type EventKind string

const (
	EventMessage  EventKind = "message"
	EventThought  EventKind = "thought"
	EventToolCall EventKind = "tool_call"
	EventPlan     EventKind = "plan"
	// EventPermission 表示 agent 在等用户裁决权限；Done 在裁决/超时后发出，
	// 界面收到后应收起卡片。
	EventPermission     EventKind = "permission"
	EventPermissionDone EventKind = "permission_done"
	// EventSettings 在 agent 自行切档/改配置后发出，带最新的统一设置视图。
	EventSettings EventKind = "settings"
	// EventUsage 是上下文用量快照（usage_update 通知）。
	EventUsage EventKind = "usage"
	// EventCommands 是可用斜杠命令清单（全量替换）。
	EventCommands EventKind = "commands"
	// EventElicitation 表示 agent 在等用户作答；Done 在作答/超时后发出，
	// 界面收到后应收起提问卡片。
	EventElicitation     EventKind = "elicitation"
	EventElicitationDone EventKind = "elicitation_done"
	EventTurnEnd         EventKind = "turn_end"
	EventError           EventKind = "error"
)

// Event 是一条归一化事件。工具调用的多条更新共用同一个 ToolCallID，
// 上层必须按它合并，否则界面会出现一堆重复条目。
type Event struct {
	Kind       EventKind       `json:"kind"`
	Text       string          `json:"text,omitempty"`
	ToolCallID string          `json:"toolCallId,omitempty"`
	Title      string          `json:"title,omitempty"`
	ToolKind   string          `json:"toolKind,omitempty"`
	Status     string          `json:"status,omitempty"`
	RawInput   json.RawMessage `json:"rawInput,omitempty"`
	RawOutput  json.RawMessage `json:"rawOutput,omitempty"`
	Content    json.RawMessage `json:"content,omitempty"`
	Locations  json.RawMessage `json:"locations,omitempty"`
	// 子代理归属。IsSubagent 为真表示这条工具调用启动了一个子代理（claude 的
	// Agent/Task、codex 的 subAgentActivity）；SubagentOf 非空表示这条事件是某个
	// 子代理干出来的，值为它所挂的启动调用 id。两者互斥。
	// 注意：agent 会在部分后续 update 里漏带这两个标记，上层按 ToolCallID 合并时
	// 必须保留已知的非空值，不能用后来的空值覆盖。
	IsSubagent bool   `json:"isSubagent,omitempty"`
	SubagentOf string `json:"subagentOf,omitempty"`
	// codex 专用：子代理是独立 thread，凭 SubagentThreadID 才能 session/load
	// 拉到它的转录；SubagentPath 形如 /root/<任务名>，末段用作展示名。
	SubagentThreadID string          `json:"subagentThreadId,omitempty"`
	SubagentPath     string          `json:"subagentPath,omitempty"`
	Entries          json.RawMessage `json:"entries,omitempty"`
	Settings         *Settings       `json:"settings,omitempty"`
	Used             int64           `json:"used,omitempty"`
	Size             int64           `json:"size,omitempty"`
	Cost             *UsageCost      `json:"cost,omitempty"`
	Commands         []Command       `json:"commands,omitempty"`
	ElicitationID    string          `json:"elicitationId,omitempty"`
	// 权限请求：ID 用于回传裁决，Options 是 agent 给的选项。
	// Title/RawInput/Content 只有 claude 带，前端按空值收敛。
	// PlanReview 非空时这是「计划完成」审批，前端渲染专门卡片。
	PermissionID string             `json:"permissionId,omitempty"`
	Options      []PermissionOption `json:"options,omitempty"`
	PlanReview   *PlanReview        `json:"planReview,omitempty"`
	StopReason   StopReason         `json:"stopReason,omitempty"`
	Usage        *Usage             `json:"usage,omitempty"`
	Error        string             `json:"error,omitempty"`
}
