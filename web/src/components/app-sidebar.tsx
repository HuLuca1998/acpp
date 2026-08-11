import * as React from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"

import { AppearanceSwitcher } from "@/components/appearance-switcher"
import { LanguageSwitcher } from "@/components/language-switcher"
import { NavMain } from "@/components/nav-main"
import { NavRecent } from "@/components/nav-recent"
import { NavSecondary } from "@/components/nav-secondary"
import { NavUser } from "@/components/nav-user"
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
  BotIcon,
  LayoutDashboardIcon,
  MessagesSquareIcon,
  PlugZapIcon,
  ScrollTextIcon,
  Settings2Icon,
  TerminalIcon,
  WrenchIcon,
} from "lucide-react"

const RECENT_LIMIT = 3

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const { t } = useTranslation()
  const [recent, setRecent] = React.useState<Session[]>([])

  React.useEffect(() => {
    let cancelled = false
    api.sessions
      .list()
      .then((res) => {
        if (!cancelled) setRecent(res.items.slice(0, RECENT_LIMIT))
      })
      .catch(() => {
        // 侧边栏的最近列表拉不到就空着，不打断主流程。
      })
    return () => {
      cancelled = true
    }
  }, [])

  const navMain = [
    { title: t("nav.overview"), url: "/", icon: <LayoutDashboardIcon /> },
    { title: t("nav.agents"), url: "/agents", icon: <BotIcon /> },
    {
      title: t("nav.sessions"),
      url: "/sessions",
      icon: <MessagesSquareIcon />,
    },
    { title: t("nav.tools"), url: "/tools", icon: <WrenchIcon /> },
    { title: t("nav.logs"), url: "/logs", icon: <ScrollTextIcon /> },
  ]

  const navSecondary = [
    { title: t("nav.settings"), url: "/settings", icon: <Settings2Icon /> },
    { title: t("nav.connections"), url: "/connections", icon: <PlugZapIcon /> },
  ]

  const recentItems = recent.map((session) => ({
    name: session.title || `${t("common.unnamed")} #${session.id}`,
    url: `/sessions/${session.id}`,
    icon: <TerminalIcon />,
  }))

  return (
    <Sidebar collapsible="offcanvas" {...props}>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              className="data-[slot=sidebar-menu-button]:p-1.5!"
              render={<Link to="/" />}
            >
              <PlugZapIcon className="size-5!" />
              <span className="text-base font-semibold">
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
        <NavUser user={{ name: "acp", email: "local@acp.dev", avatar: "" }} />
      </SidebarFooter>
    </Sidebar>
  )
}
