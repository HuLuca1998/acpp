import { useTranslation } from "react-i18next"

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { SidebarMenuButton, SidebarMenuItem } from "@/components/ui/sidebar"
import { SUPPORTED_LANGUAGES, type Language } from "@/i18n"
import { CheckIcon, LanguagesIcon } from "lucide-react"

/** 语言切换（侧栏项）。 */
export function LanguageSwitcher() {
  const { t, i18n } = useTranslation()
  const current = i18n.resolvedLanguage as Language

  return (
    <SidebarMenuItem>
      <DropdownMenu>
        <DropdownMenuTrigger render={<SidebarMenuButton />}>
          <LanguagesIcon />
          <span>{t("language.label")}</span>
        </DropdownMenuTrigger>
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
    </SidebarMenuItem>
  )
}
