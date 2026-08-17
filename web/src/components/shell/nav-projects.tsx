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
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
} from "@/components/ui/sidebar"
import type { SessionGroup } from "@/lib/session-groups"
import { ChevronRightIcon, FolderGitIcon } from "lucide-react"

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
        {groups.map(({ cwd, label, branch, sessions }) => {
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
                <CollapsibleTrigger render={<SidebarMenuButton />}>
                  <FolderGitIcon />
                  {/* 只显示目录短名，完整路径进 title——侧边栏宽度有限。 */}
                  <span className="truncate" title={cwd}>
                    {label}
                  </span>
                  {branch ? (
                    <span className="ml-auto truncate font-mono text-[10px] text-sidebar-foreground/50">
                      {branch}
                    </span>
                  ) : null}
                  <ChevronRightIcon className="ml-1 shrink-0 transition-transform duration-150 ease-snappy group-data-[open]/collapsible:rotate-90" />
                </CollapsibleTrigger>
                <CollapsibleContent>
                  <SidebarMenuSub>
                    {sessions.map((session) => (
                      <SidebarMenuSubItem key={session.id}>
                        <SidebarMenuSubButton
                          isActive={pathname === `/sessions/${session.id}`}
                          render={<Link to={`/sessions/${session.id}`} />}
                        >
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
