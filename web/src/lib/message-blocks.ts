import type { Message } from "@/types/acp"

/** 思考 / 工具调用等过程性消息，聚合成一个可折叠块展示。 */
const ACTIVITY_KINDS = new Set<Message["kind"]>([
  "thought",
  "tool_call",
  "tool_result",
  "permission_request",
  "plan",
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

/** 把连续的过程性消息合并成一个 activity 块，正文消息原样保留。 */
export function groupMessages(messages: Message[]): Block[] {
  const blocks: Block[] = []
  for (const message of messages) {
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
