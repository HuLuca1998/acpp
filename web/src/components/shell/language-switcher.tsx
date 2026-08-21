import { useTranslation } from "react-i18next"

import {
  DropdownMenuGroup,
  DropdownMenuItem,
} from "@/components/ui/dropdown-menu"
import { SUPPORTED_LANGUAGES, type Language } from "@/i18n"
import { CheckIcon } from "lucide-react"

/**
 * 语言选项。只导出菜单**内容**，壳长在用户菜单的子菜单里
 * （shell/nav-user.tsx，理由见 AppearanceMenuItems）。
 */
export function LanguageMenuItems() {
  const { t, i18n } = useTranslation()
  const current = i18n.resolvedLanguage as Language

  return (
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
  )
}
