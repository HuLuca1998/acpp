import { createContext, useContext } from "react"

import type { useChat } from "@/hooks/use-chat"
import type { useDraftSession } from "@/hooks/use-draft-session"
import type { ImageAttachment } from "@/types/acp"

/**
 * 输入框草稿的微型 store。打字是最高频的用户动作，draft 若是页面
 * state，每个按键都会重渲整棵工作区树（消息流、文件树、子代理面板
 * 全部跟着）——低配机器上空格要零点几秒才上屏就是这么来的。ref 化
 * 之后只有订阅它的对话面板重渲，页面层保持安静。
 */
export interface DraftStore {
  get: () => string
  /** 支持函数式更新（回填、追加）。 */
  set: (next: string | ((prev: string) => string)) => void
  subscribe: (listener: () => void) => () => void
}

export function createDraftStore(): DraftStore {
  let value = ""
  const listeners = new Set<() => void>()
  return {
    get: () => value,
    set: (next) => {
      const resolved = typeof next === "function" ? next(value) : next
      if (resolved === value) return
      value = resolved
      listeners.forEach((listener) => listener())
    },
    subscribe: (listener) => {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
  }
}

/**
 * 页面级状态经此 context 注入对话面板：会话流（useChat）、草稿会话、
 * 输入与附件。dockview 重挂载面板时状态不丢——它一直住在页面层。
 */
export interface ChatPanelData {
  isNew: boolean
  chat: ReturnType<typeof useChat>
  newSession: ReturnType<typeof useDraftSession>
  draftStore: DraftStore
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
  submit: () => void
  sendSuggestion: (text: string) => void
  /** 撤回一条排队插话：回填输入框与附件。 */
  recallQueued: (id: number) => void
  /** 把一条排队插话立即插进当前轮（steering）。 */
  steerQueued: (id: number) => void
  openImagePicker: () => void
  openFilePicker: () => void
  openDbRefPicker: () => void
  /** 打开上传本机文件的对话框。 */
  openUpload: () => void
  openCwdPicker: () => void
  /** 草稿态显示的待选工作目录。 */
  draftCwd: string
}

export const ChatPanelContext = createContext<ChatPanelData | null>(null)

/** 取页面注入的会话流。对话面板与子代理面板共用同一份状态。 */
export function useChatPanel(): ChatPanelData {
  const value = useContext(ChatPanelContext)
  if (!value) throw new Error("ChatPanel must be used within ChatPanelContext")
  return value
}
