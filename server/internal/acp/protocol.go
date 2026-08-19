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
	// AgentInfo 是 agent 自报的身份，flavor 识别的信号之一。
	AgentInfo   *AgentInfo   `json:"agentInfo,omitempty"`
	AuthMethods []AuthMethod `json:"authMethods"`
}

type AgentInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
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
	// AdditionalDirectories 是技能隔离注入：codex-acp 把每个目录的
	// .agents/skills 注册为 skill 根（控制端技能包 + 工作目录）。claude 不认。
	AdditionalDirectories []string `json:"additionalDirectories,omitempty"`
	// Meta 是 _meta：claude 的 claudeCode.options 隔离注入入口（settingSources
	// / plugins / strictMcpConfig）。codex 不认。
	Meta map[string]any `json:"_meta,omitempty"`
}

// NewSessionResult 只收 modes 与 configOptions；codex 顶层的 models
// （模型×推理档笛卡尔积）按交集规范弃用——统一模型清单从 configOptions 提取。
type NewSessionResult struct {
	SessionID     string         `json:"sessionId"`
	Modes         *Modes         `json:"modes,omitempty"`
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

// ---- session/set_mode、session/set_config_option ----
// 注意没有 set_model：claude 没实现它，codex 上它与 set_config_option
// 写同一个状态互相覆盖——模型切换统一走 set_config_option。

type SetModeParams struct {
	SessionID string `json:"sessionId"`
	ModeID    string `json:"modeId"`
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
	// 恢复路径也要带隔离参数（2026-08-13 实测：不带则退回机器级 skill）。
	AdditionalDirectories []string       `json:"additionalDirectories,omitempty"`
	Meta                  map[string]any `json:"_meta,omitempty"`
}

// ---- session/prompt ----

type PromptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

// ContentBlock 是 prompt 的一个内容块。text 之外，两端 promptCapabilities
// 都声明支持 image（base64）与 embeddedContext（resource 内嵌文件内容）。
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// image 块：base64 数据与 MIME 类型。
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	// resource 块：内嵌的文件内容（embeddedContext）。
	Resource *EmbeddedResource `json:"resource,omitempty"`
}

// EmbeddedResource 是嵌进 prompt 的文件内容。
type EmbeddedResource struct {
	URI      string `json:"uri"`
	Text     string `json:"text"`
	MimeType string `json:"mimeType,omitempty"`
}

// TextBlock 构造一个纯文本内容块。
func TextBlock(text string) ContentBlock {
	return ContentBlock{Type: "text", Text: text}
}

// ImageBlock 构造一个图片内容块。
func ImageBlock(data, mimeType string) ContentBlock {
	return ContentBlock{Type: "image", Data: data, MimeType: mimeType}
}

// ResourceBlock 构造一个内嵌文件内容块（@ 引用）。
func ResourceBlock(uri, text string) ContentBlock {
	return ContentBlock{Type: "resource", Resource: &EmbeddedResource{URI: uri, Text: text}}
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

// Usage 是一轮的 token 计量，只保留两端 runtime 都报的交集字段
// （claude 的 cachedWriteTokens/cost、codex 的 thoughtTokens 按交集规范废弃）。
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

// ---- session/delete（删除 agent 侧的会话历史）----

type DeleteSessionParams struct {
	SessionID string `json:"sessionId"`
}

// ---- _session/steering（turn 进行中插话，注入当前轮）----

type SteeringParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
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
	UpdateUsage             UpdateKind = "usage_update"
	UpdateSessionInfo       UpdateKind = "session_info_update"
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
	// RawOutput 的形状随 runtime 变：codex 是对象 {formatted_output, exit_code}，
	// claude 是纯字符串——所以只能收原文，展示层自行兼容。
	ToolCallID string          `json:"toolCallId,omitempty"`
	Title      string          `json:"title,omitempty"`
	Kind       string          `json:"kind,omitempty"`
	Status     string          `json:"status,omitempty"`
	RawInput   json.RawMessage `json:"rawInput,omitempty"`
	RawOutput  json.RawMessage `json:"rawOutput,omitempty"`
	// Locations 是工具触碰的文件位置 [{path}]，两端都带。
	Locations json.RawMessage `json:"locations,omitempty"`

	// plan
	Entries json.RawMessage `json:"entries,omitempty"`

	// usage_update：上下文用量。size 的语义两端有出入（claude 是模型窗口
	// 大小，codex 是会话水位），按占比展示两端都成立；claude 独有的 cost
	// 按交集规范废弃，不解析。
	Used int64 `json:"used,omitempty"`
	Size int64 `json:"size,omitempty"`

	// current_mode_update：agent 自己切了档。不同实现字段名不同，两个都收。
	CurrentModeID string `json:"currentModeId,omitempty"`
	ModeID        string `json:"modeId,omitempty"`

	// config_option_update：配置项变化，带全量新配置。
	ConfigOptions []ConfigOption `json:"configOptions,omitempty"`

	// available_commands_update：可用斜杠命令清单，带全量。
	AvailableCommands []Command `json:"availableCommands,omitempty"`

	// Meta 是私有扩展。只解析子代理相关字段——它是两端唯一的父子关系载体，
	// 协议本身没有嵌套消息的概念。
	Meta *UpdateMeta `json:"_meta,omitempty"`
}

// UpdateMeta 收两端 _meta 里的子代理信息。两端机制截然不同：claude 把子代理的
// 工具调用（声明 subagent-transcript 后还有正文与思考）混进主流，靠
// parentToolUseId 认领；codex 只发一条活动事件，转录留在独立 thread 里。
type UpdateMeta struct {
	ClaudeCode *ClaudeCodeMeta `json:"claudeCode,omitempty"`
	Codex      *CodexMeta      `json:"codex,omitempty"`
}

// ClaudeCodeMeta 是 claude 的私有扩展（只取子代理相关的）。
type ClaudeCodeMeta struct {
	// Subagent 为真表示这条 tool_call 就是启动子代理的 Agent/Task 调用本身。
	Subagent bool `json:"subagent,omitempty"`
	// ParentToolUseID 指向产生这条 update 的子代理所挂的 Agent/Task 调用。
	// 实测同一工具调用的部分后续 update 会漏带它（连 Subagent 标记一起漏），
	// 所以归属必须在按 ToolCallID 合并时记住，不能每条现读现判。
	ParentToolUseID string `json:"parentToolUseId,omitempty"`
	ToolName        string `json:"toolName,omitempty"`
}

// CodexMeta 是 codex 的私有扩展（只取子代理相关的）。
type CodexMeta struct {
	Subagent *CodexSubagent `json:"subagent,omitempty"`
}

// CodexSubagent 是 codex 子代理活动事件的载荷。子代理是独立 thread，
// ThreadID 可直接喂给 session/load 拉全量转录——这是 codex 侧取转录的唯一途径。
type CodexSubagent struct {
	ThreadID string `json:"threadId,omitempty"`
	Path     string `json:"path,omitempty"`
	// Activity 为 started / interacted / interrupted。
	Activity string `json:"activity,omitempty"`
}

// SubagentOf 返回产生这条 update 的子代理所挂的 Agent/Task 调用 id；
// 不是子代理产生的返回空。只有 claude 有——codex 不转发子代理转录。
func (u SessionUpdate) SubagentOf() string {
	if u.Meta == nil || u.Meta.ClaudeCode == nil {
		return ""
	}
	return u.Meta.ClaudeCode.ParentToolUseID
}

// IsSubagentLaunch 判断这条工具调用是不是「启动了一个子代理」的那次调用。
// claude 是 Agent/Task 工具带 subagent 标记，codex 是 subAgentActivity 事件。
func (u SessionUpdate) IsSubagentLaunch() bool {
	if u.Meta == nil {
		return false
	}
	if u.Meta.ClaudeCode != nil && u.Meta.ClaudeCode.Subagent {
		return true
	}
	return u.Meta.Codex != nil && u.Meta.Codex.Subagent != nil
}

// CodexSubagentThread 返回 codex 子代理的独立 thread id 与 agent 路径，
// 不是 codex 子代理事件时返回两个空串。
func (u SessionUpdate) CodexSubagentThread() (threadID, path string) {
	if u.Meta == nil || u.Meta.Codex == nil || u.Meta.Codex.Subagent == nil {
		return "", ""
	}
	return u.Meta.Codex.Subagent.ThreadID, u.Meta.Codex.Subagent.Path
}

// Command 是 agent 暴露的一条斜杠命令。发送时就是普通文本（"/plan …"），
// 两端 runtime 都认；codex 的 _meta.commandAction 等私有扩展不解析。
type Command struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
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

// PermissionToolCall 是权限请求关联的工具调用。codex 只给前三个字段；
// Title/RawInput/Content（diff）只有 claude 带，展示层按空值收敛。
type PermissionToolCall struct {
	ToolCallID string          `json:"toolCallId"`
	Kind       string          `json:"kind"`
	Status     string          `json:"status"`
	Title      string          `json:"title,omitempty"`
	RawInput   json.RawMessage `json:"rawInput,omitempty"`
	Content    json.RawMessage `json:"content,omitempty"`
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
