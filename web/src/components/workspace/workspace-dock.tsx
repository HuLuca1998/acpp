import { memo, useCallback, useEffect, useRef } from "react"
import { useTranslation } from "react-i18next"
import {
  DockviewReact,
  type DockviewApi,
  type DockviewReadyEvent,
  type DockviewTheme,
  type IDockviewHeaderActionsProps,
  type IDockviewPanelHeaderProps,
  type IDockviewPanelProps,
  type SerializedDockview,
} from "dockview-react"
import "dockview/dist/styles/dockview.css"
import { XIcon } from "lucide-react"

import { ChatPanel } from "@/components/workspace/panels/chat-panel"
import { applyLayoutPreset } from "@/components/workspace/layout-presets"
import { BranchesPanel } from "@/components/workspace/panels/branches-panel"
import { ChangesPanel } from "@/components/workspace/panels/changes-panel"
import { CommitDetailPanel } from "@/components/workspace/panels/commit-detail-panel"
import { HistoryPanel } from "@/components/workspace/panels/history-panel"
import { FilePreviewPanel } from "@/components/workspace/panels/file-preview-panel"
import { FileTreePanel } from "@/components/workspace/panels/file-tree-panel"
import { LogsPanel } from "@/components/workspace/panels/logs-panel"
import { TerminalPanel } from "@/components/workspace/panels/terminal-panel"
import {
  useGitOverview,
  useWorkspace,
} from "@/components/workspace/workspace-context"
import { WorkspaceMenu } from "@/components/workspace/workspace-menu"
import {
  PANEL_ICONS,
  panelKindOf,
  type WorkspacePanelKind,
} from "@/components/workspace/workspace-panels"

const LAYOUT_KEY = "acpp.workspace.layout.v1"

/** 皮肤只是壳：变量映射见 index.css 的 .dockview-theme-acpp 块。 */
const ACPP_THEME: DockviewTheme = {
  name: "acpp",
  className: "dockview-theme-acpp",
  gap: 6,
  dndTabIndicator: "line",
  tabGroupIndicator: "none",
  tabAnimation: "smooth",
}

/**
 * 面板内容包一层「右键到此为止」：面板自己的右键菜单（文件树、git 面板）
 * 处理完后，事件继续冒泡会让 dockview 去找它的企业版 ContextMenu 模块，
 * 控制台每次右键都报一条缺模块的错。我们不用它那套菜单，就在这儿断掉。
 */
function withOwnContextMenu(
  Panel: React.FunctionComponent<IDockviewPanelProps>
): React.FunctionComponent<IDockviewPanelProps> {
  return function PanelWithMenuGuard(props: IDockviewPanelProps) {
    return (
      <div className="h-full" onContextMenu={(e) => e.stopPropagation()}>
        <Panel {...props} />
      </div>
    )
  }
}

/** 面板组件注册表：引用必须稳定（模块级），dockview 据此重建面板。 */
const RAW_COMPONENTS: Record<
  WorkspacePanelKind,
  React.FunctionComponent<IDockviewPanelProps>
> = {
  chat: ChatPanel,
  files: FileTreePanel,
  preview: FilePreviewPanel,
  branches: BranchesPanel,
  history: HistoryPanel,
  changes: ChangesPanel,
  detail: CommitDetailPanel,
  logs: LogsPanel,
  terminal: TerminalPanel,
}

const COMPONENTS = Object.fromEntries(
  Object.entries(RAW_COMPONENTS).map(([kind, Panel]) => [
    kind,
    withOwnContextMenu(Panel),
  ])
) as Record<WorkspacePanelKind, React.FunctionComponent<IDockviewPanelProps>>

/**
 * 自定义 tab：图标 + 标题 + 关闭钮（chat 无）。窄栏时标题由容器查询
 * 隐藏、只剩图标（见 index.css）。拖动期间零 setState——悬停态全靠 CSS。
 */
function PanelTab(props: IDockviewPanelHeaderProps) {
  const { t } = useTranslation()
  const ws = useWorkspace()
  const id = props.api.id
  const kind = panelKindOf(id)
  const Icon = PANEL_ICONS[kind] ?? PANEL_ICONS.files
  const num = (props.params as { num?: number })?.num
  const label =
    kind === "terminal" && num
      ? `${t("workspace.panels.terminal")} ${num}`
      : t(`workspace.panels.${kind}` as never)
  return (
    <div
      className="flex h-full items-center gap-1.5 px-2 text-xs"
      title={label}
      // tab 上的右键同样不交给 dockview：它会去找只在企业版里的
      // ContextMenu 模块，控制台每次都报一条缺模块的错。
      onContextMenu={(e) => e.preventDefault()}
    >
      <span className="relative shrink-0">
        <Icon className="size-3.5" />
        {kind === "history" ? <CommitsTabDot /> : null}
      </span>
      <span className="acpp-tab-label flex items-center gap-1 truncate">
        {label}
        {kind === "history" ? <CommitsTabCount /> : null}
      </span>
      {kind !== "chat" ? (
        <button
          type="button"
          aria-label={t("workspace.closePanel")}
          className="acpp-tab-label -mr-0.5 flex size-4 items-center justify-center rounded-sm text-muted-foreground/60 hover:bg-muted hover:text-foreground"
          onPointerDown={(e) => e.stopPropagation()}
          onClick={(e) => {
            e.stopPropagation()
            // 统一走命令总线：终端面板要顺带杀 pty。
            ws.closePanel(id)
          }}
        >
          <XIcon className="size-3" />
        </button>
      ) : null}
    </div>
  )
}

/** 未推送数徽标（tab 文字态）：ahead > 0 才出现。 */
function CommitsTabCount() {
  const git = useGitOverview()
  const ahead = git.data?.ahead ?? 0
  if (ahead <= 0) return null
  return (
    <span className="rounded-full bg-primary/15 px-1.5 text-[10px] text-primary tabular-nums">
      {ahead}
    </span>
  )
}

/** 未推送标记（tab 图标态）：窄栏文字隐藏时叠在图标角上。 */
function CommitsTabDot() {
  const git = useGitOverview()
  const ahead = git.data?.ahead ?? 0
  if (ahead <= 0) return null
  return (
    <span className="absolute -top-0.5 -right-0.5 size-1.5 rounded-full bg-primary" />
  )
}

/** 组头右侧动作：只在对话所在组渲染 ⋯ 窗口管理菜单。 */
function HeaderActions(props: IDockviewHeaderActionsProps) {
  const hasChat = props.panels.some((p) => p.id === "chat")
  if (!hasChat) return null
  return <WorkspaceMenuSlot />
}

function WorkspaceMenuSlot() {
  const ws = useWorkspace()
  return (
    <div className="flex h-full items-center pr-1.5">
      <WorkspaceMenu
        onResetLayout={() => {
          localStorage.removeItem(LAYOUT_KEY)
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

/** 初始默认布局：见 layout-presets 的 default 预设（对话 80% + 右栏 tab 组）。 */
function buildDefaultLayout(api: DockviewApi) {
  applyLayoutPreset(api, "default")
}

/** 布局恢复：结构不合法（缺 chat / 未知组件）一律弃用重建。 */
function tryRestoreLayout(api: DockviewApi): boolean {
  const raw = localStorage.getItem(LAYOUT_KEY)
  if (!raw) return false
  try {
    const data = JSON.parse(raw) as SerializedDockview
    const panels = data.panels ?? {}
    const badComponent = Object.values(panels).some(
      (p) => !((p.contentComponent ?? "") in COMPONENTS)
    )
    if (!("chat" in panels) || badComponent) {
      throw new Error("layout shape mismatch")
    }
    api.fromJSON(data)
    return true
  } catch {
    localStorage.removeItem(LAYOUT_KEY)
    return false
  }
}

/** 对话组的保护：不许别的 tab 合进来（分裂到旁边仍允许）。 */
function lockChatGroup(api: DockviewApi) {
  const chat = api.getPanel("chat")
  if (chat) chat.group.locked = true
}

/**
 * 工作区 docking 容器。memo 隔离：聊天流的高频重渲染到此为止，
 * dockview 自身与其余面板不被牵连。
 */
export const WorkspaceDock = memo(function WorkspaceDock() {
  const ws = useWorkspace()
  const saveTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    return () => {
      if (saveTimer.current) clearTimeout(saveTimer.current)
      ws.attachApi(null)
    }
  }, [ws])

  const onReady = useCallback(
    (event: DockviewReadyEvent) => {
      const api = event.api
      ws.attachApi(api)

      if (!tryRestoreLayout(api)) buildDefaultLayout(api)
      lockChatGroup(api)

      // 对话面板不可拖出：在 dragstart 阶段取消原生拖拽。
      api.onWillDragPanel((e) => {
        if (e.panel.id === "chat" && e.nativeEvent instanceof DragEvent) {
          e.nativeEvent.preventDefault()
        }
      })

      // 布局持久化：防抖落盘；顺手补挂对话组保护（fromJSON/移动后组会换实例）。
      api.onDidLayoutChange(() => {
        lockChatGroup(api)
        if (saveTimer.current) clearTimeout(saveTimer.current)
        saveTimer.current = setTimeout(() => {
          try {
            localStorage.setItem(LAYOUT_KEY, JSON.stringify(api.toJSON()))
          } catch {
            // 存不进去（隐私模式等）就算了，布局丢失可重建。
          }
        }, 500)
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
