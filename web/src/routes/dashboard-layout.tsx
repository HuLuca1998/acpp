import { Outlet, useLocation } from "react-router"

import { AppSidebar } from "@/components/app-sidebar"
import { SiteHeader } from "@/components/site-header"
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar"

const TITLES: Record<string, string> = {
  "/": "Overview",
  "/agents": "Agents",
  "/sessions": "Sessions",
  "/tools": "Tools",
  "/logs": "Logs",
  "/settings": "Settings",
  "/connections": "Connections",
}

function titleFor(pathname: string) {
  if (TITLES[pathname]) return TITLES[pathname]
  const match = Object.keys(TITLES).find(
    (key) => key !== "/" && pathname.startsWith(key)
  )
  return match ? TITLES[match] : "ACP Console"
}

export function DashboardLayout() {
  const { pathname } = useLocation()

  return (
    <SidebarProvider
      style={
        {
          "--sidebar-width": "calc(var(--spacing) * 72)",
          "--header-height": "calc(var(--spacing) * 12)",
        } as React.CSSProperties
      }
    >
      <AppSidebar variant="inset" />
      <SidebarInset>
        <SiteHeader title={titleFor(pathname)} />
        <div className="flex flex-1 flex-col">
          <div className="@container/main flex flex-1 flex-col gap-2">
            <Outlet />
          </div>
        </div>
      </SidebarInset>
    </SidebarProvider>
  )
}
