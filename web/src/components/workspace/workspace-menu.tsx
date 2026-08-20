import { useState } from "react"
import { useTranslation } from "react-i18next"
import {
  BookmarkIcon,
  LayoutTemplateIcon,
  MoreHorizontalIcon,
  PlusIcon,
  SaveIcon,
  TerminalIcon,
  Trash2Icon,
} from "lucide-react"

import { Hint } from "@/components/hint"
import { LAYOUT_PRESETS } from "@/components/workspace/layout-presets"
import {
  deleteLayout,
  loadSavedLayouts,
  saveLayout,
  type SavedLayout,
} from "@/lib/saved-layouts"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { useWorkspace } from "@/components/workspace/workspace-context"
import {
  PANEL_ICONS,
  panelKindOf,
  TOGGLEABLE_PANELS,
  type WorkspacePanelKind,
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

interface TerminalEntry {
  panelId: string
  num: number
}

/**
 * ⋯ 窗口管理菜单：单例面板显隐勾选、终端实例列表 + 新建、恢复默认布局。
 * 状态在菜单打开瞬间从 dockview 现读，不常驻订阅布局事件。
 */
export function WorkspaceMenu({
  onResetLayout,
}: {
  onResetLayout: () => void
}) {
  const { t } = useTranslation()
  const ws = useWorkspace()
  const [openPanels, setOpenPanels] = useState<Set<WorkspacePanelKind>>(
    new Set()
  )
  const [terminals, setTerminals] = useState<TerminalEntry[]>([])
  const [saved, setSaved] = useState<SavedLayout[]>([])
  const [saveOpen, setSaveOpen] = useState(false)
  const [layoutName, setLayoutName] = useState("")

  /** 存当前布局：dockview 的序列化结果原样收好，应用时反序列化回去。 */
  function storeCurrent() {
    const dock = ws.getApi()
    if (!dock || !layoutName.trim()) return
    setSaved(saveLayout(layoutName, dock.toJSON()))
    setLayoutName("")
    setSaveOpen(false)
  }

  /** 应用一套自存布局。终端实例的 pty 还活着，面板照常重建。 */
  function applySaved(layout: SavedLayout) {
    const dock = ws.getApi()
    if (!dock) return
    try {
      dock.fromJSON(layout.layout)
    } catch {
      // 存的布局引用了已经不存在的面板类型（版本更迭）：删掉它，
      // 免得用户每次点都失败。
      setSaved(deleteLayout(layout.name))
    }
  }

  return (
    <>
      <DropdownMenu
        onOpenChange={(open) => {
          if (!open) return
          const opened = new Set<WorkspacePanelKind>()
          for (const id of TOGGLEABLE_PANELS) {
            if (ws.isOpen(id)) opened.add(id)
          }
          setOpenPanels(opened)
          setSaved(loadSavedLayouts())
          const dock = ws.getApi()
          setTerminals(
            (dock?.panels ?? [])
              .filter((p) => panelKindOf(p.id) === "terminal")
              .map((p) => ({
                panelId: p.id,
                num: (p.params as { num?: number })?.num ?? 0,
              }))
              .sort((a, b) => a.num - b.num)
          )
        }}
      >
        <Hint
          label={t("workspace.menu.label")}
          desc={t("workspace.menu.labelDesc")}
          align="end"
        >
          <DropdownMenuTrigger
            aria-label={t("workspace.menu.label")}
            className="flex size-6 items-center justify-center rounded-md text-muted-foreground transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97]"
          >
            <MoreHorizontalIcon className="size-4" />
          </DropdownMenuTrigger>
        </Hint>
        <DropdownMenuContent align="end" className="w-52">
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
          <DropdownMenuGroup>
            <DropdownMenuLabel>
              {t("workspace.panels.terminal")}
            </DropdownMenuLabel>
            {terminals.map((term) => (
              <DropdownMenuItem
                key={term.panelId}
                onClick={() => {
                  const dock = ws.getApi()
                  dock?.getPanel(term.panelId)?.api.setActive()
                }}
              >
                <TerminalIcon className="size-3.5 text-muted-foreground" />
                {term.num
                  ? `${t("workspace.panels.terminal")} ${term.num}`
                  : t("workspace.panels.terminal")}
              </DropdownMenuItem>
            ))}
            <DropdownMenuItem onClick={ws.newTerminal}>
              <PlusIcon className="size-3.5 text-muted-foreground" />
              {t("workspace.menu.newTerminal")}
            </DropdownMenuItem>
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuGroup>
            <DropdownMenuLabel>{t("workspace.menu.layouts")}</DropdownMenuLabel>
            {LAYOUT_PRESETS.map((preset) => (
              <DropdownMenuItem
                key={preset}
                onClick={() => ws.applyPreset(preset)}
              >
                <LayoutTemplateIcon className="size-3.5 text-muted-foreground" />
                {t(`workspace.layouts.${preset}` as never)}
              </DropdownMenuItem>
            ))}
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuGroup>
            <DropdownMenuLabel>
              {t("workspace.menu.myLayouts")}
            </DropdownMenuLabel>
            {saved.map((layout) => (
              <DropdownMenuItem
                key={layout.name}
                className="group/layout"
                onClick={() => applySaved(layout)}
              >
                <BookmarkIcon className="size-3.5 text-muted-foreground" />
                <span className="flex-1 truncate">{layout.name}</span>
                <button
                  type="button"
                  aria-label={t("common.delete")}
                  className="opacity-0 transition-opacity duration-150 group-hover/layout:opacity-100"
                  onClick={(e) => {
                    e.stopPropagation()
                    setSaved(deleteLayout(layout.name))
                  }}
                >
                  <Trash2Icon className="size-3.5 text-muted-foreground hover:text-destructive" />
                </button>
              </DropdownMenuItem>
            ))}
            <DropdownMenuItem onClick={() => setSaveOpen(true)}>
              <SaveIcon className="size-3.5 text-muted-foreground" />
              {t("workspace.menu.saveLayout")}
            </DropdownMenuItem>
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={onResetLayout}>
            {t("workspace.menu.resetLayout")}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <Dialog open={saveOpen} onOpenChange={setSaveOpen}>
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>{t("workspace.menu.saveLayout")}</DialogTitle>
            <DialogDescription>
              {t("workspace.menu.saveLayoutDesc")}
            </DialogDescription>
          </DialogHeader>
          <Input
            value={layoutName}
            autoFocus
            placeholder={t("workspace.menu.layoutNamePlaceholder")}
            onChange={(e) => setLayoutName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") storeCurrent()
            }}
          />
          <DialogFooter>
            <Button variant="outline" onClick={() => setSaveOpen(false)}>
              {t("common.cancel")}
            </Button>
            <Button onClick={storeCurrent} disabled={!layoutName.trim()}>
              {t("common.save")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
