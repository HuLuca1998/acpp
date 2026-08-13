import { useTranslation } from "react-i18next"
import { Outlet, useLocation } from "react-router"

import { AppSidebar } from "@/components/shell/app-sidebar"
import { SiteHeader } from "@/components/shell/site-header"
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar"

/** 路径前缀 → 标题的翻译 key。最长前缀优先。 */
const TITLE_KEYS = [
  ["/skills", "nav.skills"],
  ["/sessions", "nav.sessions"],
  ["/tools", "nav.tools"],
  ["/logs", "nav.logs"],
  ["/settings", "nav.settings"],
  ["/connections", "nav.connections"],
  ["/help", "nav.help"],
  ["/search", "nav.search"],
] as const

export function DashboardLayout() {
  const { t } = useTranslation()
  const { pathname } = useLocation()

  const matched = TITLE_KEYS.find(([prefix]) => pathname.startsWith(prefix))
  const title =
    pathname === "/"
      ? t("nav.overview")
      : matched
        ? t(matched[1])
        : t("common.appName")

  return (
    <SidebarProvider
      // 锁定整个 shell 到视口高度，滚动交给内容区自己处理，
      // 这样聊天页的输入框才能始终固定在底部。
      className="h-svh"
      style={
        {
          "--sidebar-width": "calc(var(--spacing) * 72)",
          "--header-height": "calc(var(--spacing) * 12)",
        } as React.CSSProperties
      }
    >
      <AppSidebar variant="inset" />
      <SidebarInset className="overflow-hidden">
        <SiteHeader title={title} />
        <div className="flex min-h-0 flex-1 flex-col">
          <div className="@container/main flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto">
            <Outlet />
          </div>
        </div>
      </SidebarInset>
    </SidebarProvider>
  )
}
