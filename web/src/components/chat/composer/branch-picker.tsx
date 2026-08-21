import { useState } from "react"
import { useTranslation } from "react-i18next"
import { useNavigate } from "react-router"
import { toast } from "sonner"

import {
  useGitOverview,
  useWorkspace,
} from "@/components/workspace/workspace-context"
import type { GitBranchView } from "@/types/acp"
import { Hint } from "@/components/hint"
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
 * 分支名读工作区共享的 git 汇总（gitStore）——那份数据在轮末与 agent 每
 * 干完一件事之后都会刷新，agent 自己 `git checkout` 换了分支，这里跟着变。
 * 会话记录里的 gitBranch 只当兜底：它是打开会话那一刻的快照，agent 改完
 * 分支之后它就在说旧话了。
 *
 * 分支**清单**仍只在打开面板时拉：那是一次额外的 exec git，关着的时候
 * 没必要为它付钱。
 */
export function BranchPicker({ fallback }: { fallback?: string }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const ws = useWorkspace()
  const { sessionId, scope } = ws
  const git = useGitOverview()

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
      // 换了分支就是换了整个工作区：文件树、变更清单、提交链路全得跟着走。
      ws.refreshWorkspace()
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

  // 打开过面板就以那一次的结果为准（用户可能刚在里面切过分支，gitStore
  // 的刷新还在路上），否则读共享汇总，最后才回落到会话快照。
  const current = view?.current ?? git.data?.branch ?? fallback

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (next) {
          void load()
        } else {
          // 关上就把这一次的快照丢掉，显示交回 gitStore——不然 agent 之后
          // 自己切了分支，胶囊还固执地显示打开面板那会儿看到的名字。
          setView(null)
        }
      }}
    >
      <Hint
        label={t("chat.branch.switch")}
        desc={t("chat.branch.switchDesc")}
        align="start"
      >
        <PopoverTrigger
          render={
            <button
              type="button"
              aria-label={t("chat.branch.switch")}
              className="flex shrink-0 items-center gap-1 rounded-md text-xs text-muted-foreground/80 transition-colors duration-150 hover:text-foreground"
            >
              <GitBranchIcon className="size-3" />
              <span className="font-mono">
                {current ?? t("chat.branch.none")}
              </span>
            </button>
          }
        />
      </Hint>
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
              <Hint label={t("chat.branch.create")} align="end">
                <Button
                  size="icon-sm"
                  variant="ghost"
                  aria-label={t("chat.branch.create")}
                  disabled={!newBranch.trim() || busy || view.dirty}
                  onClick={() => void checkout(newBranch.trim(), true)}
                >
                  <PlusIcon />
                </Button>
              </Hint>
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
                <Hint
                  label={t("chat.branch.addWorktree")}
                  desc={t("chat.branch.addWorktreeDesc")}
                  align="end"
                >
                  <Button
                    size="icon-sm"
                    variant="ghost"
                    aria-label={t("chat.branch.addWorktree")}
                    disabled={!worktreeName.trim() || busy}
                    onClick={() => void addWorktree()}
                  >
                    <PlusIcon />
                  </Button>
                </Hint>
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
