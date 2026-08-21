import { useTranslation } from "react-i18next"

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Hint } from "@/components/hint"
import { Button } from "@/components/ui/button"
import { SidebarMenuButton, SidebarMenuItem } from "@/components/ui/sidebar"
import { SUPPORTED_LANGUAGES, type Language } from "@/i18n"
import { CheckIcon, LanguagesIcon } from "lucide-react"

/** 语言切换。iconOnly 是侧栏底部的紧凑形态（见 AppearanceSwitcher）。 */
export function LanguageSwitcher({ iconOnly }: { iconOnly?: boolean }) {
  const { t, i18n } = useTranslation()
  const current = i18n.resolvedLanguage as Language

  const menu = (
    <DropdownMenu>
      {iconOnly ? (
        <Hint label={t("language.label")}>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="icon"
                className="size-7 text-muted-foreground hover:text-foreground"
                aria-label={t("language.label")}
              />
            }
          >
            <LanguagesIcon className="size-4" />
          </DropdownMenuTrigger>
        </Hint>
      ) : (
        <DropdownMenuTrigger render={<SidebarMenuButton />}>
          <LanguagesIcon />
          <span>{t("language.label")}</span>
        </DropdownMenuTrigger>
      )}
        <DropdownMenuContent side="right" align="end" className="w-36">
          <DropdownMenuGroup>
            {SUPPORTED_LANGUAGES.map((lang) => (
              <DropdownMenuItem
                key={lang}
                onClick={() => void i18n.changeLanguage(lang)}
              >
                <span className="flex-1">{t(`language.${lang}`)}</span>
                {current === lang ? <CheckIcon /> : null}
              </DropdownMenuItem>
            ))}
          </DropdownMenuGroup>
        </DropdownMenuContent>
    </DropdownMenu>
  )

  return iconOnly ? menu : <SidebarMenuItem>{menu}</SidebarMenuItem>
}
