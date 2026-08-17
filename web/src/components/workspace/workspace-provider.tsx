import { useEffect, useMemo, useRef } from "react"
import type { DockviewApi } from "dockview-react"

import * as React from "react"

import { api, workspaceScopeApi, type WorkspaceScopeApi } from "@/lib/api"
import { applyLayoutPreset } from "@/components/workspace/layout-presets"
import {
  useWorkspace,
  WorkspaceContext,
  type GitSelection,
  type GitStoreState,
  type PreviewTarget,
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
  scope,
  draftCwd,
  children,
}: {
  sessionId: number
  /** 数据面作用域，默认普通会话；编排页传 api.orchestrator。 */
  scope?: WorkspaceScopeApi
  /**
   * 草稿态选定的工作目录。给了它，文件树与 git 面板立刻可用——看文件
   * 只需要一个目录，不该等到「发出第一条消息」把会话建出来（adr-002）。
   */
  draftCwd?: string
  children: React.ReactNode
}) {
  // 会话建好前后用不同的作用域，但面板看到的是同一套方法签名。
  const activeScope = React.useMemo(
    () =>
      sessionId
        ? (scope ?? api.sessions)
        : draftCwd
          ? draftWorkspaceScope(draftCwd)
          : (scope ?? api.sessions),
    [sessionId, scope, draftCwd]
  )
  const apiRef = useRef<DockviewApi | null>(null)
  const previewRef = useRef<PreviewTarget | null>(null)
  const listenersRef = useRef(new Set<() => void>())
  const gitRef = useRef<GitStoreState>({
    data: null,
    loading: false,
    error: null,
  })
  const gitListenersRef = useRef(new Set<() => void>())
  const refreshListenersRef = useRef(new Set<() => void>())
  const referenceSinkRef = useRef<((path: string) => void) | null>(null)
  const askSinkRef = useRef<((prompt: string) => void) | null>(null)
  // git 面板群的唯一选择事实源：refs 空 = 看 HEAD，sha null = 看工作区改动。
  const selectionRef = useRef<GitSelection>({ refs: [], sha: null })
  const selectionListenersRef = useRef(new Set<() => void>())

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
      scope: activeScope,
      /** 有目录可看就算就绪：会话态看会话的 cwd，草稿态看选定的目录。 */
      ready: sessionId > 0 || Boolean(draftCwd),
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
        void activeScope
          .terminalCreate(sessionId)
          .then((info) => addTerminalPanel(dock, info.id, info.num))
          .catch(() => {})
      },
      applyPreset: (preset) => {
        const dock = apiRef.current
        if (dock) applyLayoutPreset(dock, preset)
      },
      openPreview: (path) => {
        previewRef.current = { path, mode: "file" }
        ensureOpen("preview")
        listenersRef.current.forEach((l) => l())
      },
      openDiff: (path, sha) => {
        previewRef.current = { path, mode: "diff", sha }
        ensureOpen("preview")
        listenersRef.current.forEach((l) => l())
      },
      addReference: (path) => {
        referenceSinkRef.current?.(path)
      },
      attachReferenceSink: (sink) => {
        referenceSinkRef.current = sink
      },
      askAI: (prompt) => {
        askSinkRef.current?.(prompt)
      },
      attachAskSink: (sink) => {
        askSinkRef.current = sink
      },
      selectRefs: (refs) => {
        // 最多两个：一个看历史，两个对比。第三次点击顶掉最早的那个。
        selectionRef.current = {
          refs: refs.slice(-2),
          // 换分支时旧提交多半已不在新链路里，回到「工作区改动」这个安全落点。
          sha: null,
        }
        notifySelection()
      },
      selectCommit: (sha) => {
        selectionRef.current = { ...selectionRef.current, sha }
        notifySelection()
      },
      selectionStore: {
        subscribe: (listener) => {
          selectionListenersRef.current.add(listener)
          return () => selectionListenersRef.current.delete(listener)
        },
        get: () => selectionRef.current,
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

    function notifySelection() {
      selectionListenersRef.current.forEach((l) => l())
    }

    function refreshGit() {
      if (!sessionId) return
      const notify = () => gitListenersRef.current.forEach((l) => l())
      gitRef.current = { ...gitRef.current, loading: true }
      notify()
      activeScope
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
  }, [sessionId, activeScope, draftCwd])

  return (
    <WorkspaceContext.Provider value={value}>
      {children}
    </WorkspaceContext.Provider>
  )
}

/** 页面层把「问 AI」落点注册进命令总线的行为组件，不渲染任何内容。 */
export function WorkspaceAskSink({
  onAsk,
}: {
  onAsk: (prompt: string) => void
}) {
  const ws = useWorkspace()
  useEffect(() => {
    ws.attachAskSink(onAsk)
    return () => ws.attachAskSink(null)
  }, [ws, onAsk])
  return null
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

/** 草稿态作用域：同一套方法，目录走 `?cwd=`（见 lib/api 的 workspaceScopeApi）。 */
function draftWorkspaceScope(cwd: string): WorkspaceScopeApi {
  return workspaceScopeApi("/sessions", cwd)
}
