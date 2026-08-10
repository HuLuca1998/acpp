import * as React from "react"
import { Link } from "react-router"

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
import {
  BotIcon,
  CircleHelpIcon,
  LayoutDashboardIcon,
  MessagesSquareIcon,
  PlugZapIcon,
  ScrollTextIcon,
  SearchIcon,
  Settings2Icon,
  TerminalIcon,
  WrenchIcon,
} from "lucide-react"

const data = {
  user: {
    name: "acp",
    email: "local@acp.dev",
    avatar: "/avatars/user.jpg",
  },
  navMain: [
    { title: "Overview", url: "/", icon: <LayoutDashboardIcon /> },
    { title: "Agents", url: "/agents", icon: <BotIcon /> },
    { title: "Sessions", url: "/sessions", icon: <MessagesSquareIcon /> },
    { title: "Tools", url: "/tools", icon: <WrenchIcon /> },
    { title: "Logs", url: "/logs", icon: <ScrollTextIcon /> },
  ],
  navSecondary: [
    { title: "Settings", url: "/settings", icon: <Settings2Icon /> },
    { title: "Connections", url: "/connections", icon: <PlugZapIcon /> },
    { title: "Get Help", url: "/help", icon: <CircleHelpIcon /> },
    { title: "Search", url: "/search", icon: <SearchIcon /> },
  ],
  recent: [
    { name: "Refactor auth module", url: "/sessions/1", icon: <TerminalIcon /> },
    { name: "Fix flaky tests", url: "/sessions/2", icon: <TerminalIcon /> },
    { name: "Draft release notes", url: "/sessions/3", icon: <TerminalIcon /> },
  ],
}

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
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
              <span className="text-base font-semibold">ACP Console</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        <NavMain items={data.navMain} />
        <NavRecent items={data.recent} />
        <NavSecondary items={data.navSecondary} className="mt-auto" />
      </SidebarContent>
      <SidebarFooter>
        <NavUser user={data.user} />
      </SidebarFooter>
    </Sidebar>
  )
}
