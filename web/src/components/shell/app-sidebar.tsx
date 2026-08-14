import * as React from "react"
import { useTranslation } from "react-i18next"
import { Link, useLocation } from "react-router"

import { AppearanceSwitcher } from "@/components/shell/appearance-switcher"
import { BackendStatus } from "@/components/shell/backend-status"
import { LanguageSwitcher } from "@/components/shell/language-switcher"
import { NavMain } from "@/components/shell/nav-main"
import { AgentIcon } from "@/components/agent-icon"
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
import { api } from "@/lib/api"
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

const RECENT_LIMIT = 10

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const { t } = useTranslation()
  const { pathname } = useLocation()
  const [recent, setRecent] = React.useState<Session[]>([])

  // 随路由变化刷新：新建/删除会话后列表立即跟上，不留已删会话的死链接。
  React.useEffect(() => {
    let cancelled = false
    api.sessions
      .list({ pageSize: RECENT_LIMIT })
      .then((res) => {
        if (!cancelled) setRecent(res.items)
      })
      .catch(() => {
        // 侧边栏的最近列表拉不到就空着，不打断主流程。
      })
    return () => {
      cancelled = true
    }
  }, [pathname])

  const navMain = [
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

  const navSecondary = [
    { title: t("nav.settings"), url: "/settings", icon: <Settings2Icon /> },
    { title: t("nav.connections"), url: "/connections", icon: <PlugZapIcon /> },
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
        {recentItems.length > 0 ? (
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
