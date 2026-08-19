import { useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { api } from "@/lib/api"
import { formatBytes, formatRelativeTime } from "@/lib/format"
import { useAsyncData } from "@/hooks/use-async-data"
import type { UploadedFile } from "@/types/acp"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Empty, EmptyDescription, EmptyHeader } from "@/components/ui/empty"
import {
  Item,
  ItemActions,
  ItemContent,
  ItemDescription,
  ItemMedia,
  ItemTitle,
} from "@/components/ui/item"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { FileUpIcon, Trash2Icon, UploadIcon } from "lucide-react"

/**
 * 上传本机文件并引用。
 *
 * 上传件落在各自身份的家目录下（owner 是工作区根，访客是自己的 root），
 * 拿到路径之后就是一个普通的 @ 文件引用——不为「上传的文件」另造一条通道。
 *
 * 传过的留着并列在这里：同一份数据往往要问好几轮，第二次不该再传一遍。
 * 后端按内容 hash 去重，重复上传不写盘也不占地方。
 */
export function UploadDialog({
  open,
  onOpenChange,
  onSelect,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 把这个文件的路径带进输入框的附件区。 */
  onSelect: (path: string) => void
}) {
  const { t, i18n } = useTranslation()
  const inputRef = useRef<HTMLInputElement>(null)
  const [busy, setBusy] = useState(false)
  const [dragging, setDragging] = useState(false)
  // key 变一次就重拉一次历史：上传/删除之后列表要跟上。
  const [version, setVersion] = useState(0)
  const { data, error } = useAsyncData(
    () =>
      open ? api.uploads.list().then((r) => r.items) : Promise.resolve([]),
    [open, version]
  )

  async function upload(files: FileList | File[]) {
    const list = [...files]
    if (list.length === 0) return
    setBusy(true)
    try {
      for (const file of list) {
        const saved = await api.uploads.create(file)
        // 复用的也照样引用——用户要的是「把这个文件给 AI 看」，
        // 至于盘上是不是新写了一份，与他无关。
        onSelect(saved.path)
      }
      setVersion((v) => v + 1)
      toast.success(t("upload.done", { count: list.length }))
      onOpenChange(false)
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  async function remove(file: UploadedFile) {
    try {
      await api.uploads.remove(file.hash, file.name)
      setVersion((v) => v + 1)
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("upload.title")}</DialogTitle>
          <DialogDescription>{t("upload.hint")}</DialogDescription>
        </DialogHeader>

        {/* 拖放区兼选择按钮：整块可点，拖进来也收。 */}
        <button
          type="button"
          disabled={busy}
          className={cn(
            "flex flex-col items-center justify-center gap-2 rounded-xl border border-dashed border-border px-4 py-8 text-sm text-muted-foreground transition-colors duration-150 ease-snappy hover:bg-muted/50 disabled:opacity-50",
            dragging && "border-primary bg-muted/50 text-foreground"
          )}
          onClick={() => inputRef.current?.click()}
          onDragOver={(e) => {
            e.preventDefault()
            setDragging(true)
          }}
          onDragLeave={() => setDragging(false)}
          onDrop={(e) => {
            e.preventDefault()
            setDragging(false)
            void upload(e.dataTransfer.files)
          }}
        >
          {busy ? (
            <Spinner className="size-5" />
          ) : (
            <FileUpIcon className="size-5" />
          )}
          {busy ? t("upload.uploading") : t("upload.drop")}
        </button>
        <input
          ref={inputRef}
          type="file"
          multiple
          className="hidden"
          onChange={(e) => {
            if (e.target.files) void upload(e.target.files)
            e.target.value = ""
          }}
        />

        <div className="flex flex-col gap-2">
          <p className="text-xs font-medium text-muted-foreground">
            {t("upload.history")}
          </p>
          {error ? (
            <p className="text-sm text-destructive">{error}</p>
          ) : !data ? (
            <Skeleton className="h-20 w-full" />
          ) : data.length === 0 ? (
            <Empty className="py-6">
              <EmptyHeader>
                <EmptyDescription>{t("upload.empty")}</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <ScrollArea className="max-h-64">
              <div className="flex flex-col gap-1">
                {data.map((file) => (
                  <Item
                    key={file.hash}
                    size="sm"
                    className="group cursor-pointer hover:bg-muted/50"
                    onClick={() => {
                      onSelect(file.path)
                      onOpenChange(false)
                    }}
                  >
                    <ItemMedia variant="icon">
                      <UploadIcon />
                    </ItemMedia>
                    <ItemContent className="gap-0.5">
                      <ItemTitle className="font-mono font-normal">
                        {file.name}
                      </ItemTitle>
                      <ItemDescription className="tabular-nums">
                        {formatBytes(file.size)} ·{" "}
                        {formatRelativeTime(file.uploadedAt, i18n.language)}
                      </ItemDescription>
                    </ItemContent>
                    <ItemActions>
                      <Button
                        size="icon-sm"
                        variant="ghost"
                        aria-label={t("common.delete")}
                        className="text-muted-foreground transition-colors hover:text-destructive"
                        onClick={(e) => {
                          // 行本身是「引用它」，删除不该顺带把文件也引用上。
                          e.stopPropagation()
                          void remove(file)
                        }}
                      >
                        <Trash2Icon />
                      </Button>
                    </ItemActions>
                  </Item>
                ))}
              </div>
            </ScrollArea>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
