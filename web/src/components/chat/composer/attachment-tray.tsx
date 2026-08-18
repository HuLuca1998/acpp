import { useTranslation } from "react-i18next"

import type { ImageAttachment } from "@/types/acp"
import { DatabaseIcon, FileIcon, XIcon } from "lucide-react"

/**
 * 待发送附件预览条：图片缩略图 + @ 引用的文件与数据库，各带移除按钮。
 * 渲染在输入卡内 textarea 上方，空时不占位。
 */
export function AttachmentTray({
  images,
  files,
  dbRefs,
  onRemoveImage,
  onRemoveFile,
  onRemoveDbRef,
}: {
  images: ImageAttachment[]
  files: string[]
  dbRefs: string[]
  onRemoveImage: (index: number) => void
  onRemoveFile: (index: number) => void
  onRemoveDbRef: (index: number) => void
}) {
  const { t } = useTranslation()
  if (images.length === 0 && files.length === 0 && dbRefs.length === 0) {
    return null
  }

  return (
    <div className="flex flex-wrap items-center gap-2 px-4 pt-3">
      {images.map((img, index) => (
        <div key={index} className="group relative">
          <img
            src={`data:${img.mimeType};base64,${img.data}`}
            alt=""
            className="h-14 w-14 rounded-lg border border-border object-cover"
          />
          <button
            type="button"
            aria-label={t("chat.attachments.remove")}
            className="absolute -top-1.5 -right-1.5 flex size-4 items-center justify-center rounded-full bg-foreground text-background opacity-0 transition-opacity duration-150 group-hover:opacity-100 focus-visible:opacity-100"
            onClick={() => onRemoveImage(index)}
          >
            <XIcon className="size-3" />
          </button>
        </div>
      ))}
      {files.map((path, index) => (
        <span
          key={path}
          className="flex h-7 items-center gap-1.5 rounded-full border border-border px-2.5 text-xs text-muted-foreground"
          title={path}
        >
          <FileIcon className="size-3.5 shrink-0" />
          <span className="max-w-40 truncate font-mono">
            {path.split("/").pop()}
          </span>
          <button
            type="button"
            aria-label={t("chat.attachments.remove")}
            className="text-muted-foreground transition-colors hover:text-foreground"
            onClick={() => onRemoveFile(index)}
          >
            <XIcon className="size-3" />
          </button>
        </span>
      ))}
      {dbRefs.map((ref, index) => (
        <span
          key={ref}
          className="flex h-7 items-center gap-1.5 rounded-full border border-border px-2.5 text-xs text-muted-foreground"
          title={ref}
        >
          <DatabaseIcon className="size-3.5 shrink-0" />
          {/* 只显示最后一段（库名或表名）：前缀是项目/环境，同一条会话里
              基本恒定，完整引用留给悬停。 */}
          <span className="max-w-40 truncate font-mono">
            {ref.split("/").pop()}
          </span>
          <button
            type="button"
            aria-label={t("chat.attachments.remove")}
            className="text-muted-foreground transition-colors hover:text-foreground"
            onClick={() => onRemoveDbRef(index)}
          >
            <XIcon className="size-3" />
          </button>
        </span>
      ))}
    </div>
  )
}
