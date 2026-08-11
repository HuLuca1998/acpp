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

import { ChatPanel } from "@/components/workspace/chat-panel"
import { ComingSoonPanel } from "@/components/workspace/coming-soon-panel"
import { FilePreviewPanel } from "@/components/workspace/file-preview-panel"
import { FileTreePanel } from "@/components/workspace/file-tree-panel"
import { useWorkspace } from "@/components/workspace/workspace-context"
import { WorkspaceMenu } from "@/components/workspace/workspace-menu"
import {
  PANEL_ICONS,
  type WorkspacePanelId,
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

function DiffPanel() {
  return <ComingSoonPanel id="diff" />
}
function CommitsPanel() {
  return <ComingSoonPanel id="commits" />
}
function TerminalPanel() {
  return <ComingSoonPanel id="terminal" />
}

/** 面板组件注册表：引用必须稳定（模块级），dockview 据此重建面板。 */
const COMPONENTS: Record<
  WorkspacePanelId,
  React.FunctionComponent<IDockviewPanelProps>
> = {
  chat: ChatPanel,
  files: FileTreePanel,
  preview: FilePreviewPanel,
  diff: DiffPanel,
  commits: CommitsPanel,
  terminal: TerminalPanel,
}

/**
 * 自定义 tab：图标 + 标题 + 关闭钮（chat 无）。窄栏时标题由容器查询
 * 隐藏、只剩图标（见 index.css）。拖动期间零 setState——悬停态全靠 CSS。
 */
function PanelTab(props: IDockviewPanelHeaderProps) {
  const { t } = useTranslation()
  const id = props.api.id as WorkspacePanelId
  const Icon = PANEL_ICONS[id] ?? PANEL_ICONS.files
  return (
    <div
      className="flex h-full items-center gap-1.5 px-2 text-xs"
      title={t(`workspace.panels.${id}` as never)}
    >
      <Icon className="size-3.5 shrink-0" />
      <span className="acpp-tab-label truncate">
        {t(`workspace.panels.${id}` as never)}
      </span>
      {id !== "chat" ? (
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

/** 初始默认布局：对话 80% 居左；右栏 tab 组 = 文件树（激活）+ 待命三块。 */
function buildDefaultLayout(api: DockviewApi) {
  api.addPanel({ id: "chat", component: "chat", minimumWidth: 320 })
  api.addPanel({
    id: "files",
    component: "files",
    position: { referencePanel: "chat", direction: "right" },
  })
  for (const id of ["diff", "commits", "terminal"] as const) {
    api.addPanel({
      id,
      component: id,
      inactive: true,
      position: { referencePanel: "files", direction: "within" },
    })
  }
  // dockview 挂载瞬间的容器是 100px 占位尺寸，此时按比例 setSize 会被
  // 最小宽度钳死成空操作、落盘均分布局。逐帧等到真实测量（能同时容纳
  // 对话最小宽与右栏）再定 80/20，约 2s 兜底放弃。
  let frames = 0
  const applyRatio = () => {
    const chat = api.getPanel("chat")
    if (!chat) return
    if (api.width >= 480) {
      chat.api.setSize({ width: Math.round(api.width * 0.8) })
      return
    }
    if (frames++ < 120) requestAnimationFrame(applyRatio)
  }
  requestAnimationFrame(applyRatio)
}

/** 布局恢复：结构不合法（缺 chat / 未知组件）一律弃用重建。 */
function tryRestoreLayout(api: DockviewApi): boolean {
  const raw = localStorage.getItem(LAYOUT_KEY)
  if (!raw) return false
  try {
    const data = JSON.parse(raw) as SerializedDockview
    const ids = Object.keys(data.panels ?? {})
    if (!ids.includes("chat") || ids.some((id) => !(id in COMPONENTS))) {
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
