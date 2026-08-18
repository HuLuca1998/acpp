import { useTranslation } from "react-i18next"

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { AtSignIcon, DatabaseIcon, FileIcon } from "lucide-react"

/**
 * composer 左下角的附件圆钮与 @ 引用菜单。普通会话面板与编排面板共用
 * ——两处的输入区是同一件东西，长得不一样只会让人以为功能也不一样。
 */
const buttonClass =
  "flex size-7 items-center justify-center rounded-full text-muted-foreground transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97]"

export function AttachmentButton({
  label,
  onClick,
  children,
}: {
  label: string
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      aria-label={label}
      className={buttonClass}
      onClick={onClick}
    >
      {children}
    </button>
  )
}

/**
 * @ 引用菜单：文件与数据库两种。
 *
 * 收在同一个入口下是因为它们是同一个动作——「把这个交给 AI 看」，
 * 只是内容一个来自磁盘、一个来自库。
 */
export function ReferenceMenu({
  onPickFile,
  onPickDatabase,
}: {
  onPickFile: () => void
  onPickDatabase: () => void
}) {
  const { t } = useTranslation()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            aria-label={t("chat.attachments.file")}
            className={buttonClass}
          >
            <AtSignIcon className="size-3.5" />
          </button>
        }
      />
      <DropdownMenuContent align="start" className="min-w-40">
        <DropdownMenuItem onClick={onPickFile}>
          <FileIcon />
          {t("db.refFile")}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={onPickDatabase}>
          <DatabaseIcon />
          {t("db.refDatabase")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
