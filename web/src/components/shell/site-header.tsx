import { useTranslation } from "react-i18next"

import { Hint } from "@/components/hint"
import { NotifyMenu } from "@/components/shell/notify-menu"
import { Kbd } from "@/components/ui/kbd"
import { Separator } from "@/components/ui/separator"
import { SidebarTrigger } from "@/components/ui/sidebar"

export function SiteHeader({
  title,
  children,
}: {
  title: string
  children?: React.ReactNode
}) {
  const { t } = useTranslation()
  return (
    <header className="flex h-(--header-height) shrink-0 items-center gap-2 border-b transition-[width,height] ease-linear group-has-data-[collapsible=icon]/sidebar-wrapper:h-(--header-height)">
      <div className="flex w-full items-center gap-1 px-4 lg:gap-2 lg:px-6">
        <Hint
          label={t("nav.toggleSidebar")}
          shortcut={<Kbd>⌘B</Kbd>}
          align="start"
        >
          <SidebarTrigger
            className="-ml-1"
            aria-label={t("nav.toggleSidebar")}
          />
        </Hint>
        <Separator
          orientation="vertical"
          className="mx-2 h-4 data-vertical:self-auto"
        />
        <h1 className="text-[15px] font-semibold tracking-tight">{title}</h1>
        {/* 通知开关常驻最右：局域网访客进不了设置页（adr-007），这是两种
            身份都够得着的唯一位置。 */}
        <div className="ml-auto flex items-center gap-2">
          {children}
          <NotifyMenu />
        </div>
      </div>
    </header>
  )
}
