// ACP (Agent Client Protocol) 领域类型，与 server/internal/model 保持一致。

export type AgentStatus = "idle" | "connected" | "error" | "disabled"

/** runtime 方言，由后端从 agent 身份识别；generic 表示未知 runtime。 */
export type AgentFlavor = "codex" | "claude" | "generic" | ""

/** 统一模型描述，id 是 runtime 自己的标识，透传不映射。 */
export interface UnifiedModel {
  id: string
  name: string
  description?: string
}

export interface Agent {
  id: number
  name: string
  description: string
  command: string
  args: string[]
  env: Record<string, string>
  cwd: string
  status: AgentStatus
  lastError: string
  /** 探测缓存：flavor 与可用模型清单，供新会话页在建会话前展示。 */
  flavor: AgentFlavor
  models: UnifiedModel[]
  createdAt: string
  updatedAt: string
}

export type SessionState = "active" | "idle" | "ended" | "error"

/** 统一思考深度，五档——两条 ACP 选项的交集。 */
export type EffortLevel = "low" | "medium" | "high" | "xhigh" | "max"

/** 统一权限档，三档，两条 ACP 全覆盖。 */
export type AccessLevel = "safe" | "auto-edit" | "full"

/**
 * 会话设置的统一视图（交集规范：只含两条 ACP 都支持的维度）。
 * 空数组表示该 runtime 不支持这个维度，对应控件应隐藏。
 */
export interface SessionSettings {
  flavor: AgentFlavor
  models: UnifiedModel[]
  currentModel?: string
  efforts: EffortLevel[]
  currentEffort?: EffortLevel
  levels: AccessLevel[]
  currentLevel?: AccessLevel
  planSupported: boolean
  planOn: boolean
  fastSupported: boolean
  fastOn: boolean
}

/** 逐项可选的设置变更，缺省字段不动。 */
export interface SettingsPatch {
  model?: string
  effort?: EffortLevel
  level?: AccessLevel
  plan?: boolean
  fast?: boolean
}

/** 一轮的 token 计量（两端交集字段）。 */
export interface TurnUsage {
  inputTokens: number
  outputTokens: number
  cachedReadTokens: number
  totalTokens: number
}

export interface Session {
  id: number
  agentId: number
  agentName: string
  /** agent 侧返回的 sessionId（uuid v7），用于后续的 session/prompt。 */
  acpSessionId: string
  title: string
  cwd: string
  state: SessionState
  stopReason: string
  messageCount: number
  /** 当前是否有活着的 agent 子进程。 */
  running: boolean
  settings?: SessionSettings
  createdAt: string
  updatedAt: string
}

/** agent 计划里的一项，来自 session/update 的 plan entries。 */
export interface PlanEntry {
  content: string
  priority?: string
  status?: "pending" | "in_progress" | "completed" | string
}

/** SSE 推来的事件类型。 */
export type StreamEventKind =
  | "user_message"
  | "message_chunk"
  | "thought_chunk"
  | "tool_call"
  | "permission"
  | "plan"
  | "settings"
  | "usage"
  | "elicitation"
  | "elicitation_done"
  | "turn_end"
  | "turn_done"
  | "message_saved"
  | "error"

export interface StreamEvent {
  kind: StreamEventKind
  /** 会话内单调递增，用于去重。 */
  seq: number
  text?: string
  toolCallId?: string
  title?: string
  toolKind?: string
  status?: string
  rawInput?: unknown
  rawOutput?: unknown
  /** tool_call 的内容块（diff 等），流式期间即可渲染。 */
  content?: unknown
  locations?: unknown
  /** agent 自行切档/改配置后的最新统一设置视图。 */
  settings?: SessionSettings
  /** usage 事件：上下文用量（按占比展示）。 */
  used?: number
  size?: number
  /** permission 事件：自动选中的选项名。 */
  choice?: string
  /** turn_end 事件：本轮 token 计量。 */
  usage?: TurnUsage
  elicitationId?: string
  stopReason?: string
  error?: string
  message?: Message
}

/**
 * agent 交互式提问的一道题，从 elicitation 的 requestedSchema 解析而来。
 * options 来自 oneOf；otherFieldId 指向对应的自由输入字段（`__other`）。
 */
export interface ElicitationQuestion {
  id: string
  title: string
  description?: string
  required: boolean
  options: { value: string; description?: string }[]
  otherFieldId?: string
}

/** 一次挂起的交互式提问。schema 保留原文，作答后合成历史卡片时要用。 */
export interface PendingElicitation {
  id: string
  message: string
  schema: unknown
  questions: ElicitationQuestion[]
}

export type MessageRole = "user" | "agent" | "system"

/** 对应 ACP 的 session/update 各类 chunk。 */
export type MessageKind =
  | "text"
  | "thought"
  | "tool_call"
  | "tool_result"
  | "permission_request"
  | "plan"
  | "elicitation"

export interface Message {
  id: number
  sessionId: number
  role: MessageRole
  kind: MessageKind
  content: string
  /** tool_call / tool_result / plan 的结构化载荷，后端以 JSON 字符串存储。 */
  payload: Record<string, unknown> | null
  createdAt: string
}

export interface Paged<T> {
  items: T[]
  total: number
  page: number
  pageSize: number
}
