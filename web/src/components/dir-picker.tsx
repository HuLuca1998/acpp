import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"

import { api } from "@/lib/api"
import type { DirListing } from "@/types/acp"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Empty, EmptyDescription, EmptyHeader } from "@/components/ui/empty"
import { Spinner } from "@/components/ui/spinner"
import { ArrowUpIcon, FolderIcon } from "lucide-react"

/**
 * 工作目录选择器：后端代劳列目录的导航弹窗。
 * 浏览器拿不到本地文件夹的绝对路径（File System Access API 只给 handle），
 * 所以只能走后端 /api/fs/dirs 导航；选中的始终是绝对路径。
 */
export function DirPicker({
  open,
  onOpenChange,
  initialPath,
  onSelect,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 起始目录；空则从家目录开始。 */
  initialPath?: string
  onSelect: (path: string) => void
}) {
  const { t } = useTranslation()
  const [listing, setListing] = useState<DirListing | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async (path?: string) => {
    setLoading(true)
    setError(null)
    try {
      setListing(await api.fs.dirs(path))
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  // 每次打开都从起始目录重新进入，上一次的浏览位置不残留。
  // 放进微任务，避免在 effect 内同步 setState 触发级联渲染。
  useEffect(() => {
    if (!open) return
    queueMicrotask(() => void load(initialPath || undefined))
  }, [open, initialPath, load])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{t("dirPicker.title")}</DialogTitle>
        </DialogHeader>

        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <Button
            size="icon-sm"
            variant="outline"
            aria-label={t("dirPicker.up")}
            disabled={loading || !listing?.parent}
            onClick={() => listing?.parent && void load(listing.parent)}
          >
            <ArrowUpIcon className="size-3.5" />
          </Button>
          <span className="min-w-0 truncate font-mono" title={listing?.path}>
            {listing?.path ?? ""}
          </span>
        </div>

        <div className="h-64 overflow-y-auto rounded-lg border border-border">
          {loading ? (
            <div className="flex h-full items-center justify-center">
              <Spinner className="size-5" />
            </div>
          ) : error ? (
            <Empty className="h-full justify-center">
              <EmptyHeader>
                <EmptyDescription>{error}</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : listing && listing.dirs.length === 0 ? (
            <Empty className="h-full justify-center">
              <EmptyHeader>
                <EmptyDescription>{t("dirPicker.empty")}</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <ul className="p-1">
              {listing?.dirs.map((dir) => (
                <li key={dir.path}>
                  <button
                    type="button"
                    className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-muted focus-visible:bg-muted focus-visible:outline-none"
                    onClick={() => void load(dir.path)}
                  >
                    <FolderIcon className="size-4 shrink-0 text-muted-foreground" />
                    <span className="truncate">{dir.name}</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("dirPicker.cancel")}
          </Button>
          <Button
            disabled={!listing || loading}
            onClick={() => {
              if (!listing) return
              onSelect(listing.path)
              onOpenChange(false)
            }}
          >
            {t("dirPicker.select")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
