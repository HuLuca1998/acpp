import { memo, useCallback, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { PanelEmptyState } from "@/components/workspace/panels/panel-empty-state"
import { GitPanelHeader } from "@/components/workspace/panels/git-parts"
import {
  GitConfirmDialog,
  GitPromptDialog,
  type GitConfirm,
  type GitPrompt,
} from "@/components/workspace/panels/git-dialogs"
import { copyText } from "@/lib/clipboard"
import {
  useGitSelection,
  useWorkspace,
} from "@/components/workspace/workspace-context"
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu"
import { Spinner } from "@/components/ui/spinner"
import { cn } from "@/lib/utils"
import type { GitBranchView, GitOpResult } from "@/types/acp"
import {
  ChevronDownIcon,
  ChevronRightIcon,
  GitBranchIcon,
  LockIcon,
  TagIcon,
} from "lucide-react"

type RefKind = "local" | "remote" | "tag"

/** 在分支清单里挑主分支：main 优先，其次 master，都没有就没有。 */
function mainBranchOf(names: string[]): string | null {
  return (
    names.find((n) => n === "main") ?? names.find((n) => n === "master") ?? null
  )
}

/**
 * 分支面板（vscode / GoLand 的 Git 工具窗左栏）：本地、远程、标签三组。
 *
 * 它是 git 面板群的**选择驱动方**——点一条 ref 让链路面板过滤到它，
 * ⌘/Ctrl 点第二条进入对比模式。右键菜单按**当前状态**出项：只选了一条
 * 时不会冒出「对比这两条分支」，当前分支不会出现「迁出」，脏工作区不会
 * 给你一个点了必然失败的切换。
 */
export const BranchesPanel = memo(function BranchesPanel() {
  const { t } = useTranslation()
  const ws = useWorkspace()
  const selection = useGitSelection()

  const tokenRef = useRef(0)
  const [view, setView] = useState<GitBranchView | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({
    tags: true,
  })
  const [prompt, setPrompt] = useState<GitPrompt | null>(null)
  const [confirm, setConfirm] = useState<GitConfirm | null>(null)

  const load = useCallback(() => {
    if (!ws.ready) return
    // 刷新与写操作会并发：晚发出的请求才是当前状态。
    const token = ++tokenRef.current
    ws.scope
      .gitBranches(ws.sessionId)
      .then((data) => {
        if (tokenRef.current !== token) return
        setView(data)
        setError(null)
      })
      .catch((err: Error) => {
        if (tokenRef.current === token) setError(err.message)
      })
  }, [ws])

  useEffect(() => {
    load()
    // agent 改完东西（turn 结束）分支可能已经不是原来那条了。
    return ws.onWorkspaceRefresh(load)
  }, [load, ws])

  /** 写操作走同一条路：跑命令 → 换视图 → 刷工作区 → 报结果。 */
  const run = useCallback(
    async (label: string, op: () => Promise<GitOpResult>) => {
      try {
        const result = await op()
        tokenRef.current++
        if (result.branch) setView(result.branch)
        ws.refreshWorkspace()
        toast.success(result.output?.trim() || label)
      } catch (err) {
        // git 的原话就是下一步该做什么的说明（推送被拒、合并冲突……）。
        toast.error((err as Error).message)
      }
    },
    [ws]
  )

  if (!ws.ready) {
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

  const branchView = view
  const sessionId = ws.sessionId
  const mainBranch = mainBranchOf(branchView.local.map((b) => b.name))
  const currentBranch = branchView.detached
    ? null
    : (branchView.current ?? null)

  const toggle = (key: string) =>
    setCollapsed((prev) => ({ ...prev, [key]: !prev[key] }))

  /** 单击 = 只选它（再点取消）；⌘/Ctrl 单击 = 加入选择，凑够两条即对比。 */
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

  const actions: RefActions = {
    askCompare: (base, head) =>
      ws.askAI(t("workspace.git.promptCompare", { base, head })),
    askBranch: (ref) => ws.askAI(t("workspace.git.promptBranch", { ref })),
    checkout: (ref) =>
      void run(t("chat.branch.switched", { branch: ref }), () =>
        ws.scope
          .gitCheckout(sessionId, { branch: ref })
          .then((branch) => ({ branch }))
      ),
    newBranch: (from, remote) =>
      setPrompt({
        title: remote
          ? t("workspace.git.checkoutRemote")
          : t("workspace.git.newBranchFrom", { ref: from }),
        description: t("workspace.git.newBranchDesc"),
        placeholder: "feat/my-change",
        defaultValue: remote ? from.replace(/^[^/]+\//, "") : "",
        confirmLabel: t("workspace.git.createAndSwitch"),
        onConfirm: (name) =>
          void run(t("workspace.git.branchCreated", { name }), () =>
            ws.scope.gitCreateBranch(sessionId, { name, from, checkout: true })
          ),
      }),
    merge: (ref) =>
      void run(t("workspace.git.merged", { ref }), () =>
        ws.scope.gitMerge(sessionId, ref)
      ),
    push: () =>
      void run(t("workspace.git.pushed"), () => ws.scope.gitPush(sessionId)),
    pull: () =>
      void run(t("workspace.git.pulled"), () => ws.scope.gitPull(sessionId)),
    remove: (ref) =>
      setConfirm({
        title: t("workspace.git.deleteBranchShort"),
        description: t("workspace.git.deleteBranchDesc", { ref }),
        confirmLabel: t("common.delete"),
        onConfirm: () =>
          void run(t("workspace.git.branchDeleted", { ref }), () =>
            ws.scope.gitDeleteBranch(sessionId, ref)
          ),
      }),
    copy: (value) => void copyText(value),
  }

  const compareHint =
    selection.refs.length === 2
      ? t("workspace.git.comparing", {
          base: selection.refs[0],
          head: selection.refs[1],
        })
      : t("workspace.git.pickTwoHint")

  const rowContext = (name: string): RowContext => ({
    // 只有「选中两条且本行是其中之一」才谈得上对比这两条——菜单项跟着
    // 状态走，而不是碰巧选了别的就换句话说。
    selectedPair:
      selection.refs.length === 2 && selection.refs.includes(name)
        ? (selection.refs.find((ref) => ref !== name) ?? null)
        : null,
    currentBranch,
    mainBranch,
    dirty: branchView.dirty,
  })

  return (
    <div className="flex h-full flex-col">
      <GitPanelHeader
        title={branchView.current ?? t("chat.branch.none")}
        hint={compareHint}
        onRefresh={load}
      />
      <div className="min-h-0 flex-1 overflow-y-auto py-1 text-sm">
        <Group
          label={t("workspace.git.local")}
          collapsed={collapsed.local}
          onToggle={() => toggle("local")}
        >
          {branchView.local.map((branch) => (
            <RefRow
              key={branch.name}
              name={branch.name}
              kind="local"
              icon={
                branch.worktree ? (
                  <LockIcon className="size-3.5 shrink-0 text-muted-foreground" />
                ) : (
                  <GitBranchIcon className="size-3.5 shrink-0 text-muted-foreground" />
                )
              }
              title={branch.worktree}
              current={branch.current}
              takenByWorktree={Boolean(branch.worktree)}
              selected={selection.refs.includes(branch.name)}
              context={rowContext(branch.name)}
              actions={actions}
              onPick={(additive) => pick(branch.name, additive)}
            />
          ))}
        </Group>

        <Group
          label={t("workspace.git.remote")}
          collapsed={collapsed.remote}
          onToggle={() => toggle("remote")}
        >
          {branchView.remote.map((name) => (
            <RefRow
              key={name}
              name={name}
              kind="remote"
              icon={
                <GitBranchIcon className="size-3.5 shrink-0 text-muted-foreground/70" />
              }
              selected={selection.refs.includes(name)}
              context={rowContext(name)}
              actions={actions}
              onPick={(additive) => pick(name, additive)}
            />
          ))}
        </Group>

        {branchView.tags.length > 0 ? (
          <Group
            label={t("workspace.git.tags")}
            collapsed={collapsed.tags}
            onToggle={() => toggle("tags")}
          >
            {branchView.tags.map((name) => (
              <RefRow
                key={name}
                name={name}
                kind="tag"
                icon={
                  <TagIcon className="size-3.5 shrink-0 text-muted-foreground/70" />
                }
                selected={selection.refs.includes(name)}
                context={rowContext(name)}
                actions={actions}
                onPick={(additive) => pick(name, additive)}
              />
            ))}
          </Group>
        ) : null}
      </div>

      {/* key 让每个操作拿到自己的输入框实例：换操作即重挂，输入不残留。 */}
      <GitPromptDialog
        key={prompt?.title ?? "closed"}
        prompt={prompt}
        onClose={() => setPrompt(null)}
      />
      <GitConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </div>
  )
})

/** 一行 ref 能做的事；实现全在面板里，行只按状态挑该显示的那几条。 */
interface RefActions {
  askCompare: (base: string, head: string) => void
  askBranch: (ref: string) => void
  checkout: (ref: string) => void
  newBranch: (from: string, remote?: boolean) => void
  merge: (ref: string) => void
  push: () => void
  pull: () => void
  remove: (ref: string) => void
  copy: (value: string) => void
}

/** 决定这一行菜单长什么样的全部状态。 */
interface RowContext {
  /** 选中两条且本行在其中时，另一条是谁；否则 null。 */
  selectedPair: string | null
  currentBranch: string | null
  mainBranch: string | null
  dirty: boolean
}

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
  kind,
  icon,
  title,
  current,
  takenByWorktree,
  selected,
  context,
  actions,
  onPick,
}: {
  name: string
  kind: RefKind
  icon: React.ReactNode
  title?: string
  current?: boolean
  takenByWorktree?: boolean
  selected: boolean
  context: RowContext
  actions: RefActions
  onPick: (additive: boolean) => void
}) {
  const { t } = useTranslation()
  const { selectedPair, currentBranch, mainBranch, dirty } = context

  // 迁出的前提：不是当前分支、没被别的 worktree 占着、工作区干净
  //（git 会把未提交改动带过去，那是惊吓不是便利）。
  const canCheckout = kind === "local" && !current && !takenByWorktree && !dirty
  const canMerge = kind !== "tag" && !current && currentBranch !== null
  const compareWithCurrent =
    currentBranch !== null && name !== currentBranch ? currentBranch : null
  const compareWithMain =
    mainBranch !== null && name !== mainBranch && mainBranch !== currentBranch
      ? mainBranch
      : null

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

      <ContextMenuContent className="w-60">
        <ContextMenuLabel className="truncate font-mono text-xs">
          {name}
        </ContextMenuLabel>
        <ContextMenuSeparator />

        {selectedPair ? (
          <ContextMenuItem
            onClick={() => actions.askCompare(selectedPair, name)}
          >
            {t("workspace.git.askComparePair", { base: selectedPair })}
          </ContextMenuItem>
        ) : null}
        {!selectedPair && compareWithCurrent ? (
          <ContextMenuItem
            onClick={() => actions.askCompare(compareWithCurrent, name)}
          >
            {t("workspace.git.askCompareCurrent", {
              current: compareWithCurrent,
            })}
          </ContextMenuItem>
        ) : null}
        {!selectedPair && compareWithMain ? (
          <ContextMenuItem
            onClick={() => actions.askCompare(compareWithMain, name)}
          >
            {t("workspace.git.askCompareMain", { main: compareWithMain })}
          </ContextMenuItem>
        ) : null}
        <ContextMenuItem onClick={() => actions.askBranch(name)}>
          {t("workspace.git.askBranch")}
        </ContextMenuItem>

        <ContextMenuSeparator />

        {canCheckout ? (
          <ContextMenuItem onClick={() => actions.checkout(name)}>
            {t("workspace.git.checkout")}
          </ContextMenuItem>
        ) : null}
        <ContextMenuItem
          onClick={() => actions.newBranch(name, kind === "remote")}
        >
          {kind === "remote"
            ? t("workspace.git.checkoutRemote")
            : t("workspace.git.newBranch")}
        </ContextMenuItem>
        {canMerge ? (
          <ContextMenuItem onClick={() => actions.merge(name)}>
            {t("workspace.git.mergeInto", { current: currentBranch })}
          </ContextMenuItem>
        ) : null}
        {current ? (
          <>
            <ContextMenuItem onClick={actions.pull}>
              {t("workspace.git.pull")}
            </ContextMenuItem>
            <ContextMenuItem onClick={actions.push}>
              {t("workspace.git.push")}
            </ContextMenuItem>
          </>
        ) : null}

        <ContextMenuSeparator />
        <ContextMenuItem onClick={() => actions.copy(name)}>
          {t("workspace.git.copyName")}
        </ContextMenuItem>
        {kind === "local" && !current ? (
          <ContextMenuItem
            variant="destructive"
            onClick={() => actions.remove(name)}
          >
            {t("workspace.git.deleteBranchShort")}
          </ContextMenuItem>
        ) : null}
      </ContextMenuContent>
    </ContextMenu>
  )
}
