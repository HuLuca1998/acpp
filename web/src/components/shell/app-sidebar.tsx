import * as React from "react"
import { useTranslation } from "react-i18next"
import { Link, useLocation } from "react-router"

import { AppearanceSwitcher } from "@/components/shell/appearance-switcher"
import { BackendStatus } from "@/components/shell/backend-status"
import { LanguageSwitcher } from "@/components/shell/language-switcher"
import { NavMain } from "@/components/shell/nav-main"
import { AgentIcon } from "@/components/agent-icon"
import { NavProjects } from "@/components/shell/nav-projects"
import { NavRecent } from "@/components/shell/nav-recent"
import { NavSecondary } from "@/components/shell/nav-secondary"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar"
import { useIsOwner } from "@/hooks/identity-context"
import { api } from "@/lib/api"
import { groupSessionsByProject, type SessionGroup } from "@/lib/session-groups"
import type { Session } from "@/types/acp"
import {
  DramaIcon,
  LayoutDashboardIcon,
  MessagesSquareIcon,
  NetworkIcon,
  PlugZapIcon,
  ScrollTextIcon,
  Settings2Icon,
  PuzzleIcon,
  WrenchIcon,
} from "lucide-react"

// 分组前多拉一些：最终只展示 5 个项目 × 5 条，但要先有足够的样本才能
// 挑出「最近用过的 5 个项目」。
const RECENT_LIMIT = 50

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const { t } = useTranslation()
  const { pathname } = useLocation()
  const isOwner = useIsOwner()
  const [recent, setRecent] = React.useState<Session[]>([])
  const [groups, setGroups] = React.useState<SessionGroup[]>([])

  // 随路由变化刷新：新建/删除会话后列表立即跟上，不留已删会话的死链接。
  React.useEffect(() => {
    let cancelled = false
    Promise.all([
      api.sessions.list({ pageSize: RECENT_LIMIT }),
      // 项目拉不到（工作区根不可读等）不该拖垮最近会话，失败退化成平铺。
      api.projects.list().catch(() => ({ items: [] })),
    ])
      .then(([sessions, projects]) => {
        if (cancelled) return
        setRecent(sessions.items)
        setGroups(
          groupSessionsByProject(sessions.items, projects.items).groups
        )
      })
      .catch(() => {
        // 侧边栏的最近列表拉不到就空着，不打断主流程。
      })
    return () => {
      cancelled = true
    }
  }, [pathname])

  // 租户只留会话与项目：编排、技能、角色、设置、连接都是 owner 的东西，
  // 后端也已按 owner-only 拦截，导航里直接不出现（adr-007）。
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
          title: t("nav.orchestrator"),
          url: "/orchestrator",
          icon: <NetworkIcon />,
        },
        { title: t("nav.roles"), url: "/roles", icon: <DramaIcon /> },
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

  const navSecondary = isOwner
    ? [
        { title: t("nav.settings"), url: "/settings", icon: <Settings2Icon /> },
        {
          title: t("nav.connections"),
          url: "/connections",
          icon: <PlugZapIcon />,
        },
      ]
    : []

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
        <NavSecondary items={navSecondary} className="mt-auto">
          <AppearanceSwitcher />
          <LanguageSwitcher />
        </NavSecondary>
      </SidebarContent>
      <SidebarFooter>
        <BackendStatus />
      </SidebarFooter>
    </Sidebar>
  )
}
