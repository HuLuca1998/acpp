import { useTranslation } from "react-i18next"
import { Link, useLocation } from "react-router"

import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import {
  SidebarGroup,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuAction,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
} from "@/components/ui/sidebar"
import { StatusDot } from "@/components/status-dot"
import { SESSION_STATE_TONE } from "@/lib/status-tone"
import type { SessionGroup } from "@/lib/session-groups"
import { ChevronRightIcon, PlusIcon } from "lucide-react"

/**
 * 侧边栏「最近会话」——按**工作目录**分组（adr-007）。
 *
 * 平铺一串会话标题在多工作区场景里几乎不可读：同名的「修 bug」会话可能
 * 来自三个目录。分组后目录名承担了大部分定位工作，标题只需要区分组内。
 * 当前所在的那组默认展开，其余收起。
 */
export function NavProjects({
  label,
  groups,
}: {
  label: string
  groups: SessionGroup[]
}) {
  const { t } = useTranslation()
  const { pathname } = useLocation()

  return (
    <SidebarGroup className="group-data-[collapsible=icon]:hidden">
      <SidebarGroupLabel>{label}</SidebarGroupLabel>
      <SidebarMenu>
        {groups.map(({ cwd, label, sessions }) => {
          const active = sessions.some(
            (session) => pathname === `/sessions/${session.id}`
          )
          return (
            <Collapsible
              key={cwd}
              defaultOpen={active}
              className="group/collapsible"
            >
              <SidebarMenuItem>
                {/* 组标题是轻量小节头（muted 小字），不做成实心按钮——
                    目录名只负责定位，视觉重心留给组内的会话条目。 */}
                <CollapsibleTrigger
                  render={
                    <SidebarMenuButton className="text-xs text-sidebar-foreground/70 hover:text-sidebar-foreground" />
                  }
                >
                  {/* 只显示目录短名，完整路径进 title——侧边栏宽度有限。 */}
                  <span className="truncate font-medium" title={cwd}>
                    {label}
                  </span>
                  <ChevronRightIcon className="size-3! shrink-0 transition-transform duration-150 ease-snappy group-data-[open]/collapsible:rotate-90" />
                </CollapsibleTrigger>
                {/* 在这个目录开新会话：草稿页认 ?cwd= 预填（adr-007 的项目
                    入口同一机制）。行内有 action 时按钮自动 pr-8 让位，
                    不要再手动偏移。常显但压低存在感，hover 才上色。 */}
                <SidebarMenuAction
                  className="text-sidebar-foreground/50 hover:text-sidebar-foreground"
                  render={
                    <Link to={`/sessions/new?cwd=${encodeURIComponent(cwd)}`} />
                  }
                  aria-label={t("nav.newSessionIn")}
                  title={t("nav.newSessionIn")}
                >
                  <PlusIcon />
                </SidebarMenuAction>
                <CollapsibleContent>
                  <SidebarMenuSub>
                    {sessions.map((session) => (
                      <SidebarMenuSubItem key={session.id}>
                        <SidebarMenuSubButton
                          isActive={pathname === `/sessions/${session.id}`}
                          render={<Link to={`/sessions/${session.id}`} />}
                        >
                          {/* 正在对话的会话亮绿点呼吸，静止的灰点（§5.3）。 */}
                          <StatusDot
                            tone={SESSION_STATE_TONE[session.state]}
                            pulse={session.state === "active"}
                          />
                          <span className="truncate">
                            {session.title ||
                              `${t("common.unnamed")} #${session.id}`}
                          </span>
                        </SidebarMenuSubButton>
                      </SidebarMenuSubItem>
                    ))}
                  </SidebarMenuSub>
                </CollapsibleContent>
              </SidebarMenuItem>
            </Collapsible>
          )
        })}
      </SidebarMenu>
    </SidebarGroup>
  )
}
