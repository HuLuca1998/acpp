import { memo, useCallback, useContext, useEffect, useRef } from "react"
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
import { SquareXIcon, XIcon } from "lucide-react"

import { Hint } from "@/components/hint"
import { ChatPanel } from "@/components/workspace/panels/chat-panel"
import { ChatPanelContext } from "@/components/workspace/chat-panel-context"
import { applyLayoutPreset } from "@/components/workspace/layout-presets"
import { BranchesPanel } from "@/components/workspace/panels/branches-panel"
import { ChangesPanel } from "@/components/workspace/panels/changes-panel"
import { CommitDetailPanel } from "@/components/workspace/panels/commit-detail-panel"
import { HistoryPanel } from "@/components/workspace/panels/history-panel"
import { FilePreviewPanel } from "@/components/workspace/panels/file-preview-panel"
import { FileTreePanel } from "@/components/workspace/panels/file-tree-panel"
import { LogsPanel } from "@/components/workspace/panels/logs-panel"
import { SubagentsPanel } from "@/components/workspace/panels/subagents-panel"
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

/** 面板组件注册表：引用必须稳定（模块级），dockview 据此重建面板。 */
const COMPONENTS: Record<
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
  subagents: SubagentsPanel,
  terminal: TerminalPanel,
}

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
  // 对话面板顶在左上角，它这条标签栏就是窗口的第一行——所以它显示的是
  // 「这条会话叫什么」，而不是一个永远写着「对话」的通用标签。工作区页
  // 没有别的地方交代当前在看哪条会话了（规范 §5.6）。
  // 直接读 context 而不是 useChatPanel()：后者在缺 provider 时抛错，而标签
  // 组件由 dockview 渲染，不值得为一个标题冒这个险。
  const sessionTitle = useContext(ChatPanelContext)?.chat.session?.title
  const label =
    kind === "chat" && sessionTitle
      ? sessionTitle
      : kind === "terminal" && num
        ? `${t("workspace.panels.terminal")} ${num}`
        : t(`workspace.panels.${kind}` as never)
  return (
    <div
      // 类型标记给 CSS 用：对话那一格不长成页签，只是这块面板的标题。
      data-panel-kind={kind}
      className="flex h-full items-center gap-1.5 px-2 text-xs"
      title={label}
      // tab 的右键不交给 dockview——它的 tab 菜单在企业版里，我们也不需要。
      // （dockview 在初始化时仍会为缺少该模块打一条控制台告警，那是库自身
      //  的行为，与这里的拦截无关，也不影响功能。）
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
        <Hint label={t("workspace.closePanel")} align="end">
          <button
            type="button"
            aria-label={t("workspace.closePanel")}
            className="acpp-tab-close acpp-tab-label -mr-0.5 flex size-4 items-center justify-center rounded-sm text-muted-foreground/60 hover:bg-muted hover:text-foreground"
            onPointerDown={(e) => e.stopPropagation()}
            onClick={(e) => {
              e.stopPropagation()
              // 统一走命令总线：终端面板要顺带杀 pty。
              ws.closePanel(id)
            }}
          >
            <XIcon className="size-3" />
          </button>
        </Hint>
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

/**
 * 组头右侧动作：对话组给 ⋯ 窗口管理菜单，工具组给「全部关闭」。
 *
 * 工具组常常攒到四五个 tab（文件树 / 分支 / 变更 / 详情 / 日志），换个
 * 任务想清空得逐个点 ×。只在 ≥2 个面板时给这个按钮——只剩一个时它自己
 * 的 × 就够用了，再多一个按钮反而是噪音。
 */
function HeaderActions(props: IDockviewHeaderActionsProps) {
  const hasChat = props.panels.some((p) => p.id === "chat")
  if (hasChat) return <WorkspaceMenuSlot />
  // 只有一个面板时那一格是标题而不是页签（见 index.css），关闭钮跟着挪到
  // 这一行的右端——标题里塞一颗 × 会让它重新长得像页签。
  if (props.panels.length === 1) return <ClosePanelSlot id={props.panels[0].id} />
  return <CloseAllSlot ids={props.panels.map((p) => p.id)} />
}

/** 单面板时右端那颗关闭钮。 */
function ClosePanelSlot({ id }: { id: string }) {
  const { t } = useTranslation()
  const ws = useWorkspace()
  return (
    <div className="flex h-full items-center pr-1.5">
      <Hint label={t("workspace.closePanel")} align="end">
        <button
          type="button"
          aria-label={t("workspace.closePanel")}
          className="flex size-6 items-center justify-center rounded-md text-muted-foreground/70 transition-colors duration-150 hover:bg-muted hover:text-foreground"
          // 走命令总线：终端面板要顺带杀 pty，绕过去会留下孤儿进程。
          onClick={() => ws.closePanel(id)}
        >
          <XIcon className="size-3.5" />
        </button>
      </Hint>
    </div>
  )
}

/** 一键清空这一组。 */
function CloseAllSlot({ ids }: { ids: string[] }) {
  const { t } = useTranslation()
  const ws = useWorkspace()
  return (
    <div className="flex h-full items-center pr-1.5">
      <Hint label={t("workspace.closeAllPanels")} align="end">
        <button
          type="button"
          aria-label={t("workspace.closeAllPanels")}
          className="flex size-6 items-center justify-center rounded-md text-muted-foreground/70 transition-colors duration-150 hover:bg-muted hover:text-foreground"
          onClick={() => {
            // 逐个走命令总线，不用 group.api.close()：终端面板要顺带杀
            // pty（见 tab 上那颗 × 的注释），绕过去会留下孤儿进程。
            // chat 不可关，真混进来也过滤掉。
            for (const id of ids) {
              if (id === "chat") continue
              ws.closePanel(id)
            }
          }}
        >
          <SquareXIcon className="size-4" />
        </button>
      </Hint>
    </div>
  )
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
 * 标出「窗口左上角那一组」。
 *
 * 侧栏折叠后窗口控件压在那一格上，这一组的标签栏得给它让位。用 CSS 的
 * `:first-child` 选不出来——dockview 的容器是嵌套的，每一层的第一个子元素
 * 都会中招，结果文件树、终端的标签栏也跟着缩进（看着就是「折叠前后对不齐」）。
 * 按几何位置认最靠左上的那个，才是真的那一个。
 */
function markLeadGroup(api: DockviewApi) {
  let lead: (typeof api.groups)[number] | null = null
  let best = Infinity
  for (const group of api.groups) {
    const box = group.element.getBoundingClientRect()
    // 左上角优先：先比上边沿，同高再比左边沿。
    const score = box.top * 10000 + box.left
    if (score < best) {
      best = score
      lead = group
    }
    delete group.element.dataset.acppLead
  }
  if (lead) lead.element.dataset.acppLead = "true"
}

/**
 * 给每个分组的标签栏空白处装一个「移动」把手。
 *
 * 标签栏是工作区页的窗口第一行，整条得是窗口拖动区，否则那儿拖不动窗口
 * （规范 §5.6）；而 dockview 原本正是用这块空白发起「拖走整组」——两件事
 * 撞在一起，表现为拖面板结果整个窗口跟着走。
 *
 * 把手是这块空白的子元素，单独排除出拖动区（no-drag-region），指针事件照常
 * 冒泡给 dockview 的分组拖拽。于是两种拖动各走各的：拖把手移动面板，拖旁边
 * 的空白移动窗口。锁住的分组（对话）本来就不许拖走，不给把手。
 */
function attachMoveHandles(api: DockviewApi, label: string) {
  markLeadGroup(api)
  for (const group of api.groups) {
    const slot = group.element.querySelector<HTMLElement>(".dv-void-container")
    if (!slot) continue
    const existing = slot.querySelector(".acpp-move-handle")
    if (group.locked) {
      existing?.remove()
      continue
    }
    if (existing) continue
    const handle = document.createElement("div")
    handle.className = "acpp-move-handle no-drag-region"
    handle.title = label
    slot.appendChild(handle)
  }
}

/**
 * 工作区 docking 容器。memo 隔离：聊天流的高频重渲染到此为止，
 * dockview 自身与其余面板不被牵连。
 */
export const WorkspaceDock = memo(function WorkspaceDock() {
  const ws = useWorkspace()
  const { t } = useTranslation()
  const moveLabel = t("workspace.movePanel")
  const saveTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  // 只在真正卸载时摘句柄。依赖必须是稳定的 attachApi 而不是整个 ws：
  // ws 每换一次（切会话、草稿态换目录都会换）都会跑一遍 cleanup，把句柄
  // 摘掉，而挂句柄只发生在 dockview 首次 ready 那一次——从会话页点「新建
  // 会话」之后，面板就此关不掉也切不动，正是这么来的。
  const detach = ws.attachApi
  useEffect(() => {
    const timer = saveTimer
    return () => {
      if (timer.current) clearTimeout(timer.current)
      detach(null)
    }
  }, [detach])

  const onReady = useCallback(
    (event: DockviewReadyEvent) => {
      const api = event.api
      ws.attachApi(api)

      if (!tryRestoreLayout(api)) buildDefaultLayout(api)
      lockChatGroup(api)
      attachMoveHandles(api, moveLabel)

      // 对话面板不可拖出：在 dragstart 阶段取消原生拖拽。
      api.onWillDragPanel((e) => {
        if (e.panel.id === "chat" && e.nativeEvent instanceof DragEvent) {
          e.nativeEvent.preventDefault()
        }
      })

      // 布局持久化：防抖落盘；顺手补挂对话组保护（fromJSON/移动后组会换实例）。
      api.onDidLayoutChange(() => {
        lockChatGroup(api)
        // 新分组是布局变化后才出现的，把手要跟着补挂。
        attachMoveHandles(api, moveLabel)
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
    [ws, moveLabel]
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
