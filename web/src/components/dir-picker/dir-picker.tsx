import { Fragment, useCallback, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { CloneDialog } from "@/components/projects/clone-dialog"
import { DirColumnsView } from "@/components/dir-picker/columns"
import { DirListView } from "@/components/dir-picker/list"
import { DirPlaces, type PinnedDir } from "@/components/dir-picker/places"
import { type DirSortKey } from "@/components/dir-picker/shared"
import { useIdentity } from "@/hooks/identity-context"
import { api } from "@/lib/api"
import type { DirListing, FsPlace } from "@/types/acp"
import { ScrollArea } from "@/components/ui/scroll-area"
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
  Columns2Icon,
  DownloadCloudIcon,
  EyeIcon,
  EyeOffIcon,
  FolderPlusIcon,
  LayoutListIcon,
  PinIcon,
  PinOffIcon,
} from "lucide-react"

/**
 * 把绝对路径拆成访达路径栏式的可点分段。落在 root 内时首段折成 `~`
 * （root 即身份的家：租户被钉死在自己的 root，首段之外的路径不存在；
 * owner 没有 root，从根逐段展示）。
 */
function crumbsOf(
  path: string,
  root?: string
): { label: string; path: string }[] {
  const out: { label: string; path: string }[] = []
  let rest = path
  if (root) {
    const prefix = root.endsWith("/") ? root : `${root}/`
    if (path === root || path.startsWith(prefix)) {
      out.push({ label: "~", path: root })
      rest = path.slice(root.length)
    }
  }
  let acc = out.length > 0 ? out[0].path : ""
  for (const seg of rest.split("/").filter(Boolean)) {
    acc = `${acc}/${seg}`
    out.push({ label: seg, path: acc })
  }
  return out
}

const PINS_KEY = "acpp.dirPicker.pins"
const VIEW_KEY = "acpp.dirPicker.view"

function readPins(): PinnedDir[] {
  try {
    const raw = JSON.parse(localStorage.getItem(PINS_KEY) ?? "[]") as unknown
    return Array.isArray(raw) ? (raw as PinnedDir[]) : []
  } catch {
    return []
  }
}

/**
 * 目录/文件选择器：后端代劳列目录的导航弹窗，全应用共用这一个。
 * 浏览器拿不到本地路径（File System Access API 只给 handle），
 * 所以只能走后端 /api/fs/dirs 导航；选中的始终是绝对路径。
 * mode="file" 时连同文件一起列，点文件即选中。
 *
 * 形态照访达：左侧「位置」栏（默认位置 + 用户固定的目录），右侧
 * 列表/分栏两种视图，路径栏分段可点。
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
  const { t, i18n } = useTranslation()
  const { identity } = useIdentity()
  // columns 是从起点到当前的每层 listing：列表视图只看最后一列，
  // 分栏视图整条展示——两个视图共享同一份导航状态，切换不丢层级。
  const [columns, setColumns] = useState<DirListing[]>([])
  const listing = columns.length > 0 ? columns[columns.length - 1] : null
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [newName, setNewName] = useState("")
  const [createError, setCreateError] = useState<string | null>(null)
  const [cloneOpen, setCloneOpen] = useState(false)
  const [showHidden, setShowHidden] = useState(false)
  const [sortKey, setSortKey] = useState<DirSortKey>("name")
  const [sortAsc, setSortAsc] = useState(true)
  const [view, setView] = useState<"list" | "columns">(
    () => (localStorage.getItem(VIEW_KEY) === "columns" ? "columns" : "list")
  )
  const [places, setPlaces] = useState<FsPlace[]>([])
  const [pins, setPins] = useState<PinnedDir[]>(() => readPins())

  const watching = useRef<string | null>(null)
  // 当前浏览的目录正被克隆时给出提示。git clone 先拉 .git 再 checkout 文件，
  // 中途目录里只有隐藏的 .git——列表看上去空空如也，不说一声就像是失败了。
  const [cloningHere, setCloningHere] = useState(false)

  const fetchListing = useCallback(
    (path?: string, hidden?: boolean) =>
      api.fs.dirs(path, mode === "file", hidden ?? showHidden),
    [mode, showHidden]
  )

  /** 导航：replace 整个层级重来（面包屑/位置/↑），或在第 at 列之后下钻。 */
  const navigate = useCallback(
    async (path: string | undefined, opts?: { at?: number; hidden?: boolean }) => {
      setLoading(true)
      setError(null)
      // 导航时收起新建输入行，避免残留在与之无关的目录上
      setCreating(false)
      setNewName("")
      setCreateError(null)
      try {
        const next = await fetchListing(path, opts?.hidden)
        setColumns((prev) =>
          opts?.at !== undefined ? [...prev.slice(0, opts.at + 1), next] : [next]
        )
        const clones = await api.projects
          .clones()
          .then((res) => res.items)
          .catch(() => [])
        setCloningHere(
          clones.some((c) => c.state === "running" && c.path === next.path)
        )
      } catch (err) {
        setError((err as Error).message)
      } finally {
        setLoading(false)
      }
    },
    [fetchListing]
  )

  /** 「上一层」优先折回上一列（分栏语义），折不动才整层重开。 */
  const goUp = useCallback(() => {
    const parent = listing?.parent
    if (!parent) return
    if (columns.length > 1 && columns[columns.length - 2].path === parent) {
      setColumns((prev) => prev.slice(0, -1))
      return
    }
    void navigate(parent)
  }, [columns, listing, navigate])

  const createFolder = useCallback(async () => {
    const name = newName.trim()
    if (!listing || !name) return
    setCreateError(null)
    try {
      // 建完直接进入新目录，底部「选择此目录」即可选中
      const entry = await api.fs.createDir(listing.path, name)
      await navigate(entry.path, { at: columns.length - 1 })
    } catch (err) {
      setCreateError((err as Error).message)
    }
  }, [listing, newName, columns.length, navigate])

  // 每次打开都从起始目录重新进入，上一次的浏览位置不残留。
  // 放进微任务，避免在 effect 内同步 setState 触发级联渲染。
  useEffect(() => {
    if (!open) return
    queueMicrotask(() => {
      void navigate(initialPath || undefined)
      api.fs
        .places()
        .then(setPlaces)
        .catch(() => setPlaces([]))
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, initialPath])

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
              void navigate(path)
            } else {
              toast.error(task.error || t("projects.cloneFailed"))
            }
          })
          .catch(() => {
            // 轮询失败下一轮再来。
          })
      }, 3000)
    },
    [navigate, t]
  )

  const pinned = pins.some((p) => p.path === listing?.path)
  const togglePin = () => {
    if (!listing) return
    const next = pinned
      ? pins.filter((p) => p.path !== listing.path)
      : [
          ...pins,
          {
            name: listing.path.split("/").filter(Boolean).pop() ?? listing.path,
            path: listing.path,
          },
        ]
    setPins(next)
    localStorage.setItem(PINS_KEY, JSON.stringify(next))
  }

  const switchView = (next: "list" | "columns") => {
    setView(next)
    localStorage.setItem(VIEW_KEY, next)
  }

  const pickFile = (path: string) => {
    onSelect(path)
    onOpenChange(false)
  }

  const empty =
    listing && listing.dirs.length === 0 && (listing.files?.length ?? 0) === 0

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-3xl">
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
            onClick={goUp}
          >
            <ArrowUpIcon className="size-3.5" />
          </Button>
          {/* 访达式路径栏：每一段都可点，长路径横向滚动不换行。 */}
          <div
            className="min-w-0 flex-1 overflow-x-auto font-mono whitespace-nowrap [scrollbar-width:none]"
            title={listing?.path}
          >
            {crumbsOf(listing?.path ?? "", identity?.root).map((crumb, i) => (
              <Fragment key={crumb.path}>
                {i > 0 ? (
                  <span className="mx-0.5 text-muted-foreground/50">/</span>
                ) : null}
                <button
                  type="button"
                  className="rounded px-0.5 transition-colors hover:bg-muted hover:text-foreground disabled:hover:bg-transparent"
                  disabled={loading || crumb.path === listing?.path}
                  onClick={() => void navigate(crumb.path)}
                >
                  {crumb.label}
                </button>
              </Fragment>
            ))}
          </div>
          <div className="flex items-center gap-0.5 rounded-md border border-border p-0.5">
            <Button
              size="icon-sm"
              variant={view === "list" ? "secondary" : "ghost"}
              aria-label={t("dirPicker.viewList")}
              title={t("dirPicker.viewList")}
              onClick={() => switchView("list")}
            >
              <LayoutListIcon className="size-3.5" />
            </Button>
            <Button
              size="icon-sm"
              variant={view === "columns" ? "secondary" : "ghost"}
              aria-label={t("dirPicker.viewColumns")}
              title={t("dirPicker.viewColumns")}
              onClick={() => switchView("columns")}
            >
              <Columns2Icon className="size-3.5" />
            </Button>
          </div>
          <Button
            size="icon-sm"
            variant="outline"
            aria-label={pinned ? t("dirPicker.unpin") : t("dirPicker.pin")}
            title={pinned ? t("dirPicker.unpin") : t("dirPicker.pin")}
            disabled={loading || !listing}
            onClick={togglePin}
          >
            {pinned ? (
              <PinOffIcon className="size-3.5" />
            ) : (
              <PinIcon className="size-3.5" />
            )}
          </Button>
          <Button
            size="icon-sm"
            variant="outline"
            aria-label={t("dirPicker.showHidden")}
            title={t("dirPicker.showHidden")}
            disabled={loading}
            onClick={() => {
              const next = !showHidden
              setShowHidden(next)
              void navigate(listing?.path, { hidden: next })
            }}
          >
            {showHidden ? (
              <EyeIcon className="size-3.5" />
            ) : (
              <EyeOffIcon className="size-3.5" />
            )}
          </Button>
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

        <div className="flex min-h-0 gap-2">
          <DirPlaces
            places={places}
            pins={pins}
            currentPath={listing?.path}
            onNavigate={(path) => void navigate(path)}
            onUnpin={(path) => {
              const next = pins.filter((p) => p.path !== path)
              setPins(next)
              localStorage.setItem(PINS_KEY, JSON.stringify(next))
            }}
          />

          {loading && !listing ? (
            <div className="flex h-80 flex-1 items-center justify-center rounded-lg border border-border">
              <Spinner className="size-5" />
            </div>
          ) : error ? (
            <Empty className="h-80 flex-1 justify-center rounded-lg border border-border">
              <EmptyHeader>
                <EmptyDescription>{error}</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : empty && view === "list" ? (
            <Empty className="h-80 flex-1 justify-center rounded-lg border border-border">
              <EmptyHeader>
                <EmptyDescription>
                  {cloningHere
                    ? t("dirPicker.cloningHere")
                    : t("dirPicker.empty")}
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : view === "columns" ? (
            <div className="h-80 min-w-0 flex-1 rounded-lg border border-border">
              {columns.length > 0 ? (
                <DirColumnsView
                  columns={columns}
                  sortKey={sortKey}
                  sortAsc={sortAsc}
                  onDrill={(path, colIndex) =>
                    void navigate(path, { at: colIndex })
                  }
                  onPickFile={pickFile}
                />
              ) : null}
            </div>
          ) : (
            <ScrollArea className="h-80 min-w-0 flex-1 rounded-lg border border-border">
              {listing ? (
                <DirListView
                  listing={listing}
                  sortKey={sortKey}
                  sortAsc={sortAsc}
                  locale={i18n.language}
                  onSort={(key) => {
                    if (key === sortKey) {
                      setSortAsc(!sortAsc)
                    } else {
                      setSortKey(key)
                      // 名称习惯升序，时间和大小先看最新/最大的。
                      setSortAsc(key === "name")
                    }
                  }}
                  onOpenDir={(path) =>
                    void navigate(path, { at: columns.length - 1 })
                  }
                  onPickFile={pickFile}
                />
              ) : null}
            </ScrollArea>
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
