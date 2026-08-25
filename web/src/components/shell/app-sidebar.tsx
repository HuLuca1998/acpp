import * as React from "react"
import { useTranslation } from "react-i18next"
import { useLocation } from "react-router"

import { NavUser } from "@/components/shell/nav-user"
import { NoticeCenter } from "@/components/shell/notice-center"
import { NavMain } from "@/components/shell/nav-main"
import { AgentIcon } from "@/components/agent-icon"
import { NavProjects } from "@/components/shell/nav-projects"
import { NavRecent } from "@/components/shell/nav-recent"
import { SidebarResizer } from "@/components/shell/window/sidebar-resizer"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  useSidebar,
} from "@/components/ui/sidebar"
import type { useSidebarFrame } from "@/hooks/use-sidebar-frame"
import { useIdentity } from "@/hooks/identity-context"
import { toast } from "sonner"

import { api } from "@/lib/api"
import { capitalize } from "@/lib/format"
import { cn } from "@/lib/utils"
import { groupSessionsByCwd } from "@/lib/session-groups"
import type { Session } from "@/types/acp"
import {
  DatabaseIcon,
  LayoutDashboardIcon,
  MessagesSquareIcon,
  ScrollTextIcon,
  PuzzleIcon,
  WrenchIcon,
} from "lucide-react"

// 分组前多拉一些：最终只展示 5 个项目 × 5 条，但要先有足够的样本才能
// 挑出「最近用过的 5 个项目」。
const RECENT_LIMIT = 50

export function AppSidebar({
  frame,
  ...props
}: React.ComponentProps<typeof Sidebar> & {
  frame: ReturnType<typeof useSidebarFrame>
}) {
  const { t } = useTranslation()
  const { state } = useSidebar()
  const { pathname } = useLocation()
  const { identity } = useIdentity()
  const isOwner = identity?.owner ?? false
  // 底部显示「我是谁」：owner 是这台机器的主人，访客用自己的名字（首字母
  // 大写，租户名是目录名、多半是小写的）。
  const whoami = isOwner
    ? t("identity.admin")
    : identity?.tenantName
      ? capitalize(identity.tenantName)
      : ""
  const [recent, setRecent] = React.useState<Session[]>([])

  // 随路由变化刷新：新建/删除会话后列表立即跟上，不留已删会话的死链接。
  //
  // 但绝大多数导航并不改变这份列表（点开一条已有会话最典型），所以拉回来
  // 先比一遍——没变就留住原引用，整棵侧栏子树不重渲染。侧栏跟着每次导航
  // 抖一下，恰好发生在用户切会话、页面本来就最忙的那一刻。
  React.useEffect(() => {
    let cancelled = false
    api.sessions
      .list({ pageSize: RECENT_LIMIT })
      .then((sessions) => {
        if (cancelled) return
        setRecent((prev) =>
          sameRecentList(prev, sessions.items) ? prev : sessions.items
        )
      })
      .catch(() => {
        // 侧边栏的最近列表拉不到就空着，不打断主流程。
      })
    return () => {
      cancelled = true
    }
  }, [pathname])

  // 按 cwd 分组，不依赖项目扫描——会话自带的目录永远对得上。
  const groups = React.useMemo(() => groupSessionsByCwd(recent), [recent])

  // 租户只留会话与项目：技能、设置、连接都是 owner 的东西，后端也已按
  // owner-only 拦截，导航里直接不出现（adr-007）。
  const navMain = React.useMemo(
    () =>
      isOwner
        ? [
            {
              title: t("nav.overview"),
              url: "/",
              icon: <LayoutDashboardIcon />,
            },
            { title: t("nav.skills"), url: "/skills", icon: <PuzzleIcon /> },
            {
              title: t("nav.sessions"),
              url: "/sessions",
              icon: <MessagesSquareIcon />,
            },
            {
              title: t("nav.databases"),
              url: "/databases",
              icon: <DatabaseIcon />,
            },
            { title: t("nav.tools"), url: "/tools", icon: <WrenchIcon /> },
            { title: t("nav.logs"), url: "/logs", icon: <ScrollTextIcon /> },
          ]
        : [
            {
              title: t("nav.sessions"),
              url: "/sessions",
              icon: <MessagesSquareIcon />,
            },
          ],
    [isOwner, t]
  )

  // 品牌图标标出会话属于哪个 agent，一眼可辨。
  const recentItems = React.useMemo(
    () =>
      recent.map((session) => ({
        id: session.id,
        name: session.title || `${t("common.unnamed")} #${session.id}`,
        url: `/sessions/${session.id}`,
        icon: <AgentIcon flavor={session.agentFlavor} className="size-4" />,
      })),
    [recent, t]
  )

  // 改名后就地更新本地这份列表：等下次导航再刷新的话，改完那一下标题不动，
  // 看着像没生效。
  const renameSession = React.useCallback(
    (id: number, title: string) => {
      api.sessions
        .rename(id, title)
        .then((updated) =>
          setRecent((prev) =>
            prev.map((s) => (s.id === id ? { ...s, title: updated.title } : s))
          )
        )
        .catch(() => toast.error(t("nav.renameFailed")))
    },
    [t]
  )

  // 折叠后侧栏是悬停浮出的临时浮层：盖在内容上，不把内容挤开（规范 §5.6）。
  // 宽度那时也没有留存的意义，把手不渲染。
  const collapsed = state === "collapsed"

  return (
    <Sidebar
      collapsible="offcanvas"
      // 顶部不留内边距：侧栏那条让位要贴着窗口上沿，才能和内容区的顶栏连成
      // 一条线，也才能接住系统红绿灯（规范 §5.6）。
      className={cn(
        // 不留内边距：侧栏整块贴着窗口边，与内容区靠底色区分而不是留白。
        "p-0!",
        collapsed && frame.peek && "left-0! z-50 shadow-2xl transition-[left]"
      )}
      onMouseEnter={collapsed ? frame.openPeek : undefined}
      onMouseLeave={frame.closePeek}
      {...props}
    >
      {/* 顶部这条只做一件事：给窗口条让位（那一条是 fixed 的，见
          window/window-controls.tsx），顺带当窗口的拖动区。 */}
      <div className="drag-region h-(--titlebar-height) shrink-0" />
      <SidebarContent>
        <NavMain items={navMain} />
        {/* 有项目就按项目分组（最多 5 组 × 5 条），否则平铺最近会话——
            工作区里还没有仓库时分组只会多一层空壳。 */}
        {groups.length > 0 ? (
          <NavProjects
            label={t("nav.recentSessions")}
            groups={groups}
            onRename={renameSession}
          />
        ) : recentItems.length > 0 ? (
          <NavRecent
            label={t("nav.recentSessions")}
            items={recentItems}
            onRename={renameSession}
          />
        ) : null}
      </SidebarContent>
      <SidebarFooter>
        {/* 底部从上到下：通知中心（要人动手处理的东西，占大头）→ 一行
            用户条目——设置/连接/外观/语言与后端状态全部收进它的菜单
            （shadcn NavUser 模式），空间尽数让给通知。 */}
        <NoticeCenter />
        <NavUser whoami={whoami} isOwner={isOwner} />
      </SidebarFooter>
      {!collapsed && (
        <SidebarResizer
          onPointerDown={frame.startResize}
          onDoubleClick={frame.resetWidth}
        />
      )}
    </Sidebar>
  )
}

/**
 * 两次拉取的最近会话列表是不是同一份。只比侧栏真正显示的字段——
 * 用量、设置这些每轮都在变的字段与侧栏无关，跟着它们重渲染纯属白费。
 */
function sameRecentList(a: Session[], b: Session[]): boolean {
  if (a.length !== b.length) return false
  return a.every((s, i) => {
    const t = b[i]
    return (
      s.id === t.id &&
      s.title === t.title &&
      s.cwd === t.cwd &&
      s.agentFlavor === t.agentFlavor &&
      s.updatedAt === t.updatedAt
    )
  })
}
