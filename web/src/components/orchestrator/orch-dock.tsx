import { memo, useCallback, useEffect } from "react"
import { useTranslation } from "react-i18next"
import {
  DockviewReact,
  type DockviewApi,
  type DockviewReadyEvent,
  type DockviewTheme,
  type IDockviewHeaderActionsProps,
  type IDockviewPanelHeaderProps,
  type IDockviewPanelProps,
} from "dockview-react"
import "dockview/dist/styles/dockview.css"
import { ListTodoIcon, MessagesSquareIcon, XIcon } from "lucide-react"

import { OrchMainPanel } from "@/components/orchestrator/main-panel"
import { OrchTaskPanel } from "@/components/orchestrator/task-panel"
import { OrchTasksPanel } from "@/components/orchestrator/tasks-panel"
import { useOrchCtx } from "@/components/orchestrator/orch-context"
import { CommitsPanel } from "@/components/workspace/panels/commits-panel"
import { DiffPanel } from "@/components/workspace/panels/diff-panel"
import { FilePreviewPanel } from "@/components/workspace/panels/file-preview-panel"
import { FileTreePanel } from "@/components/workspace/panels/file-tree-panel"
import { LogsPanel } from "@/components/workspace/panels/logs-panel"
import { TerminalPanel } from "@/components/workspace/panels/terminal-panel"
import { useWorkspace } from "@/components/workspace/workspace-context"
import { WorkspaceMenu } from "@/components/workspace/workspace-menu"
import {
  PANEL_ICONS,
  panelKindOf,
} from "@/components/workspace/workspace-panels"

/** 与工作区同皮肤（变量映射见 index.css 的 .dockview-theme-acpp 块）。 */
const ACPP_THEME: DockviewTheme = {
  name: "acpp",
  className: "dockview-theme-acpp",
  gap: 6,
  dndTabIndicator: "line",
  tabGroupIndicator: "none",
  tabAnimation: "smooth",
}

/**
 * 编排页的面板注册表：编排专属（chat=主控对话 / tasks / task:<id>）+
 * 普通会话工作区的全部数据面板（升级不降级）。chat 用同名 id 是刻意的
 * ——workspace 的落点/菜单逻辑以 chat 为锚。
 */
const COMPONENTS: Record<
  string,
  React.FunctionComponent<IDockviewPanelProps>
> = {
  chat: OrchMainPanel,
  tasks: OrchTasksPanel,
  task: OrchTaskPanel,
  files: FileTreePanel,
  preview: FilePreviewPanel,
  diff: DiffPanel,
  commits: CommitsPanel,
  logs: LogsPanel,
  terminal: TerminalPanel,
}

function PanelTab(props: IDockviewPanelHeaderProps) {
  const { t } = useTranslation()
  const ws = useWorkspace()
  const { chat } = useOrchCtx()
  const id = props.api.id

  let label: string
  let icon: React.ReactNode
  if (id === "chat") {
    label = t("orch.panels.main")
    icon = <MessagesSquareIcon className="size-3.5" />
  } else if (id === "tasks") {
    label = t("orch.panels.tasks")
    icon = <ListTodoIcon className="size-3.5" />
  } else if (id.startsWith("task:")) {
    const taskId = (props.params as { taskId?: number })?.taskId
    const task = chat.tasks.find((item) => item.id === taskId)
    label = task
      ? `${task.roleName} #${task.id}`
      : t("orch.panels.taskFallback")
    icon = <ListTodoIcon className="size-3.5" />
  } else {
    const kind = panelKindOf(id)
    const Icon = PANEL_ICONS[kind] ?? PANEL_ICONS.files
    const num = (props.params as { num?: number })?.num
    label =
      kind === "terminal" && num
        ? `${t("workspace.panels.terminal")} ${num}`
        : t(`workspace.panels.${kind}` as never)
    icon = <Icon className="size-3.5" />
  }

  const closable = id !== "chat" && id !== "tasks"
  return (
    <div
      className="flex h-full items-center gap-1.5 px-2 text-xs"
      title={label}
    >
      <span className="shrink-0">{icon}</span>
      <span className="acpp-tab-label truncate">{label}</span>
      {closable ? (
        <button
          type="button"
          aria-label={t("workspace.closePanel")}
          className="acpp-tab-label -mr-0.5 flex size-4 items-center justify-center rounded-sm text-muted-foreground/60 hover:bg-muted hover:text-foreground"
          onPointerDown={(e) => e.stopPropagation()}
          onClick={(e) => {
            e.stopPropagation()
            // 工作区面板走命令总线（终端要顺带杀 pty）；任务面板直接关。
            if (id.startsWith("task:")) props.api.close()
            else ws.closePanel(id)
          }}
        >
          <XIcon className="size-3" />
        </button>
      ) : null}
    </div>
  )
}

/** 组头右侧动作：主控对话所在组渲染 ⋯ 面板菜单（与普通会话同款）。 */
function HeaderActions(props: IDockviewHeaderActionsProps) {
  const hasChat = props.panels.some((p) => p.id === "chat")
  if (!hasChat) return null
  return <OrchMenuSlot />
}

function OrchMenuSlot() {
  const ws = useWorkspace()
  return (
    <div className="flex h-full items-center pr-1.5">
      <WorkspaceMenu
        onResetLayout={() => {
          const api = ws.getApi()
          if (api) {
            api.clear()
            buildDefaultLayout(api)
          }
        }}
      />
    </div>
  )
}

function buildDefaultLayout(api: DockviewApi) {
  api.addPanel({ id: "chat", component: "chat" })
  api.addPanel({
    id: "tasks",
    component: "tasks",
    position: { referencePanel: "chat", direction: "right" },
    initialWidth: 340,
  })
}

/**
 * 编排页 docking 容器：主控对话 + 常驻任务列表 + 完整工作区面板
 * （文件树/预览/diff/commits/日志/终端，经 ⋯ 菜单开）。任务子会话面板
 * 由列表点开（不自动弹出），可拖动布局、可关闭（任务照跑）。
 */
export const OrchDock = memo(function OrchDock() {
  const ws = useWorkspace()

  useEffect(() => {
    return () => ws.attachApi(null)
  }, [ws])

  const onReady = useCallback(
    (event: DockviewReadyEvent) => {
      ws.attachApi(event.api)
      buildDefaultLayout(event.api)
      // 主控对话与任务列表不可拖出（布局锚点），其余面板自由。
      event.api.onWillDragPanel((e) => {
        if (
          (e.panel.id === "chat" || e.panel.id === "tasks") &&
          e.nativeEvent instanceof DragEvent
        ) {
          e.nativeEvent.preventDefault()
        }
      })
    },
    [ws]
  )

  return (
    <DockviewReact
      className="h-full w-full"
      theme={ACPP_THEME}
      components={COMPONENTS}
      defaultTabComponent={PanelTab}
      rightHeaderActionsComponent={HeaderActions}
      onReady={onReady}
    />
  )
})
