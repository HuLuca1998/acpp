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
 * 侧边栏「最近会话」——按项目分组（adr-007）。
 *
 * 平铺一串会话标题在多项目场景里几乎不可读：同名的「修 bug」会话可能来自
 * 三个仓库。分组后项目名承担了大部分定位工作，标题只需要区分组内。
 * 当前所在项目默认展开，其余收起。
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
        {groups.map(({ project, sessions }) => {
          const active = sessions.some(
            (session) => pathname === `/sessions/${session.id}`
          )
          return (
            <Collapsible
              key={project.name}
              defaultOpen={active}
              className="group/collapsible"
            >
              <SidebarMenuItem>
                <CollapsibleTrigger render={<SidebarMenuButton />}>
                  <FolderGitIcon />
                  {/* 只显示仓库名，组织名让位给标题——侧边栏宽度有限，
                      `BDBGAME2024/pp-game` 折行反而更难认。 */}
                  <span className="truncate" title={project.name}>
                    {repoName(project.name)}
                  </span>
                  {project.branch ? (
                    <span className="ml-auto truncate font-mono text-[10px] text-sidebar-foreground/50">
                      {project.branch}
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

/** `组织/仓库` → `仓库`；没有组织段时原样返回。 */
function repoName(name: string): string {
  const slash = name.lastIndexOf("/")
  return slash < 0 ? name : name.slice(slash + 1)
}
