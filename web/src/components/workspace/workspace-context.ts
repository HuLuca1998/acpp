import { createContext, useContext, useSyncExternalStore } from "react"
import type { DockviewApi } from "dockview-react"

import type { WorkspacePanelId } from "@/components/workspace/workspace-panels"

/**
 * 工作区命令总线：面板间不直接对话，联动动作一律走这里的命令。
 * 值引用稳定（provider 里 useMemo 只依赖 sessionId），面板订阅它
 * 不会被聊天流的高频重渲染牵连。预览路径用外部 store（ref + listeners）
 * 承载，只有订阅了 usePreviewPath 的组件会因它重渲染。
 */
export interface WorkspaceValue {
  /** 草稿态（会话未创建）为 0，面板据此显示「创建后可用」空态。 */
  sessionId: number
  /** dock ready 时注入 dockview api；卸载时传 null。 */
  attachApi: (api: DockviewApi | null) => void
  getApi: () => DockviewApi | null
  /** 确保面板存在并激活：不在布局里则按默认落点创建。 */
  ensureOpen: (id: WorkspacePanelId) => void
  /** 面板当前是否在布局里。 */
  isOpen: (id: WorkspacePanelId) => boolean
  /** 关闭（移除）一个面板；chat 不受理。 */
  closePanel: (id: WorkspacePanelId) => void
  /** 打开文件预览：自动 ensureOpen 预览面板并切到该文件。 */
  openPreview: (path: string) => void
  previewStore: {
    subscribe: (listener: () => void) => () => void
    get: () => string | null
  }
}

export const WorkspaceContext = createContext<WorkspaceValue | null>(null)

export function useWorkspace(): WorkspaceValue {
  const value = useContext(WorkspaceContext)
  if (!value)
    throw new Error("useWorkspace must be used within WorkspaceProvider")
  return value
}

/** 当前预览的文件路径；只有预览面板订阅它。 */
export function usePreviewPath(): string | null {
  const ws = useWorkspace()
  return useSyncExternalStore(ws.previewStore.subscribe, ws.previewStore.get)
}
