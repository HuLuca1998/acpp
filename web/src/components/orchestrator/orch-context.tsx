import { createContext, useContext } from "react"

import type { useOrchChat } from "@/hooks/use-orch-chat"
import type { ImageAttachment } from "@/types/acp"

/** 编排页共享给各面板的数据与命令（与普通会话的 ChatPanelData 对齐）。 */
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
  /** 待发送附件：图片与 @ 引用文件。 */
  images: ImageAttachment[]
  files: string[]
  /** @ 引用的数据库（`<项目>/<环境>[/<库>[/<表>]]`）。 */
  dbRefs: string[]
  removeImage: (index: number) => void
  removeFile: (index: number) => void
  removeDbRef: (index: number) => void
  /** 加一条数据库引用（/db 面板与 @ 菜单共用，重复的不再加）。 */
  addDbRef: (ref: string) => void
  addImages: (picked: File[]) => void
  openImagePicker: () => void
  openFilePicker: () => void
  openDbRefPicker: () => void
  /** 打开上传本机文件的对话框。 */
  openUpload: () => void
  /** 排队插话：撤回回填 / 立即插入当前轮。 */
  recallQueued: (id: number) => void
  steerQueued: (id: number) => void
}

export const OrchChatContext = createContext<OrchChatValue | null>(null)

export function useOrchCtx(): OrchChatValue {
  const ctx = useContext(OrchChatContext)
  if (!ctx) throw new Error("useOrchCtx must be used within OrchChatContext")
  return ctx
}
