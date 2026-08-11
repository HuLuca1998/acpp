import { useMemo, useRef } from "react"
import type { DockviewApi } from "dockview-react"

import {
  WorkspaceContext,
  type WorkspaceValue,
} from "@/components/workspace/workspace-context"
import {
  addWorkspacePanel,
  type WorkspacePanelId,
} from "@/components/workspace/workspace-panels"

/** 命令总线的宿主：状态全在 ref 里，provider 本身不因命令重渲染。 */
export function WorkspaceProvider({
  sessionId,
  children,
}: {
  sessionId: number
  children: React.ReactNode
}) {
  const apiRef = useRef<DockviewApi | null>(null)
  const previewRef = useRef<string | null>(null)
  const listenersRef = useRef(new Set<() => void>())

  const value = useMemo<WorkspaceValue>(() => {
    const ensureOpen = (id: WorkspacePanelId) => {
      const api = apiRef.current
      if (!api) return
      const panel = api.getPanel(id)
      if (panel) {
        panel.api.setActive()
        return
      }
      addWorkspacePanel(api, id)
    }
    return {
      sessionId,
      attachApi: (api) => {
        apiRef.current = api
      },
      getApi: () => apiRef.current,
      ensureOpen,
      isOpen: (id) => apiRef.current?.getPanel(id) !== undefined,
      closePanel: (id) => {
        if (id === "chat") return
        const api = apiRef.current
        const panel = api?.getPanel(id)
        if (api && panel) api.removePanel(panel)
      },
      openPreview: (path) => {
        previewRef.current = path
        ensureOpen("preview")
        listenersRef.current.forEach((l) => l())
      },
      previewStore: {
        subscribe: (listener) => {
          listenersRef.current.add(listener)
          return () => listenersRef.current.delete(listener)
        },
        get: () => previewRef.current,
      },
    }
  }, [sessionId])

  return (
    <WorkspaceContext.Provider value={value}>
      {children}
    </WorkspaceContext.Provider>
  )
}
