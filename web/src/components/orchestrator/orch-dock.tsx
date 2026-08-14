import { memo, useCallback } from "react"
import { useTranslation } from "react-i18next"
import {
  DockviewReact,
  type DockviewApi,
  type DockviewReadyEvent,
  type DockviewTheme,
  type IDockviewPanelHeaderProps,
  type IDockviewPanelProps,
} from "dockview-react"
import "dockview/dist/styles/dockview.css"
import { ListTodoIcon, MessagesSquareIcon, XIcon } from "lucide-react"

import { OrchMainPanel } from "@/components/orchestrator/main-panel"
import { OrchTaskPanel } from "@/components/orchestrator/task-panel"
import { OrchTasksPanel } from "@/components/orchestrator/tasks-panel"
import { useOrchCtx } from "@/components/orchestrator/orch-context"

/** 与工作区同皮肤（变量映射见 index.css 的 .dockview-theme-acpp 块）。 */
const ACPP_THEME: DockviewTheme = {
  name: "acpp",
  className: "dockview-theme-acpp",
  gap: 6,
  dndTabIndicator: "line",
  tabGroupIndicator: "none",
  tabAnimation: "smooth",
}

const COMPONENTS: Record<
  string,
  React.FunctionComponent<IDockviewPanelProps>
> = {
  main: OrchMainPanel,
  tasks: OrchTasksPanel,
  task: OrchTaskPanel,
}

function PanelTab(props: IDockviewPanelHeaderProps) {
  const { t } = useTranslation()
  const { chat } = useOrchCtx()
  const id = props.api.id

  let label: string
  let icon: React.ReactNode
  if (id === "main") {
    label = t("orch.panels.main")
    icon = <MessagesSquareIcon className="size-3.5" />
  } else if (id === "tasks") {
    label = t("orch.panels.tasks")
    icon = <ListTodoIcon className="size-3.5" />
  } else {
    const taskId = (props.params as { taskId?: number })?.taskId
    const task = chat.tasks.find((item) => item.id === taskId)
    label = task
      ? `${task.roleName} #${task.id}`
      : t("orch.panels.taskFallback")
    icon = <ListTodoIcon className="size-3.5" />
  }

  return (
    <div
      className="flex h-full items-center gap-1.5 px-2 text-xs"
      title={label}
    >
      <span className="shrink-0">{icon}</span>
      <span className="acpp-tab-label truncate">{label}</span>
      {id !== "main" && id !== "tasks" ? (
        <button
          type="button"
          aria-label={t("workspace.closePanel")}
          className="acpp-tab-label -mr-0.5 flex size-4 items-center justify-center rounded-sm text-muted-foreground/60 hover:bg-muted hover:text-foreground"
          onPointerDown={(e) => e.stopPropagation()}
          onClick={(e) => {
            e.stopPropagation()
            props.api.close()
          }}
        >
          <XIcon className="size-3" />
        </button>
      ) : null}
    </div>
  )
}

function buildDefaultLayout(api: DockviewApi) {
  api.addPanel({ id: "main", component: "main" })
  api.addPanel({
    id: "tasks",
    component: "tasks",
    position: { referencePanel: "main", direction: "right" },
    initialWidth: 340,
  })
}

/**
 * 编排页 docking 容器：主对话 + 常驻任务列表。任务子会话面板由列表
 * 点开（不自动弹出），可拖动布局、可关闭（任务照跑）。
 * 布局不持久化——任务面板是围绕一次运行的临时观察窗口。
 */
export const OrchDock = memo(function OrchDock({
  attachApi,
}: {
  attachApi: (api: DockviewApi | null) => void
}) {
  const onReady = useCallback(
    (event: DockviewReadyEvent) => {
      attachApi(event.api)
      buildDefaultLayout(event.api)
      // 主对话与任务列表不可拖出成浮窗，也不可被关闭。
      event.api.onWillDragPanel((e) => {
        if (
          (e.panel.id === "main" || e.panel.id === "tasks") &&
          e.nativeEvent instanceof DragEvent
        ) {
          e.nativeEvent.preventDefault()
        }
      })
    },
    [attachApi]
  )

  return (
    <DockviewReact
      className="h-full w-full"
      theme={ACPP_THEME}
      components={COMPONENTS}
      defaultTabComponent={PanelTab}
      onReady={onReady}
    />
  )
})
