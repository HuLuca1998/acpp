import { useTranslation } from "react-i18next"

import { Hint } from "@/components/hint"
import type { ImageAttachment } from "@/types/acp"
import {
  Attachment,
  AttachmentAction,
  AttachmentActions,
  AttachmentContent,
  AttachmentGroup,
  AttachmentMedia,
  AttachmentTitle,
} from "@/components/ui/attachment"
import { DatabaseIcon, FileIcon, XIcon } from "lucide-react"

/**
 * 待发送附件预览条：图片缩略图 + @ 引用的文件与数据库，各带移除按钮。
 * 渲染在输入卡内 textarea 上方，空时不占位。
 *
 * 用 AttachmentGroup 而不是 flex-wrap：附件多了横向滚动，不会一行行把
 * 输入框往上顶。
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
    <AttachmentGroup className="px-4 pt-3">
      {images.map((img, index) => (
        // 竖版只有缩略图，移除按钮压在右上角；w-14 保持原来的 56px，
        // 默认的 w-24 会把输入卡顶高一截。
        <Attachment
          key={index}
          size="sm"
          orientation="vertical"
          className="w-14"
        >
          <AttachmentMedia variant="image">
            <img src={`data:${img.mimeType};base64,${img.data}`} alt="" />
          </AttachmentMedia>
          <AttachmentActions className="top-1 right-1">
            <Hint label={t("chat.attachments.remove")}>
              <AttachmentAction
                aria-label={t("chat.attachments.remove")}
                className="size-4 rounded-full bg-foreground text-background opacity-0 group-hover/attachment:opacity-100 hover:bg-foreground hover:text-background focus-visible:opacity-100"
                onClick={() => onRemoveImage(index)}
              >
                <XIcon className="size-3" />
              </AttachmentAction>
            </Hint>
          </AttachmentActions>
        </Attachment>
      ))}

      {files.map((path, index) => (
        <RefChip
          key={path}
          icon={<FileIcon />}
          full={path}
          onRemove={() => onRemoveFile(index)}
        />
      ))}

      {dbRefs.map((ref, index) => (
        <RefChip
          key={ref}
          icon={<DatabaseIcon />}
          full={ref}
          onRemove={() => onRemoveDbRef(index)}
        />
      ))}
    </AttachmentGroup>
  )
}

/**
 * 一条 @ 引用（文件或数据库）。
 *
 * 只显示最后一段——文件是文件名，数据库是库名或表名；前缀（目录、
 * 项目/环境）在同一条会话里基本恒定，完整值留给悬停。
 */
function RefChip({
  icon,
  full,
  onRemove,
}: {
  icon: React.ReactNode
  full: string
  onRemove: () => void
}) {
  const { t } = useTranslation()
  return (
    <Attachment size="xs" title={full} className="min-w-0">
      <AttachmentMedia>{icon}</AttachmentMedia>
      <AttachmentContent>
        <AttachmentTitle className="max-w-40 truncate font-mono">
          {full.split("/").pop()}
        </AttachmentTitle>
      </AttachmentContent>
      <AttachmentActions className="pr-1">
        <Hint label={t("chat.attachments.remove")}>
          <AttachmentAction
            aria-label={t("chat.attachments.remove")}
            onClick={onRemove}
          >
            <XIcon />
          </AttachmentAction>
        </Hint>
      </AttachmentActions>
    </Attachment>
  )
}
