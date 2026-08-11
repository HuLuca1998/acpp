package acp

import (
	"encoding/json"
	"strings"
)

// ProtocolVersion 是当前 ACP 的 MAJOR 版本，单个整数。
const ProtocolVersion = 1

// ---- JSON-RPC 2.0 ----

// rpcMessage 是 ndjson 里的一行，request / response / notification 三种形态共用。
//
//	有 Method + 有 ID → agent 反向调用我们，必须回
//	有 Method + 无 ID → 通知，不回
//	无 Method + 有 ID → 我们发出的请求的响应
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// ---- initialize ----

type InitializeParams struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientCapabilities ClientCapabilities `json:"clientCapabilities"`
}

// ClientCapabilities 是一份承诺：声明了就必须实现，否则 agent 调过来会吃到 -32601。
type ClientCapabilities struct {
	FS       FSCapability `json:"fs"`
	Terminal bool         `json:"terminal"`
	// Elicitation 声明后，agent 的交互式提问（AskUserQuestion 一类）
	// 会作为 elicitation/create 反向调用发过来；不声明会被 agent 静默跳过。
	Elicitation *ElicitationCapability `json:"elicitation,omitempty"`
}

// ElicitationCapability 里 form 字段非 null 即表示支持表单式提问。
type ElicitationCapability struct {
	Form struct{} `json:"form"`
}

type FSCapability struct {
	ReadTextFile  bool `json:"readTextFile"`
	WriteTextFile bool `json:"writeTextFile"`
}

type InitializeResult struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities AgentCapabilities `json:"agentCapabilities"`
	AuthMethods       []AuthMethod      `json:"authMethods"`
}

type AgentCapabilities struct {
	LoadSession bool `json:"loadSession"`
}

type AuthMethod struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ---- session/new ----

type NewSessionParams struct {
	// Cwd 必须是已存在的绝对路径。
	Cwd string `json:"cwd"`
	// MCPServers 必需，可以是空数组。
	MCPServers []any `json:"mcpServers"`
}

type NewSessionResult struct {
	SessionID     string         `json:"sessionId"`
	Modes         *Modes         `json:"modes,omitempty"`
	Models        *Models        `json:"models,omitempty"`
	ConfigOptions []ConfigOption `json:"configOptions,omitempty"`
}

type Modes struct {
	AvailableModes []Mode `json:"availableModes"`
	CurrentModeID  string `json:"currentModeId"`
}

type Mode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type Models struct {
	AvailableModels []Model `json:"availableModels"`
	CurrentModelID  string  `json:"currentModelId"`
}

type Model struct {
	ModelID     string `json:"modelId"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ConfigOption 是 agent 暴露的会话级配置项（模型族、协作模式、推理档等）。
// codex-acp 实测全部是 type=select，value 都是字符串。
type ConfigOption struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Description  string              `json:"description,omitempty"`
	Category     string              `json:"category,omitempty"`
	Type         string              `json:"type"`
	CurrentValue string              `json:"currentValue,omitempty"`
	Options      []ConfigOptionValue `json:"options,omitempty"`
}

type ConfigOptionValue struct {
	Value       string `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ---- session/set_mode、session/set_model、session/set_config_option ----

type SetModeParams struct {
	SessionID string `json:"sessionId"`
	ModeID    string `json:"modeId"`
}

type SetModelParams struct {
	SessionID string `json:"sessionId"`
	ModelID   string `json:"modelId"`
}

type SetConfigOptionParams struct {
	SessionID string `json:"sessionId"`
	ConfigID  string `json:"configId"`
	Value     string `json:"value"`
}

// SetConfigOptionResult 带回设置后的全量配置项，直接覆盖本地缓存即可。
type SetConfigOptionResult struct {
	ConfigOptions []ConfigOption `json:"configOptions"`
}

// ---- session/load（恢复既有会话，agent 会重放历史 update）----

type LoadSessionParams struct {
	SessionID  string `json:"sessionId"`
	Cwd        string `json:"cwd"`
	MCPServers []any  `json:"mcpServers"`
}

// ---- session/prompt ----

type PromptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// TextBlock 构造一个纯文本内容块。
func TextBlock(text string) ContentBlock {
	return ContentBlock{Type: "text", Text: text}
}

// Text 从 Content 里取出文本，同时接受单个内容块和内容块数组两种形状。
func (u SessionUpdate) Text() string {
	if len(u.Content) == 0 {
		return ""
	}

	var block ContentBlock
	if err := json.Unmarshal(u.Content, &block); err == nil {
		return block.Text
	}

	var blocks []ContentBlock
	if err := json.Unmarshal(u.Content, &blocks); err == nil {
		var out strings.Builder
		for _, b := range blocks {
			out.WriteString(b.Text)
		}
		return out.String()
	}
	return ""
}

type PromptResult struct {
	StopReason StopReason `json:"stopReason"`
	Usage      *Usage     `json:"usage,omitempty"`
}

// StopReason 是一轮的结束原因，只有 end_turn 表示正常说完。
type StopReason string

const (
	StopEndTurn         StopReason = "end_turn"
	StopMaxTokens       StopReason = "max_tokens"
	StopMaxTurnRequests StopReason = "max_turn_requests"
	StopRefusal         StopReason = "refusal"
	StopCancelled       StopReason = "cancelled"
)

// OK 报告这一轮是否正常说完；其余四种都意味着回答可能是残缺的。
func (s StopReason) OK() bool { return s == StopEndTurn }

type Usage struct {
	InputTokens      int `json:"inputTokens"`
	OutputTokens     int `json:"outputTokens"`
	CachedReadTokens int `json:"cachedReadTokens"`
	TotalTokens      int `json:"totalTokens"`
}

// ---- session/cancel ----

type CancelParams struct {
	SessionID string `json:"sessionId"`
}

// ---- session/update 通知 ----

// UpdateKind 是 sessionUpdate 字段的取值。
type UpdateKind string

const (
	UpdateAgentMessageChunk UpdateKind = "agent_message_chunk"
	UpdateAgentThoughtChunk UpdateKind = "agent_thought_chunk"
	UpdateUserMessageChunk  UpdateKind = "user_message_chunk"
	UpdateToolCall          UpdateKind = "tool_call"
	UpdateToolCallUpdate    UpdateKind = "tool_call_update"
	UpdatePlan              UpdateKind = "plan"
	UpdateCurrentMode       UpdateKind = "current_mode_update"
	UpdateAvailableCommands UpdateKind = "available_commands_update"
	UpdateConfigOption      UpdateKind = "config_option_update"
)

// SessionNotification 是 session/update 的 params。
type SessionNotification struct {
	SessionID string        `json:"sessionId"`
	Update    SessionUpdate `json:"update"`
}

// SessionUpdate 覆盖全部 sessionUpdate 变体；除 SessionUpdate 外都是可选的。
type SessionUpdate struct {
	SessionUpdate UpdateKind `json:"sessionUpdate"`

	// Content 的形状随 sessionUpdate 变化：消息 / 思考分片是单个内容块，
	// 而 tool_call 上是内容块数组。所以只能先收原文，再按类型解码。
	Content json.RawMessage `json:"content,omitempty"`

	// 工具调用。tool_call_update 里除 ToolCallID 外全是可选，必须按 id 合并。
	ToolCallID string          `json:"toolCallId,omitempty"`
	Title      string          `json:"title,omitempty"`
	Kind       string          `json:"kind,omitempty"`
	Status     string          `json:"status,omitempty"`
	RawInput   json.RawMessage `json:"rawInput,omitempty"`
	RawOutput  json.RawMessage `json:"rawOutput,omitempty"`

	// plan
	Entries json.RawMessage `json:"entries,omitempty"`

	// current_mode_update：agent 自己切了档。不同实现字段名不同，两个都收。
	CurrentModeID string `json:"currentModeId,omitempty"`
	ModeID        string `json:"modeId,omitempty"`

	// config_option_update：配置项变化，带全量新配置。
	ConfigOptions []ConfigOption `json:"configOptions,omitempty"`
}

// EffectiveModeID 归一化 current_mode_update 的两种字段名。
func (u SessionUpdate) EffectiveModeID() string {
	if u.CurrentModeID != "" {
		return u.CurrentModeID
	}
	return u.ModeID
}

// ---- session/request_permission（agent 反向请求，阻塞等我们回）----

type RequestPermissionParams struct {
	SessionID string             `json:"sessionId"`
	ToolCall  PermissionToolCall `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

// PermissionToolCall 实测只有这三个字段，没有 title。
type PermissionToolCall struct {
	ToolCallID string `json:"toolCallId"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
}

type PermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

type RequestPermissionResult struct {
	Outcome PermissionOutcome `json:"outcome"`
}

type PermissionOutcome struct {
	Outcome  string `json:"outcome"`
	OptionID string `json:"optionId,omitempty"`
}

// ---- elicitation/create（agent 反向请求，阻塞等用户作答）----

// ElicitationParams 是 agent 的交互式提问。requestedSchema 是一个 JSON Schema
// object：每个 property 是一道题（title/description/oneOf 选项），
// 带 `_meta.codex.isOtherAnswer` 的 property 是对应题目的自由输入栏。
type ElicitationParams struct {
	SessionID       string          `json:"sessionId"`
	ToolCallID      string          `json:"toolCallId,omitempty"`
	Mode            string          `json:"mode"`
	Message         string          `json:"message"`
	RequestedSchema json.RawMessage `json:"requestedSchema,omitempty"`
}

// ElicitationResult 是用户的作答。content 的 key 是题目 id，值是选中的
// 选项文本（oneOf 的 const）或自由输入。
type ElicitationResult struct {
	Action  string         `json:"action"` // accept | decline | cancel
	Content map[string]any `json:"content,omitempty"`
}

// ---- fs/read_text_file、fs/write_text_file（agent 反向调用）----

type ReadTextFileParams struct {
	SessionID string `json:"sessionId"`
	Path      string `json:"path"`
	Line      *int   `json:"line,omitempty"`
	Limit     *int   `json:"limit,omitempty"`
}

type ReadTextFileResult struct {
	Content string `json:"content"`
}

type WriteTextFileParams struct {
	SessionID string `json:"sessionId"`
	Path      string `json:"path"`
	Content   string `json:"content"`
}
