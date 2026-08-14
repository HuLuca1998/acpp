import { createContext, useContext } from "react"

import type { useOrchChat } from "@/hooks/use-orch-chat"

/** 编排页共享给各面板的数据与命令。 */
export interface OrchChatValue {
  /** 草稿态为 true：会话未创建，首条消息落地才建。 */
  isNew: boolean
  chat: ReturnType<typeof useOrchChat>
  draft: string
  setDraft: (v: string) => void
  submit: () => void
  /** 从任务列表打开（或聚焦）一个任务子会话面板。 */
  openTaskPanel: (taskId: number) => void
  /** 草稿态的工具选择。 */
  agentId: number
  setAgentId: (id: number) => void
  draftCwd: string
  openCwdPicker: () => void
}

export const OrchChatContext = createContext<OrchChatValue | null>(null)

export function useOrchCtx(): OrchChatValue {
  const ctx = useContext(OrchChatContext)
  if (!ctx) throw new Error("useOrchCtx must be used within OrchChatContext")
  return ctx
}
