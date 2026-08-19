import type { LiveToolCall } from "@/lib/chat-events"
import type { Message } from "@/types/acp"

/**
 * 子代理清单的提取。
 *
 * 两端派子代理的机制完全不同，差异在这里收敛成一种条目：
 * - claude 用 Agent/Task 工具，简报与结果都在这次调用的 rawInput/rawOutput 上，
 *   拿得到全套；子代理干活时的工具调用另行挂在 `subagentOf` 上。
 * - codex 的子代理是独立 thread，主流里只有一条活动事件，简报被加密、结果不在
 *   协议里——只有 threadId 能顺藤摸瓜（转录要另行 session/load 拉）。
 */

/** 子代理的运行状态，列表按它分组。 */
export type SubagentState = "running" | "done" | "failed"

export interface SubagentEntry {
  /** 启动它的那次工具调用 id，列表内唯一。 */
  id: string
  /** 展示名：claude 是 subagent_type，codex 取 agentPath 末段。 */
  name: string
  /** 一句话描述，codex 侧没有。 */
  description: string
  state: SubagentState
  /** 任务简报原文。codex 侧拿不到（spawn_agent 的 message 是密文）。 */
  input: string
  /** 最终结果。codex 侧要凭 threadId 另行拉取。 */
  output: string
  /** codex 专用：子代理独立 thread 的 id。 */
  threadId?: string
}

/** 工具调用的通用形状，消息 payload 与流式条目共用这一份读取口径。 */
interface ToolCallShape {
  id: string
  status: string
  rawInput?: unknown
  rawOutput?: unknown
  isSubagent?: boolean
  subagentThreadId?: string
  subagentPath?: string
}

function record(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
}

function str(value: unknown): string {
  return typeof value === "string" ? value : ""
}

/**
 * 结果文本的提取。claude 的 rawOutput 实测是 `[{type:"text",text}]`，
 * 也可能直接是字符串；codex 的工具输出是对象，取不到就留空由界面兜底。
 */
function outputText(raw: unknown): string {
  if (typeof raw === "string") return raw
  if (Array.isArray(raw)) {
    return raw
      .map((block) => str(record(block).text))
      .filter(Boolean)
      .join("\n")
      .trim()
  }
  return ""
}

/** codex 的 agentPath 形如 `/root/project_inventory`，末段才是人看的名字。 */
function nameFromPath(path: string): string {
  const last = path.split("/").filter(Boolean).pop()
  return last ?? ""
}

function stateOf(tool: ToolCallShape): SubagentState {
  // codex 的中断是活动种类而非状态——它照样以 completed 收尾，
  // 只有 activityKind 能看出这次是被打断的。
  if (str(record(tool.rawInput).activityKind) === "interrupted") return "failed"
  switch (tool.status) {
    case "completed":
      return "done"
    case "failed":
    case "cancelled":
      return "failed"
    default:
      return "running"
  }
}

function toEntry(tool: ToolCallShape): SubagentEntry {
  const input = record(tool.rawInput)
  const path = tool.subagentPath ?? str(input.agentPath)
  return {
    id: tool.id,
    name: str(input.subagent_type) || nameFromPath(path),
    description: str(input.description),
    state: stateOf(tool),
    input: str(input.prompt),
    output: outputText(tool.rawOutput),
    threadId: tool.subagentThreadId,
  }
}

function fromMessage(message: Message): ToolCallShape | null {
  if (message.kind !== "tool_call" || !message.payload) return null
  const p = message.payload
  if (p.isSubagent !== true) return null
  const id = str(p.toolCallId)
  if (!id) return null
  return {
    id,
    status: str(p.status),
    rawInput: p.rawInput,
    rawOutput: p.rawOutput,
    isSubagent: true,
    subagentThreadId: str(p.subagentThreadId) || undefined,
    subagentPath: str(p.subagentPath) || undefined,
  }
}

/**
 * 从已落转录的消息与本轮流式条目中提取子代理清单，按出现顺序排列。
 * 同一个 id 以流式条目为准——它比重建出的历史更新。
 */
export function collectSubagents(
  messages: Message[],
  liveTools: LiveToolCall[] = []
): SubagentEntry[] {
  const order: string[] = []
  const byID = new Map<string, ToolCallShape>()
  const put = (tool: ToolCallShape) => {
    if (!byID.has(tool.id)) order.push(tool.id)
    byID.set(tool.id, tool)
  }

  for (const message of messages) {
    const tool = fromMessage(message)
    if (tool) put(tool)
  }
  for (const tool of liveTools) {
    if (tool.isSubagent) put(tool)
  }
  return order.map((id) => toEntry(byID.get(id)!))
}

/** 判断一条工具调用是不是某个子代理干的——主对话流要把它们摘出去。 */
export function isSubagentWork(
  payload: Record<string, unknown> | null
): boolean {
  return (
    !!payload &&
    typeof payload.subagentOf === "string" &&
    payload.subagentOf !== ""
  )
}
