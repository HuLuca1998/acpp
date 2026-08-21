import * as React from "react"
import { useTranslation } from "react-i18next"
import { Link, useLocation } from "react-router"

import { NavUser } from "@/components/shell/nav-user"
import { NoticeCenter } from "@/components/shell/notice-center"
import { NavMain } from "@/components/shell/nav-main"
import { AgentIcon } from "@/components/agent-icon"
import { NavProjects } from "@/components/shell/nav-projects"
import { NavRecent } from "@/components/shell/nav-recent"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar"
import { useIdentity } from "@/hooks/identity-context"
import { api } from "@/lib/api"
import { capitalize } from "@/lib/format"
import { groupSessionsByCwd, type SessionGroup } from "@/lib/session-groups"
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

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const { t } = useTranslation()
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
  const [groups, setGroups] = React.useState<SessionGroup[]>([])

  // 随路由变化刷新：新建/删除会话后列表立即跟上，不留已删会话的死链接。
  React.useEffect(() => {
    let cancelled = false
    api.sessions
      .list({ pageSize: RECENT_LIMIT })
      .then((sessions) => {
        if (cancelled) return
        setRecent(sessions.items)
        // 按 cwd 分组，不依赖项目扫描——会话自带的目录永远对得上。
        setGroups(groupSessionsByCwd(sessions.items))
      })
      .catch(() => {
        // 侧边栏的最近列表拉不到就空着，不打断主流程。
      })
    return () => {
      cancelled = true
    }
  }, [pathname])

  // 租户只留会话与项目：技能、设置、连接都是 owner 的东西，后端也已按
  // owner-only 拦截，导航里直接不出现（adr-007）。
  const navMain = isOwner
    ? [
        { title: t("nav.overview"), url: "/", icon: <LayoutDashboardIcon /> },
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
      ]

  // 品牌图标标出会话属于哪个 agent，一眼可辨。
  const recentItems = recent.map((session) => ({
    name: session.title || `${t("common.unnamed")} #${session.id}`,
    url: `/sessions/${session.id}`,
    icon: <AgentIcon flavor={session.agentFlavor} className="size-4" />,
  }))

  return (
    <Sidebar collapsible="offcanvas" {...props}>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              className="h-auto data-[slot=sidebar-menu-button]:p-1.5!"
              render={<Link to="/" />}
            >
              {/* 品牌徽标：app 图标（public/app-icon.svg，与桌面版图标同源）。 */}
              <img src="/app-icon.svg" alt="" className="size-7 shrink-0" />
              <span className="text-base font-semibold tracking-tight">
                {t("common.appName")}
              </span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        <NavMain items={navMain} />
        {/* 有项目就按项目分组（最多 5 组 × 5 条），否则平铺最近会话——
            工作区里还没有仓库时分组只会多一层空壳。 */}
        {groups.length > 0 ? (
          <NavProjects label={t("nav.recentSessions")} groups={groups} />
        ) : recentItems.length > 0 ? (
          <NavRecent label={t("nav.recentSessions")} items={recentItems} />
        ) : null}
      </SidebarContent>
      <SidebarFooter>
        {/* 底部从上到下：通知中心（要人动手处理的东西，占大头）→ 一行
            用户条目——设置/连接/外观/语言与后端状态全部收进它的菜单
            （shadcn NavUser 模式），空间尽数让给通知。 */}
        <NoticeCenter />
        <NavUser whoami={whoami} isOwner={isOwner} />
      </SidebarFooter>
    </Sidebar>
  )
}
