import type { DockviewApi } from "dockview-react"
import {
  FileDiffIcon,
  FileTextIcon,
  FolderTreeIcon,
  GitCommitHorizontalIcon,
  MessageSquareIcon,
  TerminalIcon,
  type LucideIcon,
} from "lucide-react"

/** 工作区六类面板。chat 不可关闭、不参与显隐菜单。 */
export type WorkspacePanelId =
  "chat" | "files" | "preview" | "diff" | "commits" | "terminal"

/** ⋯ 菜单里可显隐的面板（顺序即菜单顺序）。 */
export const TOGGLEABLE_PANELS = [
  "files",
  "preview",
  "diff",
  "commits",
  "terminal",
] as const satisfies readonly WorkspacePanelId[]

export const PANEL_ICONS: Record<WorkspacePanelId, LucideIcon> = {
  chat: MessageSquareIcon,
  files: FolderTreeIcon,
  preview: FileTextIcon,
  diff: FileDiffIcon,
  commits: GitCommitHorizontalIcon,
  terminal: TerminalIcon,
}

/**
 * 打开一个不在布局里的面板：优先并进现有工具组（任一非 chat 面板所在组），
 * 一个不剩时在 chat 右侧新开一组。位置记忆的精确恢复留到 M4。
 */
export function addWorkspacePanel(api: DockviewApi, id: WorkspacePanelId) {
  const anchor = TOGGLEABLE_PANELS.filter((p) => p !== id)
    .map((p) => api.getPanel(p))
    .find((p) => p !== undefined)
  api.addPanel({
    id,
    component: id,
    position: anchor
      ? { referencePanel: anchor.id, direction: "within" }
      : { referencePanel: "chat", direction: "right" },
  })
}
