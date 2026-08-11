import { useEffect, useMemo, useRef } from "react"
import type { DockviewApi } from "dockview-react"

import { api } from "@/lib/api"
import {
  useWorkspace,
  WorkspaceContext,
  type GitStoreState,
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
  const gitRef = useRef<GitStoreState>({
    data: null,
    loading: false,
    error: null,
  })
  const gitListenersRef = useRef(new Set<() => void>())
  const refreshListenersRef = useRef(new Set<() => void>())

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
      refreshGit,
      gitStore: {
        subscribe: (listener) => {
          gitListenersRef.current.add(listener)
          return () => gitListenersRef.current.delete(listener)
        },
        get: () => gitRef.current,
      },
      refreshWorkspace: () => {
        refreshGit()
        refreshListenersRef.current.forEach((l) => l())
      },
      onWorkspaceRefresh: (listener) => {
        refreshListenersRef.current.add(listener)
        return () => refreshListenersRef.current.delete(listener)
      },
    }

    function refreshGit() {
      if (!sessionId) return
      const notify = () => gitListenersRef.current.forEach((l) => l())
      gitRef.current = { ...gitRef.current, loading: true }
      notify()
      api.sessions
        .gitOverview(sessionId)
        .then((data) => {
          gitRef.current = { data, loading: false, error: null }
        })
        .catch((err) => {
          gitRef.current = {
            ...gitRef.current,
            loading: false,
            error: err instanceof Error ? err.message : String(err),
          }
        })
        .finally(notify)
    }
  }, [sessionId])

  return (
    <WorkspaceContext.Provider value={value}>
      {children}
    </WorkspaceContext.Provider>
  )
}

/**
 * turn 结束（busy 从 true 落回 false）时刷新工作区数据面——agent 刚改完
 * 文件的时刻。挂在 Provider 内任意处的行为组件，不渲染任何内容。
 */
export function WorkspaceAutoRefresh({ busy }: { busy: boolean }) {
  const ws = useWorkspace()
  const prev = useRef(busy)
  useEffect(() => {
    if (prev.current && !busy) ws.refreshWorkspace()
    prev.current = busy
  }, [busy, ws])
  return null
}
