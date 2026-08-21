import { useTranslation } from "react-i18next"
import { Link } from "react-router"

import { MoreHorizontalIcon } from "lucide-react"

import {
  SidebarGroup,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar"

/**
 * 侧边栏「最近会话」平铺分组：会话都没有工作目录时的退化形态
 *（有目录就走 NavProjects 按项目分组）。
 *
 * 条目只做跳转，不带行内菜单——原来那个菜单里的三项（继续/导出/删除）
 * 从来没接过处理器，点了什么都不会发生。真正的会话管理在会话列表页。
 */
export function NavRecent({
  label,
  items,
}: {
  label: string
  items: {
    name: string
    url: string
    icon: React.ReactNode
  }[]
}) {
  const { t } = useTranslation()
  return (
    <SidebarGroup className="group-data-[collapsible=icon]:hidden">
      <SidebarGroupLabel>{label}</SidebarGroupLabel>
      <SidebarMenu>
        {items.map((item) => (
          // 标题可重复（如两个"你好"会话），key 用含会话 id 的 url。
          <SidebarMenuItem key={item.url}>
            <SidebarMenuButton render={<Link to={item.url} />}>
              {item.icon}
              <span>{item.name}</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        ))}
        <SidebarMenuItem>
          <SidebarMenuButton
            className="text-sidebar-foreground/70"
            render={<Link to="/sessions" />}
          >
            <MoreHorizontalIcon className="text-sidebar-foreground/70" />
            <span>{t("nav.viewAll")}</span>
          </SidebarMenuButton>
        </SidebarMenuItem>
      </SidebarMenu>
    </SidebarGroup>
  )
}
