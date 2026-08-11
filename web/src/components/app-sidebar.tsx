import * as React from "react"
import { useTranslation } from "react-i18next"
import { Link, useLocation } from "react-router"

import { AppearanceSwitcher } from "@/components/appearance-switcher"
import { BackendStatus } from "@/components/backend-status"
import { LanguageSwitcher } from "@/components/language-switcher"
import { NavMain } from "@/components/nav-main"
import { AgentIcon } from "@/components/agent-icon"
import { NavRecent } from "@/components/nav-recent"
import { NavSecondary } from "@/components/nav-secondary"
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
              {/* 品牌徽标：主色渐变方块，palette 切换时自动跟随。 */}
              <span className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-linear-to-b from-primary to-[color-mix(in_oklch,var(--primary),black_18%)] text-primary-foreground shadow-sm [box-shadow:inset_0_1px_0_oklch(1_0_0/25%),0_1px_2px_oklch(0_0_0/20%)]">
                <PlugZapIcon className="size-4!" />
              </span>
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
