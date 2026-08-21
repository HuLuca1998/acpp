import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"
import {
  ChevronsUpDownIcon,
  LanguagesIcon,
  PlugZapIcon,
  RefreshCwIcon,
  Settings2Icon,
  SwatchBookIcon,
  UserRoundIcon,
} from "lucide-react"

import { api } from "@/lib/api"
import { useVersionWatch } from "@/hooks/use-version-watch"
import { AppearanceMenuItems } from "@/components/shell/appearance-switcher"
import { LanguageMenuItems } from "@/components/shell/language-switcher"
import { StatusDot } from "@/components/status-dot"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar"

type Health = { status: "ok"; version: string } | { status: "down" } | null

/**
 * 侧栏底部的用户条目：身份 + 全部次级入口收进一个菜单（shadcn 的
 * NavUser 模式）。底部只留这一行，省下来的空间都归通知中心。
 *
 * 菜单内容按身份裁剪（adr-007）：设置与连接是 owner 的，租户只见
 * 外观与语言。后端状态也收在菜单底部——行上只留一个状态点提示
 * （绿=连着、红=不可达、黄=有新版本），细节点开看。
 *
 * 后端换版本后菜单底部的状态行变成「刷新」入口；通知中心里那张
 * update 卡是主入口，这里是常驻的兜底（通知可以被划掉，状态不该跟着丢）。
 */
export function NavUser({
  whoami,
  isOwner,
}: {
  whoami: string
  isOwner: boolean
}) {
  const { t } = useTranslation()
  const [health, setHealth] = useState<Health>(null)
  const updated = useVersionWatch()

  useEffect(() => {
    let cancelled = false
    api
      .health()
      .then((res) => {
        if (!cancelled) setHealth({ status: "ok", version: res.version })
      })
      .catch(() => {
        if (!cancelled) setHealth({ status: "down" })
      })
    return () => {
      cancelled = true
    }
  }, [])

  const tone = updated
    ? ("warning" as const)
    : health?.status === "ok"
      ? ("success" as const)
      : ("destructive" as const)

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={<SidebarMenuButton className="h-9" />}
          >
            <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-muted">
              <UserRoundIcon className="size-3.5 text-muted-foreground" />
            </span>
            <span className="flex-1 truncate text-sm">{whoami}</span>
            <StatusDot tone={tone} pulse={tone === "success"} />
            <ChevronsUpDownIcon className="size-3.5 text-muted-foreground" />
          </DropdownMenuTrigger>

          <DropdownMenuContent side="right" align="end" className="w-52">
            {/* Base UI 的 Label 必须待在 Group 里，散放会直接抛错。 */}
            <DropdownMenuGroup>
              <DropdownMenuLabel>{whoami}</DropdownMenuLabel>
            </DropdownMenuGroup>
            <DropdownMenuSeparator />

            {/* 设置与连接是 owner 的（adr-007），租户的菜单里不出现。 */}
            {isOwner ? (
              <>
                <DropdownMenuGroup>
                  <DropdownMenuItem render={<Link to="/settings" />}>
                    <Settings2Icon />
                    {t("nav.settings")}
                  </DropdownMenuItem>
                  <DropdownMenuItem render={<Link to="/connections" />}>
                    <PlugZapIcon />
                    {t("nav.connections")}
                  </DropdownMenuItem>
                </DropdownMenuGroup>
                <DropdownMenuSeparator />
              </>
            ) : null}

            <DropdownMenuGroup>
              <DropdownMenuSub>
                <DropdownMenuSubTrigger>
                  <SwatchBookIcon />
                  {t("appearance.label")}
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent className="w-44">
                  <AppearanceMenuItems />
                </DropdownMenuSubContent>
              </DropdownMenuSub>
              <DropdownMenuSub>
                <DropdownMenuSubTrigger>
                  <LanguagesIcon />
                  {t("language.label")}
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent className="w-36">
                  <LanguageMenuItems />
                </DropdownMenuSubContent>
              </DropdownMenuSub>
            </DropdownMenuGroup>

            <DropdownMenuSeparator />

            {/* 后端状态：有更新时是可点的刷新项，其余是纯信息行。 */}
            {updated ? (
              <DropdownMenuItem onClick={() => window.location.reload()}>
                <RefreshCwIcon className="text-warning" />
                <span className="flex-1">{t("backend.reload")}</span>
                <span className="font-mono text-xs tabular-nums">
                  v{updated}
                </span>
              </DropdownMenuItem>
            ) : (
              <div className="flex items-center justify-between gap-2 px-2 py-1.5 text-xs text-muted-foreground">
                {health?.status === "ok" ? (
                  <>
                    <StatusDot tone="success" pulse label={t("backend.connected")} />
                    <span className="font-mono">v{health.version}</span>
                  </>
                ) : (
                  <StatusDot
                    tone="destructive"
                    label={t("backend.unreachable")}
                  />
                )}
              </div>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  )
}
