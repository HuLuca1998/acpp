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
  draftCwd,
  children,
}: {
  sessionId: number
  /**
   * 草稿态选定的工作目录。给了它，文件树与 git 面板立刻可用——看文件
   * 只需要一个目录，不该等到「发出第一条消息」把会话建出来（adr-002）。
   */
  draftCwd?: string
  children: React.ReactNode
}) {
  // 会话建好前后用不同的作用域，但面板看到的是同一套方法签名。
  //
  // 判据是 `!== undefined` 而不是真假值：草稿态没手动选目录时 draftCwd 是
  // **空串**，那不是「没有草稿」，而是「用这个身份的默认起点」——后端对
  // `?cwd=` 空值本来就落到 Home(scope)（owner 是工作区根、访客是自己的
  // root）。按真假值判会把这种最常见的情形当成没会话，整个工作区面板群
  // 一起哑掉。
  const activeScope = React.useMemo(
    () =>
      !sessionId && draftCwd !== undefined
        ? draftWorkspaceScope(draftCwd)
        : api.sessions,
    [sessionId, draftCwd]
  )
  const apiRef = useRef<DockviewApi | null>(null)
  // 稳定引用：dock 拿它做「卸载时摘句柄」的 effect 依赖，跟着 value 一起
  // 换新会让每次 sessionId/scope 变化都触发一次「摘句柄」，而挂句柄只在
  // dockview 首次 ready 时发生一次——摘掉就再也接不回来了。
  const attachApi = React.useCallback((next: DockviewApi | null) => {
    apiRef.current = next
  }, [])
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
      /**
       * 有目录可看就算就绪：会话态看会话的 cwd，草稿态看选定的目录——
       * 没选也算数，空 cwd 由后端落到默认起点（见上面 activeScope 的注释）。
       * 真正需要会话的面板（日志、子代理、终端）各自看 sessionId，不看这个。
       */
      ready: sessionId > 0 || draftCwd !== undefined,
      attachApi,
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
      openPreview: (path, line) => {
        previewRef.current = { path, mode: "file", line }
        ensureOpen("preview")
        listenersRef.current.forEach((l) => l())
      },
      openDiff: (path, sha) => {
        previewRef.current = { path, mode: "diff", sha }
        ensureOpen("preview")
        listenersRef.current.forEach((l) => l())
      },
      downloadFile: (path, archive) => {
        // 造一个一次性链接点掉：download 属性让浏览器走「另存为」而不是
        // 在标签页里打开，后端的 Content-Disposition 决定最终文件名。
        const link = document.createElement("a")
        link.href = activeScope.downloadUrl(sessionId, path, archive)
        const base = path.split("/").pop() ?? "download"
        link.download = archive ? `${base}.zip` : base
        document.body.appendChild(link)
        link.click()
        link.remove()
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

/** 轮内刷新的合帧窗口：一次工具调用落地到界面跟上，最多等这么久。 */
const TOOL_REFRESH_MS = 1500

/**
 * 刷新工作区数据面的时机。挂在 Provider 内任意处的行为组件，不渲染内容。
 *
 * 两个触发点：
 *  - turn 结束（busy 从 true 落回 false）——agent 收工的时刻；
 *  - 轮内每完成一次工具调用——agent 干活是一整轮里陆续发生的，等到轮末
 *    才刷新意味着它切了分支、改了文件之后，底部分支、变更清单、文件树
 *    全在说旧话，一直说到这一轮结束（长任务能是十几分钟）。
 *
 * 工具调用一轮能来几十次，每次都 exec 一遍 git 不值当——按窗口合帧，
 * 密集调用期间也只刷最后那一下。
 */
export function WorkspaceAutoRefresh({
  busy,
  toolsDone,
}: {
  busy: boolean
  /** 本轮已完成的工具调用数；每 +1 表示 agent 刚干完一件事。 */
  toolsDone?: number
}) {
  const ws = useWorkspace()
  const prev = useRef(busy)
  useEffect(() => {
    if (prev.current && !busy) ws.refreshWorkspace()
    prev.current = busy
  }, [busy, ws])

  const seenTools = useRef(toolsDone ?? 0)
  useEffect(() => {
    const done = toolsDone ?? 0
    // 轮末清空 liveTools 会让计数落回 0，那一下由上面的 busy 分支管，
    // 这里不重复刷。
    if (done <= seenTools.current) {
      seenTools.current = done
      return
    }
    seenTools.current = done
    const timer = setTimeout(() => ws.refreshWorkspace(), TOOL_REFRESH_MS)
    return () => clearTimeout(timer)
  }, [toolsDone, ws])
  return null
}

/** 草稿态作用域：同一套方法，目录走 `?cwd=`（见 lib/api 的 workspaceScopeApi）。 */
function draftWorkspaceScope(cwd: string): WorkspaceScopeApi {
  return workspaceScopeApi("/sessions", cwd)
}
