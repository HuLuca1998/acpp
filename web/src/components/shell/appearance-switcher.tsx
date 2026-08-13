import { useState } from "react"
import { useTranslation } from "react-i18next"

import { useTheme } from "@/components/shell/theme-provider"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { SidebarMenuButton, SidebarMenuItem } from "@/components/ui/sidebar"
import {
  PALETTES,
  PALETTE_SWATCHES,
  loadPalette,
  savePalette,
  type Palette,
} from "@/lib/palette"
import {
  CheckIcon,
  MonitorIcon,
  MoonIcon,
  SunIcon,
  SwatchBookIcon,
} from "lucide-react"

const THEME_OPTIONS = [
  { value: "light", icon: SunIcon },
  { value: "dark", icon: MoonIcon },
  { value: "system", icon: MonitorIcon },
] as const

/** 主题方案的双色预览：纸面色打底，右下角是主色。 */
function PaletteSwatch({ palette }: { palette: Palette }) {
  const swatch = PALETTE_SWATCHES[palette]
  return (
    <span
      aria-hidden="true"
      className="size-4 shrink-0 rounded-full ring-1 ring-foreground/15"
      style={{
        background: `linear-gradient(135deg, ${swatch.surface} 50%, ${swatch.primary} 50%)`,
      }}
    />
  )
}

/** 外观切换（侧栏项）：明暗模式 + 完整主题方案，即时生效并持久化。 */
export function AppearanceSwitcher() {
  const { t } = useTranslation()
  const { theme, setTheme } = useTheme()
  const [palette, setPalette] = useState<Palette>(() => loadPalette())

  function pickPalette(next: Palette) {
    savePalette(next)
    setPalette(next)
  }

  return (
    <SidebarMenuItem>
      <DropdownMenu>
        <DropdownMenuTrigger render={<SidebarMenuButton />}>
          <SwatchBookIcon />
          <span>{t("appearance.label")}</span>
        </DropdownMenuTrigger>
        <DropdownMenuContent side="right" align="end" className="w-44">
          <DropdownMenuGroup>
            <DropdownMenuLabel>{t("appearance.theme")}</DropdownMenuLabel>
            {THEME_OPTIONS.map(({ value, icon: Icon }) => (
              <DropdownMenuItem key={value} onClick={() => setTheme(value)}>
                <Icon />
                <span className="flex-1">{t(`appearance.${value}`)}</span>
                {theme === value ? <CheckIcon /> : null}
              </DropdownMenuItem>
            ))}
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuGroup>
            <DropdownMenuLabel>{t("appearance.palette")}</DropdownMenuLabel>
            {PALETTES.map((option) => (
              <DropdownMenuItem
                key={option}
                onClick={() => pickPalette(option)}
              >
                <PaletteSwatch palette={option} />
                <span className="flex-1">
                  {t(`appearance.palettes.${option}`)}
                </span>
                {palette === option ? <CheckIcon /> : null}
              </DropdownMenuItem>
            ))}
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>
    </SidebarMenuItem>
  )
}
