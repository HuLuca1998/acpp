import { createContext } from "react"

import type { useChat } from "@/hooks/use-chat"
import type { useDraftSession } from "@/hooks/use-draft-session"
import type { ImageAttachment } from "@/types/acp"

/**
 * 页面级状态经此 context 注入对话面板：会话流（useChat）、草稿会话、
 * 输入与附件。dockview 重挂载面板时状态不丢——它一直住在页面层。
 */
export interface ChatPanelData {
  isNew: boolean
  chat: ReturnType<typeof useChat>
  newSession: ReturnType<typeof useDraftSession>
  draft: string
  setDraft: (v: string) => void
  images: ImageAttachment[]
  files: string[]
  /** @ 引用的数据库（`<项目>/<环境>[/<库>[/<表>]]`）。 */
  dbRefs: string[]
  removeImage: (index: number) => void
  removeFile: (index: number) => void
  removeDbRef: (index: number) => void
  addImages: (picked: File[]) => void
  submit: () => void
  sendSuggestion: (text: string) => void
  /** 撤回一条排队插话：回填输入框与附件。 */
  recallQueued: (id: number) => void
  /** 把一条排队插话立即插进当前轮（steering）。 */
  steerQueued: (id: number) => void
  openImagePicker: () => void
  openFilePicker: () => void
  openDbRefPicker: () => void
  openCwdPicker: () => void
  /** 草稿态显示的待选工作目录。 */
  draftCwd: string
}

export const ChatPanelContext = createContext<ChatPanelData | null>(null)
