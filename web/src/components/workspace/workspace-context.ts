import { createContext, useContext, useSyncExternalStore } from "react"
import type { DockviewApi } from "dockview-react"

import type { WorkspaceScopeApi } from "@/lib/api"
import type { GitOverview } from "@/types/acp"
import type { LayoutPreset } from "@/components/workspace/layout-presets"
import type { WorkspacePanelKind } from "@/components/workspace/workspace-panels"

/**
 * 文件查看器要看什么。mode 决定形态：file 看当前内容，diff 看改动前后。
 * 两种形态共用一个面板——「这个文件现在什么样」与「它改了什么」是同一个
 * 阅读动作的两面，分成两个面板只会让人来回找 tab。
 */
export interface PreviewTarget {
  path: string
  mode: "file" | "diff"
  /** diff 模式下的提交；空表示工作区里尚未提交的改动。 */
  sha?: string
}

/**
 * git 面板群共享的选择态（adr-002 M4）。
 *
 * 四个 git 面板不互相说话，全部读这一份选择：分支面板改 refs，链路面板
 * 改 sha，变更与详情面板只读。这样「选了什么」永远只有一个事实源，
 * 面板可以任意开关、任意摆放，也可以只开其中一个。
 */
export interface GitSelection {
  /** 选中的分支/标签。一个 = 看这条 ref 的历史，两个 = 进入对比模式。 */
  refs: string[]
  /** 选中的提交；null 表示看工作区里尚未提交的改动（链路顶部那条）。 */
  sha: string | null
}

/** git 汇总的加载态快照：引用不可变，更新即换新对象。 */
export interface GitStoreState {
  data: GitOverview | null
  loading: boolean
  error: string | null
}

/**
 * 工作区命令总线：面板间不直接对话，联动动作一律走这里的命令。
 * 值引用稳定（provider 里 useMemo 只依赖 sessionId），面板订阅它
 * 不会被聊天流的高频重渲染牵连。预览路径用外部 store（ref + listeners）
 * 承载，只有订阅了 usePreviewPath 的组件会因它重渲染。
 */
export interface WorkspaceValue {
  /** 草稿态（会话未创建）为 0。 */
  sessionId: number
  /**
   * 工作区数据面能否使用：会话态永远能，草稿态取决于用户选没选工作目录。
   * 面板用它决定画空态还是画内容——「有没有会话」不是判据，「有没有
   * 目录」才是。
   */
  ready: boolean
  /** 工作区数据面的作用域 API：普通会话与编排主会话只差路径前缀。 */
  scope: WorkspaceScopeApi
  /** dock ready 时注入 dockview api；卸载时传 null。 */
  attachApi: (api: DockviewApi | null) => void
  getApi: () => DockviewApi | null
  /** 确保面板存在并激活：不在布局里则按默认落点创建。 */
  ensureOpen: (id: WorkspacePanelKind) => void
  /** 面板当前是否在布局里。 */
  isOpen: (id: WorkspacePanelKind) => boolean
  /** 关闭（移除）一个面板；chat 不受理。终端面板顺带杀掉 pty。 */
  closePanel: (id: string) => void
  /** 新建一个终端实例：后端 spawn pty 后落成 `terminal:<id>` 面板。 */
  newTerminal: () => void
  /** 应用内置布局预设（终端实例原样保留）。 */
  applyPreset: (preset: LayoutPreset) => void
  /** 打开文件预览：自动 ensureOpen 查看器面板并切到该文件。 */
  openPreview: (path: string) => void
  /** 在查看器里以 diff 模式打开：sha 为空看工作区改动，否则看那条提交。 */
  openDiff: (path: string, sha?: string) => void
  /** 下载工作区里的一个文件（浏览器另存为）。 */
  downloadFile: (path: string) => void
  /** 把文件/文件夹加进 composer 的 @ 引用（由页面层注册实现）。 */
  addReference: (path: string) => void
  /** 页面层注册 addReference 的落点；卸载时传 null。 */
  attachReferenceSink: (sink: ((path: string) => void) | null) => void
  previewStore: {
    subscribe: (listener: () => void) => () => void
    get: () => PreviewTarget | null
  }
  /** 选中分支/标签（传两个进对比模式）。选择变化会带动链路与变更面板。 */
  selectRefs: (refs: string[]) => void
  /** 选中提交；null = 回到工作区未提交改动。 */
  selectCommit: (sha: string | null) => void
  selectionStore: {
    subscribe: (listener: () => void) => () => void
    get: () => GitSelection
  }
  /**
   * 把一段结构化 prompt 送进输入框（由页面层注册落点）。
   * **只填不发**——发消息是用户的动作，我们只负责把话写好。
   */
  askAI: (prompt: string) => void
  /** 页面层注册 askAI 的落点；卸载时传 null。 */
  attachAskSink: (sink: ((prompt: string) => void) | null) => void
  /** 重取 git 汇总（变更面板与 tab 徽标共享一份数据）。 */
  refreshGit: () => void
  gitStore: {
    subscribe: (listener: () => void) => () => void
    get: () => GitStoreState
  }
  /** 刷新整个工作区数据面：git 汇总 + 广播给订阅方（文件树重拉）。 */
  refreshWorkspace: () => void
  onWorkspaceRefresh: (listener: () => void) => () => void
}

export const WorkspaceContext = createContext<WorkspaceValue | null>(null)

export function useWorkspace(): WorkspaceValue {
  const value = useContext(WorkspaceContext)
  if (!value)
    throw new Error("useWorkspace must be used within WorkspaceProvider")
  return value
}

/** 当前要看的文件与形态；只有文件查看器面板订阅它。 */
export function usePreviewTarget(): PreviewTarget | null {
  const ws = useWorkspace()
  return useSyncExternalStore(ws.previewStore.subscribe, ws.previewStore.get)
}

/** git 汇总快照；只有 diff / commit 面板与 tab 徽标订阅它。 */
export function useGitOverview(): GitStoreState {
  const ws = useWorkspace()
  return useSyncExternalStore(ws.gitStore.subscribe, ws.gitStore.get)
}

/** 订阅 git 面板群的选择态。只有真正用到选择的面板会因它重渲染。 */
export function useGitSelection(): GitSelection {
  const ws = useWorkspace()
  return useSyncExternalStore(
    ws.selectionStore.subscribe,
    ws.selectionStore.get
  )
}
