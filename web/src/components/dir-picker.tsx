import { useCallback, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { CloneDialog } from "@/components/projects/clone-dialog"
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
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import {
  ArrowUpIcon,
  DownloadCloudIcon,
  FileIcon,
  FolderIcon,
  FolderPlusIcon,
} from "lucide-react"

/**
 * 目录/文件选择器：后端代劳列目录的导航弹窗。
 * 浏览器拿不到本地路径（File System Access API 只给 handle），
 * 所以只能走后端 /api/fs/dirs 导航；选中的始终是绝对路径。
 * mode="file" 时连同文件一起列，点文件即选中（@ 引用用）。
 */
export function DirPicker({
  open,
  onOpenChange,
  initialPath,
  onSelect,
  mode = "dir",
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 起始目录；空则从家目录开始。 */
  initialPath?: string
  onSelect: (path: string) => void
  mode?: "dir" | "file"
}) {
  const { t } = useTranslation()
  const [listing, setListing] = useState<DirListing | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [newName, setNewName] = useState("")
  const [createError, setCreateError] = useState<string | null>(null)
  const [cloneOpen, setCloneOpen] = useState(false)
  // 克隆是后台任务：盯到它结束，成功就直接把选择器带进新仓库目录——
  // 用户点「克隆」的意图就是要在那儿干活，不该让他自己再翻一遍目录。
  const watching = useRef<string | null>(null)

  const load = useCallback(
    async (path?: string) => {
      setLoading(true)
      setError(null)
      // 导航时收起新建输入行，避免残留在与之无关的目录上
      setCreating(false)
      setNewName("")
      setCreateError(null)
      try {
        setListing(await api.fs.dirs(path, mode === "file"))
      } catch (err) {
        setError((err as Error).message)
      } finally {
        setLoading(false)
      }
    },
    [mode]
  )

  const createFolder = useCallback(async () => {
    const name = newName.trim()
    if (!listing || !name) return
    setCreateError(null)
    try {
      // 建完直接进入新目录，底部「选择此目录」即可选中
      const entry = await api.fs.createDir(listing.path, name)
      await load(entry.path)
    } catch (err) {
      setCreateError((err as Error).message)
    }
  }, [listing, newName, load])

  // 每次打开都从起始目录重新进入，上一次的浏览位置不残留。
  // 放进微任务，避免在 effect 内同步 setState 触发级联渲染。
  useEffect(() => {
    if (!open) return
    queueMicrotask(() => void load(initialPath || undefined))
  }, [open, initialPath, load])

  const watchClone = useCallback(
    (id: string, path: string) => {
      watching.current = id
      const timer = setInterval(() => {
        void api.projects
          .clones()
          .then((res) => {
            const task = res.items.find((item) => item.id === id)
            if (!task || task.state === "running") return
            clearInterval(timer)
            watching.current = null
            if (task.state === "done") {
              toast.success(t("projects.cloneDone", { name: task.name }))
              void load(path)
            } else {
              toast.error(task.error || t("projects.cloneFailed"))
            }
          })
          .catch(() => {
            // 轮询失败下一轮再来。
          })
      }, 3000)
    },
    [load, t]
  )

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>
            {mode === "file" ? t("dirPicker.fileTitle") : t("dirPicker.title")}
          </DialogTitle>
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
          <span
            className="min-w-0 flex-1 truncate font-mono"
            title={listing?.path}
          >
            {listing?.path ?? ""}
          </span>
          {mode === "dir" ? (
            <>
              {/* 选工作目录时最常见的下一步其实是「先把仓库弄下来」——
                  克隆入口放在这里，不用先去别处建好项目再回来选。 */}
              <Button
                size="icon-sm"
                variant="outline"
                aria-label={t("projects.clone")}
                title={t("projects.clone")}
                onClick={() => setCloneOpen(true)}
              >
                <DownloadCloudIcon className="size-3.5" />
              </Button>
              <Button
                size="icon-sm"
                variant="outline"
                aria-label={t("dirPicker.newFolder")}
                disabled={loading || !listing}
                onClick={() => {
                  setCreating(true)
                  setCreateError(null)
                }}
              >
                <FolderPlusIcon className="size-3.5" />
              </Button>
            </>
          ) : null}
        </div>

        {creating ? (
          // 真表单：回车创建走浏览器原生的隐式提交，不手拦 keydown
          <form
            className="flex flex-col gap-1"
            onSubmit={(e) => {
              e.preventDefault()
              void createFolder()
            }}
          >
            <div className="flex items-center gap-2">
              <Input
                autoFocus
                value={newName}
                placeholder={t("dirPicker.newFolderName")}
                className="h-8 text-sm"
                onChange={(e) => setNewName(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Escape") {
                    // 只收起输入行，不让 Esc 冒泡关掉整个弹窗
                    e.stopPropagation()
                    setCreating(false)
                    setNewName("")
                    setCreateError(null)
                  }
                }}
              />
              <Button type="submit" size="sm" disabled={!newName.trim()}>
                {t("dirPicker.create")}
              </Button>
            </div>
            {createError ? (
              <p className="text-xs text-destructive">{createError}</p>
            ) : null}
          </form>
        ) : null}

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
          ) : listing &&
            listing.dirs.length === 0 &&
            (listing.files?.length ?? 0) === 0 ? (
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
              {(listing?.files ?? []).map((file) => (
                <li key={file.path}>
                  <button
                    type="button"
                    className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-muted focus-visible:bg-muted focus-visible:outline-none"
                    onClick={() => {
                      onSelect(file.path)
                      onOpenChange(false)
                    }}
                  >
                    <FileIcon className="size-4 shrink-0 text-muted-foreground" />
                    <span className="truncate">{file.name}</span>
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
          {mode === "dir" ? (
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
          ) : null}
        </DialogFooter>
      </DialogContent>

      <CloneDialog
        open={cloneOpen}
        onOpenChange={setCloneOpen}
        onCloned={(task) => {
          toast.info(t("projects.cloning"))
          watchClone(task.id, task.path)
        }}
      />
    </Dialog>
  )
}
