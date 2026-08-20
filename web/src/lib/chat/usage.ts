import type { Message, TurnUsage } from "@/types/acp"

/** 会话累计用量：把历史各轮的 token 计量加起来。 */
export interface SessionUsageTotals {
  /** 有计量的轮数——不是消息数，一轮只有一条正文带 turnUsage。 */
  turns: number
  inputTokens: number
  outputTokens: number
  cachedReadTokens: number
  totalTokens: number
}

/** 从历史里读出的用量：累计 + 最近一轮。 */
export interface SessionUsage {
  totals: SessionUsageTotals
  /** 最后一轮的计量——SSE 的 lastUsage 刷新即失，靠它兜底。 */
  last: TurnUsage
}

/**
 * 从重建出的历史消息里累加会话总用量，并带出最后一轮。
 *
 * 数据源是每轮 prompt 响应带的 usage（重建器挂在轮末正文的
 * payload.turnUsage 上）——所以它统计的是**已收尾的轮**，正在跑的这一轮
 * 要等结束后才计入。没有任何一轮带计量时返回 null，界面据此不显示这块。
 */
export function sumSessionUsage(messages: Message[]): SessionUsage | null {
  const totals: SessionUsageTotals = {
    turns: 0,
    inputTokens: 0,
    outputTokens: 0,
    cachedReadTokens: 0,
    totalTokens: 0,
  }
  let last: TurnUsage | null = null
  for (const m of messages) {
    const usage = (m.payload as { turnUsage?: TurnUsage } | null)?.turnUsage
    if (!usage) continue
    last = usage
    totals.turns += 1
    totals.inputTokens += usage.inputTokens
    totals.outputTokens += usage.outputTokens
    totals.cachedReadTokens += usage.cachedReadTokens
    totals.totalTokens += usage.totalTokens
  }
  return last ? { totals, last } : null
}
