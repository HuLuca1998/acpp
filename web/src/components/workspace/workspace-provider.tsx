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
  addTerminalPanel,
  addWorkspacePanel,
  type WorkspacePanelKind,
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
  const referenceSinkRef = useRef<((path: string) => void) | null>(null)

  const value = useMemo<WorkspaceValue>(() => {
    const ensureOpen = (id: WorkspacePanelKind) => {
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
        const dock = apiRef.current
        const panel = dock?.getPanel(id)
        if (dock && panel) dock.removePanel(panel)
        // 关闭终端 tab = 杀 pty 的诚实语义；其余路径靠 detach 兜底回收。
        if (id.startsWith("terminal:") && sessionId) {
          void api.sessions
            .terminalRemove(sessionId, id.slice("terminal:".length))
            .catch(() => {})
        }
      },
      newTerminal: () => {
        const dock = apiRef.current
        if (!dock || !sessionId) return
        void api.sessions
          .terminalCreate(sessionId)
          .then((info) => addTerminalPanel(dock, info.id, info.num))
          .catch(() => {})
      },
      openPreview: (path) => {
        previewRef.current = path
        ensureOpen("preview")
        listenersRef.current.forEach((l) => l())
      },
      addReference: (path) => {
        referenceSinkRef.current?.(path)
      },
      attachReferenceSink: (sink) => {
        referenceSinkRef.current = sink
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

/** 页面层把「加引用」落点注册进命令总线的行为组件，不渲染任何内容。 */
export function WorkspaceReferenceSink({
  onAdd,
}: {
  onAdd: (path: string) => void
}) {
  const ws = useWorkspace()
  useEffect(() => {
    ws.attachReferenceSink(onAdd)
    return () => ws.attachReferenceSink(null)
  }, [ws, onAdd])
  return null
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
