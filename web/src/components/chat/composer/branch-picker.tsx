import { useState } from "react"
import { useTranslation } from "react-i18next"
import { useNavigate } from "react-router"
import { toast } from "sonner"

import { useWorkspace } from "@/components/workspace/workspace-context"
import type { GitBranchView } from "@/types/acp"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { Separator } from "@/components/ui/separator"
import { Spinner } from "@/components/ui/spinner"
import {
  CheckIcon,
  GitBranchIcon,
  LockIcon,
  PlusIcon,
  SplitIcon,
} from "lucide-react"

/**
 * 输入卡下沿的分支控件：显示当前分支，点开可切换分支、开隔离工作区。
 *
 * 数据只在打开时拉——分支状态每次打开都可能已经被 agent 改过，缓存它
 * 只会显示陈旧信息；关着的时候更没必要为它 exec git。
 */
export function BranchPicker({ fallback }: { fallback?: string }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { sessionId, scope } = useWorkspace()

  const [open, setOpen] = useState(false)
  const [view, setView] = useState<GitBranchView | null>(null)
  const [busy, setBusy] = useState(false)
  const [newBranch, setNewBranch] = useState("")
  const [worktreeName, setWorktreeName] = useState("")

  // 草稿态没有会话，也就没有仓库可读。
  if (!sessionId) {
    return fallback ? <BranchLabel branch={fallback} /> : null
  }

  async function load() {
    try {
      setView(await scope.gitBranches(sessionId))
    } catch (err) {
      toast.error((err as Error).message)
      setOpen(false)
    }
  }

  async function checkout(branch: string, create = false) {
    if (busy) return
    setBusy(true)
    try {
      setView(await scope.gitCheckout(sessionId, { branch, create }))
      setNewBranch("")
      toast.success(t("chat.branch.switched", { branch }))
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  async function addWorktree() {
    const name = worktreeName.trim()
    if (!name || busy) return
    setBusy(true)
    try {
      const { path } = await scope.worktreeCreate(sessionId, { name })
      setWorktreeName("")
      setOpen(false)
      // 开工作区的意图就是「去那儿干活」，所以直接把新会话页开在它上面。
      void navigate(`/sessions/new?cwd=${encodeURIComponent(path)}`)
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const current = view?.current ?? fallback

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (next) void load()
      }}
    >
      <PopoverTrigger
        render={
          <button
            type="button"
            className="flex shrink-0 items-center gap-1 rounded-md text-xs text-muted-foreground/80 transition-colors duration-150 hover:text-foreground"
          />
        }
      >
        <GitBranchIcon className="size-3" />
        <span className="font-mono">{current ?? t("chat.branch.none")}</span>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-72 p-0">
        {view === null ? (
          <div className="flex items-center justify-center py-6">
            <Spinner className="size-4 text-muted-foreground" />
          </div>
        ) : !view.isRepo ? (
          <p className="px-3 py-4 text-xs text-muted-foreground">
            {t("chat.branch.notRepo")}
          </p>
        ) : (
          <>
            {view.dirty ? (
              <p className="border-b border-border px-3 py-2 text-xs text-warning">
                {t("chat.branch.dirty")}
              </p>
            ) : null}

            <ScrollArea className="max-h-56 py-1">
              {view.local.map((branch) => {
                const taken = Boolean(branch.worktree)
                return (
                  <button
                    key={branch.name}
                    type="button"
                    disabled={busy || branch.current || taken || view.dirty}
                    title={branch.worktree}
                    className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm transition-colors duration-150 hover:bg-accent disabled:pointer-events-none disabled:opacity-50"
                    onClick={() => void checkout(branch.name)}
                  >
                    {branch.current ? (
                      <CheckIcon className="size-3.5 shrink-0" />
                    ) : taken ? (
                      <LockIcon className="size-3.5 shrink-0" />
                    ) : (
                      <span className="size-3.5 shrink-0" />
                    )}
                    <span className="truncate font-mono text-xs">
                      {branch.name}
                    </span>
                  </button>
                )
              })}
            </ScrollArea>

            <Separator />
            <div className="flex items-center gap-1.5 p-2">
              <Input
                value={newBranch}
                disabled={view.dirty}
                placeholder={t("chat.branch.newPlaceholder")}
                className="h-7 font-mono text-xs"
                onChange={(e) => setNewBranch(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && newBranch.trim()) {
                    void checkout(newBranch.trim(), true)
                  }
                }}
              />
              <Button
                size="icon-sm"
                variant="ghost"
                aria-label={t("chat.branch.create")}
                disabled={!newBranch.trim() || busy || view.dirty}
                onClick={() => void checkout(newBranch.trim(), true)}
              >
                <PlusIcon />
              </Button>
            </div>

            <Separator />
            <div className="p-2">
              <p className="px-1 pb-1 text-[11px] text-muted-foreground">
                {t("chat.branch.worktrees")}
              </p>
              {view.worktrees
                .filter((worktree) => !worktree.main)
                .map((worktree) => (
                  <div
                    key={worktree.path}
                    className="flex items-center gap-2 px-1 py-1 text-xs text-muted-foreground"
                    title={worktree.path}
                  >
                    <SplitIcon className="size-3.5 shrink-0" />
                    <span className="truncate font-mono">
                      {worktree.branch ?? worktree.path}
                    </span>
                    {worktree.current ? (
                      <CheckIcon className="ml-auto size-3.5 shrink-0" />
                    ) : null}
                  </div>
                ))}
              <div className="flex items-center gap-1.5 pt-1">
                <Input
                  value={worktreeName}
                  placeholder={t("chat.branch.worktreePlaceholder")}
                  className="h-7 font-mono text-xs"
                  onChange={(e) => setWorktreeName(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") void addWorktree()
                  }}
                />
                <Button
                  size="icon-sm"
                  variant="ghost"
                  aria-label={t("chat.branch.addWorktree")}
                  disabled={!worktreeName.trim() || busy}
                  onClick={() => void addWorktree()}
                >
                  <PlusIcon />
                </Button>
              </div>
            </div>
          </>
        )}
      </PopoverContent>
    </Popover>
  )
}

function BranchLabel({ branch }: { branch: string }) {
  return (
    <span className="flex shrink-0 items-center gap-1 text-xs text-muted-foreground/80">
      <GitBranchIcon className="size-3" />
      <span className="font-mono">{branch}</span>
    </span>
  )
}
