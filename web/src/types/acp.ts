// ACP (Agent Client Protocol) 领域类型，与 server/internal/model 保持一致。

export type AgentStatus = "idle" | "connected" | "error" | "disabled"

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
  createdAt: string
  updatedAt: string
}

export type SessionState = "active" | "idle" | "ended" | "error"

/** 会话模式（审批/沙箱档位），来自 session/new 的 modes。 */
export interface SessionMode {
  id: string
  name: string
  description?: string
}

/** 会话可用模型，来自 session/new 的 models。 */
export interface SessionModel {
  modelId: string
  name: string
  description?: string
}

/** 会话配置项（模型族、协作模式、推理档等），来自 session/new 的 configOptions。 */
export interface ConfigOption {
  id: string
  name: string
  description?: string
  category?: string
  type: string
  currentValue?: string
  options?: { value: string; name: string; description?: string }[]
}

/** 活会话的可配置能力快照，只在 agent 子进程开着时非空。 */
export interface SessionCaps {
  modes?: { availableModes: SessionMode[]; currentModeId: string }
  models?: { availableModels: SessionModel[]; currentModelId: string }
  configOptions?: ConfigOption[]
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
  caps?: SessionCaps
  createdAt: string
  updatedAt: string
}

/** SSE 推来的事件类型。 */
export type StreamEventKind =
  | "user_message"
  | "message_chunk"
  | "thought_chunk"
  | "tool_call"
  | "permission"
  | "plan"
  | "mode"
  | "config"
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
  modeId?: string
  configOptions?: ConfigOption[]
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
