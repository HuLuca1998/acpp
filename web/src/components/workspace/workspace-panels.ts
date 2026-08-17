import type {
  AddPanelOptions,
  DockviewApi,
  IDockviewPanel,
} from "dockview-react"
import {
  FileDiffIcon,
  FileTextIcon,
  FolderTreeIcon,
  GitBranchIcon,
  GitCommitHorizontalIcon,
  InfoIcon,
  MessageSquareIcon,
  ScrollTextIcon,
  TerminalIcon,
  type LucideIcon,
} from "lucide-react"

/**
 * 工作区面板类型。chat 不可关闭、不参与显隐菜单；terminal 可多实例，
 * 面板 id 形如 `terminal:<termId>`，其余类型面板 id 即类型名。
 */
export type WorkspacePanelKind =
  | "chat"
  | "files"
  | "preview"
  | "branches"
  | "history"
  | "changes"
  | "detail"
  | "logs"
  | "terminal"

/** ⋯ 菜单里勾选显隐的单例面板（终端是实例列表，单独渲染）。 */
export const TOGGLEABLE_PANELS = [
  "files",
  "preview",
  "branches",
  "history",
  "changes",
  "detail",
  "logs",
] as const satisfies readonly WorkspacePanelKind[]

export const PANEL_ICONS: Record<WorkspacePanelKind, LucideIcon> = {
  chat: MessageSquareIcon,
  files: FolderTreeIcon,
  preview: FileTextIcon,
  branches: GitBranchIcon,
  history: GitCommitHorizontalIcon,
  changes: FileDiffIcon,
  detail: InfoIcon,
  logs: ScrollTextIcon,
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

/** 打开一个不在布局里的单例面板。位置记忆的精确恢复留作后续优化。 */
export function addWorkspacePanel(api: DockviewApi, id: WorkspacePanelKind) {
  const position = workspacePanelPosition(api, id)
  api.addPanel({
    id,
    component: id,
    position,
  })
  sizeNewSideGroup(api, id, position)
}

/** 打开一个终端实例面板（renderer always：切走 tab 不丢终端状态）。 */
export function addTerminalPanel(
  api: DockviewApi,
  termId: string,
  num: number
) {
  const position = workspacePanelPosition(api)
  api.addPanel({
    id: `terminal:${termId}`,
    component: "terminal",
    renderer: "always",
    params: { termId, num },
    position,
  })
  sizeNewSideGroup(api, `terminal:${termId}`, position)
}

/**
 * 从纯对话布局里首次唤起面板时会在 chat 右侧新开一组，dockview 默认
 * 对半分——把新组收窄到约 1/4，对话仍是主角；并进现有工具组时不动。
 */
function sizeNewSideGroup(
  api: DockviewApi,
  id: string,
  position: AddPanelOptions["position"]
) {
  const ref = (position as { referencePanel?: string } | undefined)
    ?.referencePanel
  if (ref === "chat" && api.width > 0) {
    api.getPanel(id)?.api.setSize({ width: Math.round(api.width * 0.25) })
  }
}
