import type {
  AddPanelOptions,
  DockviewApi,
  IDockviewPanel,
} from "dockview-react"
import {
  FileDiffIcon,
  FileTextIcon,
  FolderTreeIcon,
  GitCommitHorizontalIcon,
  MessageSquareIcon,
  TerminalIcon,
  type LucideIcon,
} from "lucide-react"

/**
 * 工作区面板类型。chat 不可关闭、不参与显隐菜单；terminal 可多实例，
 * 面板 id 形如 `terminal:<termId>`，其余类型面板 id 即类型名。
 */
export type WorkspacePanelKind =
  "chat" | "files" | "preview" | "diff" | "commits" | "terminal"

/** ⋯ 菜单里勾选显隐的单例面板（终端是实例列表，单独渲染）。 */
export const TOGGLEABLE_PANELS = [
  "files",
  "preview",
  "diff",
  "commits",
] as const satisfies readonly WorkspacePanelKind[]

export const PANEL_ICONS: Record<WorkspacePanelKind, LucideIcon> = {
  chat: MessageSquareIcon,
  files: FolderTreeIcon,
  preview: FileTextIcon,
  diff: FileDiffIcon,
  commits: GitCommitHorizontalIcon,
  terminal: TerminalIcon,
}

/** 从面板 id 得到类型：`terminal:*` 归 terminal，其余 id 即类型。 */
export function panelKindOf(id: string): WorkspacePanelKind {
  if (id.startsWith("terminal:")) return "terminal"
  return id as WorkspacePanelKind
}

/** 新面板的默认落点：优先并进现有工具组，一个不剩时在 chat 右侧开新组。 */
export function workspacePanelPosition(
  api: DockviewApi,
  excludeId?: string
): AddPanelOptions["position"] {
  const anchor: IDockviewPanel | undefined = api.panels.find(
    (p) => p.id !== "chat" && p.id !== excludeId
  )
  return anchor
    ? { referencePanel: anchor.id, direction: "within" }
    : { referencePanel: "chat", direction: "right" }
}

/** 打开一个不在布局里的单例面板。位置记忆的精确恢复留到 M4。 */
export function addWorkspacePanel(api: DockviewApi, id: WorkspacePanelKind) {
  api.addPanel({
    id,
    component: id,
    position: workspacePanelPosition(api, id),
  })
}

/** 打开一个终端实例面板（renderer always：切走 tab 不丢终端状态）。 */
export function addTerminalPanel(
  api: DockviewApi,
  termId: string,
  num: number
) {
  api.addPanel({
    id: `terminal:${termId}`,
    component: "terminal",
    renderer: "always",
    params: { termId, num },
    position: workspacePanelPosition(api),
  })
}
