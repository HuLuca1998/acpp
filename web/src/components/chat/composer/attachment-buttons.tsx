import { useTranslation } from "react-i18next"

import { Hint } from "@/components/hint"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { AtSignIcon, DatabaseIcon, FileIcon, FileUpIcon } from "lucide-react"

/**
 * composer 左下角的附件圆钮与 @ 引用菜单。会话面板与草稿态共用
 * ——两处的输入区是同一件东西，长得不一样只会让人以为功能也不一样。
 */
const buttonClass =
  "flex size-7 items-center justify-center rounded-full text-muted-foreground transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97]"

export function AttachmentButton({
  label,
  desc,
  onClick,
  children,
}: {
  label: string
  /** 悬停说明的第二行：这枚圆钮按下去会发生什么。 */
  desc?: string
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <Hint label={label} desc={desc} align="start">
      <button
        type="button"
        aria-label={label}
        className={buttonClass}
        onClick={onClick}
      >
        {children}
      </button>
    </Hint>
  )
}

/**
 * @ 引用菜单：工作区文件、数据库、上传本机文件。
 *
 * 收在同一个入口下是因为它们是同一个动作——「把这个交给 AI 看」，
 * 只是内容一个来自工作区、一个来自库、一个来自你自己的机器。
 */
export function ReferenceMenu({
  onPickFile,
  onPickDatabase,
  onUpload,
}: {
  onPickFile: () => void
  onPickDatabase: () => void
  onUpload: () => void
}) {
  const { t } = useTranslation()
  return (
    <DropdownMenu>
      <Hint
        label={t("chat.attachments.reference")}
        desc={t("chat.attachments.referenceDesc")}
        align="start"
      >
        <DropdownMenuTrigger
          render={
            <button
              type="button"
              aria-label={t("chat.attachments.reference")}
              className={buttonClass}
            >
              <AtSignIcon className="size-3.5" />
            </button>
          }
        />
      </Hint>
      <DropdownMenuContent align="start" className="min-w-40">
        <DropdownMenuItem onClick={onPickFile}>
          <FileIcon />
          {t("db.refFile")}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={onPickDatabase}>
          <DatabaseIcon />
          {t("db.refDatabase")}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={onUpload}>
          <FileUpIcon />
          {t("upload.title")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
