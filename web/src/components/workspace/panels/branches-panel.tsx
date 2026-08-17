import { memo, useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { PanelEmptyState } from "@/components/workspace/panels/panel-empty-state"
import { GitPanelHeader } from "@/components/workspace/panels/git-parts"
import {
  useGitSelection,
  useWorkspace,
} from "@/components/workspace/workspace-context"
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu"
import { Spinner } from "@/components/ui/spinner"
import { cn } from "@/lib/utils"
import type { GitBranchView } from "@/types/acp"
import {
  ChevronDownIcon,
  ChevronRightIcon,
  GitBranchIcon,
  LockIcon,
  TagIcon,
} from "lucide-react"

/**
 * 分支面板（vscode / GoLand 的 Git 工具窗左栏）：本地、远程、标签三组。
 *
 * 它是 git 面板群的**选择驱动方**——点一条 ref 让链路面板过滤到它，
 * 按住 ⌘/Ctrl 点第二条进入对比模式，变更与详情面板随之显示对比结果。
 * 面板之间不互相调用，全部经命令总线的选择态（workspace-context）。
 */
export const BranchesPanel = memo(function BranchesPanel() {
  const { t } = useTranslation()
  const ws = useWorkspace()
  const selection = useGitSelection()

  const [view, setView] = useState<GitBranchView | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({
    tags: true,
  })

  const load = useCallback(() => {
    if (!ws.sessionId) return
    ws.scope
      .gitBranches(ws.sessionId)
      .then((data) => {
        setView(data)
        setError(null)
      })
      .catch((err: Error) => setError(err.message))
  }, [ws])

  useEffect(() => {
    load()
    // agent 改完东西（turn 结束）分支可能已经不是原来那条了。
    return ws.onWorkspaceRefresh(load)
  }, [load, ws])

  if (!ws.sessionId) {
    return (
      <PanelEmptyState
        title={t("workspace.tree.draftTitle")}
        description={t("workspace.tree.draftHint")}
      />
    )
  }
  if (error) {
    return (
      <PanelEmptyState title={t("common.loadFailed")} description={error} />
    )
  }
  if (!view) {
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner className="size-4 text-muted-foreground" />
      </div>
    )
  }
  if (!view.isRepo) {
    return <PanelEmptyState title={t("workspace.git.notRepo")} />
  }

  const toggle = (key: string) =>
    setCollapsed((prev) => ({ ...prev, [key]: !prev[key] }))

  /** 单击 = 只选它；⌘/Ctrl 单击 = 加入选择（凑够两个即对比）。 */
  const pick = (ref: string, additive: boolean) => {
    if (!additive) {
      ws.selectRefs(
        selection.refs.length === 1 && selection.refs[0] === ref ? [] : [ref]
      )
      return
    }
    ws.selectRefs(
      selection.refs.includes(ref)
        ? selection.refs.filter((item) => item !== ref)
        : [...selection.refs, ref]
    )
  }

  const checkout = async (ref: string) => {
    try {
      setView(await ws.scope.gitCheckout(ws.sessionId, { branch: ref }))
      ws.refreshWorkspace()
      toast.success(t("chat.branch.switched", { branch: ref }))
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  /**
   * 右键「让 AI 分析」：已经选了另一条 ref 就写成对比请求，否则问这条
   * 分支本身。prompt 只填进输入框，发不发由用户决定。
   */
  const askAbout = (target: string) => {
    const other = selection.refs.find((ref) => ref !== target)
    ws.askAI(
      other
        ? t("workspace.git.promptCompare", { base: other, head: target })
        : t("workspace.git.promptBranch", { ref: target })
    )
  }

  const compareHint =
    selection.refs.length === 2
      ? t("workspace.git.comparing", {
          base: selection.refs[0],
          head: selection.refs[1],
        })
      : t("workspace.git.pickTwoHint")

  return (
    <div className="flex h-full flex-col">
      <GitPanelHeader
        title={view.current ?? t("chat.branch.none")}
        hint={compareHint}
        onRefresh={load}
      />
      <div className="min-h-0 flex-1 overflow-y-auto py-1 text-sm">
        <Group
          label={t("workspace.git.local")}
          collapsed={collapsed.local}
          onToggle={() => toggle("local")}
        >
          {view.local.map((branch) => (
            <RefRow
              key={branch.name}
              name={branch.name}
              icon={
                branch.worktree ? (
                  <LockIcon className="size-3.5 shrink-0 text-muted-foreground" />
                ) : (
                  <GitBranchIcon className="size-3.5 shrink-0 text-muted-foreground" />
                )
              }
              title={branch.worktree}
              current={branch.current}
              selected={selection.refs.includes(branch.name)}
              onPick={(additive) => pick(branch.name, additive)}
              onCheckout={
                branch.current || branch.worktree || view.dirty
                  ? undefined
                  : () => void checkout(branch.name)
              }
              onAsk={() => askAbout(branch.name)}
            />
          ))}
        </Group>

        <Group
          label={t("workspace.git.remote")}
          collapsed={collapsed.remote}
          onToggle={() => toggle("remote")}
        >
          {view.remote.map((name) => (
            <RefRow
              key={name}
              name={name}
              icon={
                <GitBranchIcon className="size-3.5 shrink-0 text-muted-foreground/70" />
              }
              selected={selection.refs.includes(name)}
              onPick={(additive) => pick(name, additive)}
              onAsk={() => askAbout(name)}
            />
          ))}
        </Group>

        {view.tags.length > 0 ? (
          <Group
            label={t("workspace.git.tags")}
            collapsed={collapsed.tags}
            onToggle={() => toggle("tags")}
          >
            {view.tags.map((name) => (
              <RefRow
                key={name}
                name={name}
                icon={
                  <TagIcon className="size-3.5 shrink-0 text-muted-foreground/70" />
                }
                selected={selection.refs.includes(name)}
                onPick={(additive) => pick(name, additive)}
                onAsk={() => askAbout(name)}
              />
            ))}
          </Group>
        ) : null}
      </div>
    </div>
  )
})

function Group({
  label,
  collapsed,
  onToggle,
  children,
}: {
  label: string
  collapsed?: boolean
  onToggle: () => void
  children: React.ReactNode
}) {
  return (
    <div>
      <button
        type="button"
        className="flex w-full items-center gap-1 px-2 py-1 text-xs text-muted-foreground transition-colors duration-150 hover:text-foreground"
        onClick={onToggle}
      >
        {collapsed ? (
          <ChevronRightIcon className="size-3.5" />
        ) : (
          <ChevronDownIcon className="size-3.5" />
        )}
        {label}
      </button>
      {collapsed ? null : <div className="pb-1">{children}</div>}
    </div>
  )
}

function RefRow({
  name,
  icon,
  title,
  current,
  selected,
  onPick,
  onCheckout,
  onAsk,
}: {
  name: string
  icon: React.ReactNode
  title?: string
  current?: boolean
  selected: boolean
  onPick: (additive: boolean) => void
  onCheckout?: () => void
  onAsk: () => void
}) {
  const { t } = useTranslation()
  return (
    <ContextMenu>
      <ContextMenuTrigger
        render={
          <button
            type="button"
            title={title ?? name}
            className={cn(
              "flex w-full items-center gap-1.5 px-2 py-1 pl-6 text-left transition-colors duration-150 hover:bg-accent",
              selected && "bg-accent",
              current && "font-medium"
            )}
            onClick={(e) => onPick(e.metaKey || e.ctrlKey)}
          />
        }
      >
        {icon}
        <span className="truncate font-mono text-xs">{name}</span>
      </ContextMenuTrigger>
      <ContextMenuContent className="w-52">
        <ContextMenuItem onClick={onAsk}>
          {t("workspace.git.askCompare")}
        </ContextMenuItem>
        {onCheckout ? (
          <>
            <ContextMenuSeparator />
            <ContextMenuItem onClick={onCheckout}>
              {t("workspace.git.checkout")}
            </ContextMenuItem>
          </>
        ) : null}
      </ContextMenuContent>
    </ContextMenu>
  )
}
