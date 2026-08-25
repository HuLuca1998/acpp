import { useSidebar } from "@/components/ui/sidebar"
import { cn } from "@/lib/utils"

/**
 * 内容区顶部那一条。
 *
 * **不管当前页有没有东西要放，这一条都在**：它同时是窗口的拖动区，缺了这一段
 * 顶部就有一块拖不动的死区（列表页最明显）。既然常驻，就让它把页面标题接住，
 * 空着才叫浪费——那正是改版前「空顶栏 + 页面内再来一个大标题」的毛病
 * （规范 §5.6）。
 *
 * 左侧让位只在折叠态出现：侧栏展开时窗口控件组坐在侧栏那条上，这里不必绕开
 * 它，标题直接贴左沿。
 */
export function TitleBar({
  title,
  workspace = false,
  children,
}: {
  title: string
  /** 会话工作区：整块空间归 dockview，这一条把位置让出去（见下）。 */
  workspace?: boolean
  children?: React.ReactNode
}) {
  const { state } = useSidebar()
  const collapsed = state === "collapsed"

  // 工作区页的第一行是 dockview 自己的标签栏，再叠一条标题栏纯属重复，
  // 拖动区也由那条标签栏兼任（见 index.css 的 dockview 块）。唯一的例外
  // 是折叠态：红绿灯浮到内容区左上角，总得有条空白接住它。
  if (workspace && !collapsed) return null
  if (workspace) {
    return (
      <div className="drag-region h-(--titlebar-height) shrink-0" aria-hidden />
    )
  }

  return (
    <header
      className={cn(
        "drag-region flex h-(--titlebar-height) shrink-0 items-center gap-2 pe-3",
        collapsed ? "ps-(--titlebar-inset)" : "ps-3"
      )}
    >
      <h1 className="truncate text-[13px] font-medium text-muted-foreground">
        {title}
      </h1>
      <div className="ms-auto flex items-center gap-1">{children}</div>
    </header>
  )
}
