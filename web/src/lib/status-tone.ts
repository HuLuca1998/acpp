import type { AgentStatus, SessionState } from "@/types/acp"

export type StatusTone = "success" | "warning" | "destructive" | "muted"

/** 会话状态 → 状态点色调，全部页面共用一份语义。 */
export const SESSION_STATE_TONE: Record<SessionState, StatusTone> = {
  active: "success",
  idle: "muted",
  ended: "muted",
  error: "destructive",
}

/** agent 状态 → 状态点色调。「正在运行」由 running/pulse 表达，不占状态位。 */
export const AGENT_STATUS_TONE: Record<AgentStatus, StatusTone> = {
  idle: "muted",
  error: "destructive",
}
