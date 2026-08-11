import { useState } from "react"
import { useTranslation } from "react-i18next"
import { MoreHorizontalIcon } from "lucide-react"

import { useWorkspace } from "@/components/workspace/workspace-context"
import {
  PANEL_ICONS,
  TOGGLEABLE_PANELS,
  type WorkspacePanelId,
} from "@/components/workspace/workspace-panels"
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

/**
 * ⋯ 窗口管理菜单：其余 5 类面板的显隐勾选 + 恢复默认布局。
 * 勾选状态在菜单打开瞬间从 dockview 现读，不常驻订阅布局事件。
 */
export function WorkspaceMenu({
  onResetLayout,
}: {
  onResetLayout: () => void
}) {
  const { t } = useTranslation()
  const ws = useWorkspace()
  const [openPanels, setOpenPanels] = useState<Set<WorkspacePanelId>>(new Set())

  return (
    <DropdownMenu
      onOpenChange={(open) => {
        if (!open) return
        const api = wsApiPanels(ws)
        setOpenPanels(api)
      }}
    >
      <DropdownMenuTrigger
        aria-label={t("workspace.menu.label")}
        className="flex size-6 items-center justify-center rounded-md text-muted-foreground transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97]"
      >
        <MoreHorizontalIcon className="size-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-48">
        {/* Base UI 的 GroupLabel 必须住在 Group 里，直接平铺会抛错。 */}
        <DropdownMenuGroup>
          <DropdownMenuLabel>{t("workspace.menu.windows")}</DropdownMenuLabel>
          {TOGGLEABLE_PANELS.map((id) => {
          const Icon = PANEL_ICONS[id]
          const open = openPanels.has(id)
          return (
            <DropdownMenuCheckboxItem
              key={id}
              checked={open}
              onCheckedChange={() => {
                if (open) {
                  ws.closePanel(id)
                } else {
                  ws.ensureOpen(id)
                }
                setOpenPanels((prev) => {
                  const next = new Set(prev)
                  if (open) {
                    next.delete(id)
                  } else {
                    next.add(id)
                  }
                  return next
                })
              }}
            >
              <Icon className="size-3.5 text-muted-foreground" />
              {t(`workspace.panels.${id}` as never)}
              </DropdownMenuCheckboxItem>
            )
          })}
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={onResetLayout}>
          {t("workspace.menu.resetLayout")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function wsApiPanels(ws: ReturnType<typeof useWorkspace>) {
  const open = new Set<WorkspacePanelId>()
  for (const id of TOGGLEABLE_PANELS) {
    if (ws.isOpen(id)) open.add(id)
  }
  return open
}
