// MCP 工具面（工具台页面 /tools）的领域类型。
// 与 server/internal/mcp 的声明形状、internal/mcpcall 的记录模型对齐；
// 从 ./acp 一并转出，调用方仍统一 import "@/types/acp"。

/** JSON Schema 里我们会渲染成控件的那几位；其余原样展示在 Schema 页签。 */
export interface McpSchemaProperty {
  type?: string
  description?: string
  enum?: string[]
  default?: unknown
}

export interface McpInputSchema {
  type?: string
  properties?: Record<string, McpSchemaProperty>
  required?: string[]
}

/**
 * MCP 标准工具注解。只用两位：只读与破坏性。
 * 它是**提示不是护栏**——真正拦住写操作的是数据源上的只读开关，
 * 页面拿它决定「按下运行前要不要先弹确认」。
 */
export interface McpToolAnnotations {
  readOnlyHint?: boolean
  destructiveHint?: boolean
}

/** 一个工具的声明。描述就是给模型看的那段原文，不是给人另写的说明。 */
export interface McpTool {
  name: string
  description: string
  inputSchema: McpInputSchema
  annotations?: McpToolAnnotations
}

/**
 * 一个挂给 agent 的工具面。`mounted` 为 false 表示这个上下文下工具面
 * 根本不会挂给 agent（没有可用数据源）——工具还能在页面里试，但 AI
 * 那边看不见它们，这件事必须说清楚。
 */
export interface McpServer {
  name: string
  endpoint: string
  mounted: boolean
  sourceCount: number
  tools: McpTool[]
}

/** 一次 JSON-RPC 往返的结果。accepted 表示这条消息是通知、本来就没有响应。 */
export interface McpInspectResult {
  response?: unknown
  durationMs: number
  accepted: boolean
}

/** 调用来源：agent 子进程回连，或工具台里人工按的运行。 */
export type McpCallSource = "agent" | "manual"

/** 一次工具调用的记录。args/result 是落库前截断过的文本。 */
export interface McpCall {
  id: number
  server: string
  tool: string
  sessionId: number
  source: McpCallSource
  cwd: string
  args: string
  result: string
  isError: boolean
  durationMs: number
  createdAt: string
}

/** 按工具聚合的调用统计。 */
export interface McpToolStat {
  server: string
  tool: string
  count: number
  errorCount: number
  avgMs: number
  lastUsedAt: string
}
