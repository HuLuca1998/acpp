import { isSubagentWork } from "@/lib/subagents"
import type { Message } from "@/types/acp"

/** 思考 / 工具调用等过程性消息，聚合成一个可折叠块展示。
 *  plan 不在其中：计划快照独立成卡，与实时计划卡的呈现位置一致。 */
const ACTIVITY_KINDS = new Set<Message["kind"]>([
  "thought",
  "tool_call",
  "tool_result",
  "permission_request",
])

export type Block =
  | { type: "chat"; message: Message }
  | { type: "edit"; message: Message }
  | { type: "activity"; key: string; items: Message[] }

/** 文件编辑类工具调用：独立成消息条展示，不折叠进活动块。 */
function isEditToolCall(message: Message): boolean {
  if (message.kind !== "tool_call") return false
  return (message.payload as { kind?: string } | null)?.kind === "edit"
}

/**
 * 把连续的过程性消息合并成一个 activity 块，正文消息原样保留。
 *
 * 子代理干活时的工具调用不进主流——它们由子代理面板成列陈列。不滤掉的话
 * 主 agent 看起来像在跑一堆用户没让它跑的命令（agent 无论是否声明
 * subagent-transcript 都会把这些调用推过来）。
 */
export function groupMessages(messages: Message[]): Block[] {
  const blocks: Block[] = []
  for (const message of messages) {
    if (isSubagentWork(message.payload)) continue
    if (isEditToolCall(message)) {
      blocks.push({ type: "edit", message })
      continue
    }
    if (!ACTIVITY_KINDS.has(message.kind)) {
      blocks.push({ type: "chat", message })
      continue
    }
    const last = blocks[blocks.length - 1]
    if (last?.type === "activity") {
      last.items.push(message)
    } else {
      blocks.push({
        type: "activity",
        key: `activity-${message.id}`,
        items: [message],
      })
    }
  }
  return blocks
}

/** 块的稳定 key：活动块自带 key，其余用消息 id。 */
export function blockKey(block: Block): string {
  return block.type === "activity" ? block.key : String(block.message.id)
}

/**
 * 算出哪些块是「一轮的开头」——头像只戳在那儿，同一轮后续的块留空槽。
 * liveStartsTurn 说的是历史里最后一句是人说的：那这一轮的活内容就是开头，
 * 头像该给实时区排在最前的那一块。
 */
export function turnStartsOf(blocks: Block[]): {
  starts: Set<string>
  liveStartsTurn: boolean
} {
  const starts = new Set<string>()
  let live = true
  for (const block of blocks) {
    if (block.type === "chat" && block.message.role === "user") {
      live = true
      continue
    }
    if (live) {
      starts.add(blockKey(block))
      live = false
    }
  }
  return { starts, liveStartsTurn: live }
}
