import { memo, useCallback, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"

import { GitPanelHeader } from "@/components/workspace/panels/git-parts"
import { PanelEmptyState } from "@/components/workspace/panels/panel-empty-state"
import {
  useGitOverview,
  useGitSelection,
  useWorkspace,
} from "@/components/workspace/workspace-context"
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@/components/ui/context-menu"
import { Spinner } from "@/components/ui/spinner"
import { formatRelativeTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { GitCommit, GitOverview } from "@/types/acp"
import { GitCommitHorizontalIcon, PencilLineIcon } from "lucide-react"

const PAGE_SIZE = 50

/** 未推送提交的 sha 集合；overview 未加载时是空集。 */
function unpushedShas(overview: GitOverview | null): Set<string> {
  return new Set((overview?.commits ?? []).map((commit) => commit.sha))
}

/**
 * 提交链路面板（vscode / GoLand 的中栏）。取代了原来的「未推送提交」面板：
 * 未推送只是历史的一个子集，为它单开一个面板等于把同一条时间线切成两半。
 * 未推送的那几条在这里带标记，不必换面板看。
 *
 * 顶部固定一条「工作区改动」——它是时间线上「还没提交的那一段」，
 * 点它变更面板就回到未提交状态，与选中某条提交是同一种操作。
 */
export const HistoryPanel = memo(function HistoryPanel() {
  const { t, i18n } = useTranslation()
  const ws = useWorkspace()
  const selection = useGitSelection()
  const git = useGitOverview()

  const tokenRef = useRef(0)
  const [commits, setCommits] = useState<GitCommit[] | null>(null)
  const [hasMore, setHasMore] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // 选中一条 ref 就看它的历史；选了两条是对比模式，链路显示 head 的历史。
  const ref = selection.refs.at(-1) ?? ""

  // 加载态刻意由数据推断（commits === null = 首屏加载中）：effect 里同步
  // setState 会触发额外一轮渲染，spinner 只留给显式刷新与翻页。
  const load = useCallback(
    (offset = 0) => {
      if (!ws.sessionId) return
      // 换 ref 后旧请求的结果必须丢掉，否则「加载更早」会把另一条分支的
      // 提交接在当前链路后面。
      const token = ++tokenRef.current
      ws.scope
        .gitHistory(ws.sessionId, { ref, limit: PAGE_SIZE, offset })
        .then((page) => {
          if (tokenRef.current !== token) return
          setCommits((prev) =>
            offset === 0 ? page.commits : [...(prev ?? []), ...page.commits]
          )
          setHasMore(page.hasMore)
          setError(null)
        })
        .catch((err: Error) => {
          if (tokenRef.current === token) setError(err.message)
        })
        .finally(() => {
          if (tokenRef.current === token) setLoading(false)
        })
    },
    [ws, ref]
  )

  useEffect(() => {
    load(0)
    return ws.onWorkspaceRefresh(() => load(0))
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

  // 未推送集合跟着 git 汇总走，不随链路的每次重渲染重建。
  const unpushed = unpushedShas(git.data)
  const dirtyCount = git.data?.files.length ?? 0

  return (
    <div className="flex h-full flex-col">
      <GitPanelHeader
        title={ref || git.data?.branch || t("chat.branch.none")}
        hint={
          commits
            ? t("workspace.git.commitCount", { count: commits.length })
            : undefined
        }
        loading={loading}
        onRefresh={() => {
          setLoading(true)
          load(0)
        }}
      />

      <div className="min-h-0 flex-1 overflow-y-auto">
        {/* 时间线的第一段：尚未提交的改动。 */}
        <button
          type="button"
          className={cn(
            "flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-xs transition-colors duration-150 hover:bg-accent",
            selection.sha === null && "bg-accent"
          )}
          onClick={() => ws.selectCommit(null)}
        >
          <PencilLineIcon className="size-3.5 shrink-0 text-warning" />
          <span className="truncate">{t("workspace.git.workingTree")}</span>
          {dirtyCount > 0 ? (
            <span className="ml-auto shrink-0 text-muted-foreground tabular-nums">
              {dirtyCount}
            </span>
          ) : null}
        </button>

        {commits === null ? (
          <div className="flex items-center justify-center py-6">
            <Spinner className="size-4 text-muted-foreground" />
          </div>
        ) : commits.length === 0 ? (
          <PanelEmptyState title={t("workspace.git.noCommits")} />
        ) : (
          commits.map((commit) => (
            <CommitRow
              key={commit.sha}
              commit={commit}
              selected={selection.sha === commit.sha}
              unpushed={unpushed.has(commit.sha)}
              time={formatRelativeTime(
                new Date(commit.time * 1000).toISOString(),
                i18n.language
              )}
              onSelect={() => ws.selectCommit(commit.sha)}
              onAsk={() =>
                ws.askAI(
                  t("workspace.git.promptCommit", {
                    sha: commit.short,
                    subject: commit.subject,
                  })
                )
              }
            />
          ))
        )}

        {hasMore ? (
          <button
            type="button"
            disabled={loading}
            className="w-full py-2 text-xs text-muted-foreground transition-colors duration-150 hover:text-foreground disabled:opacity-50"
            onClick={() => {
              setLoading(true)
              load(commits?.length ?? 0)
            }}
          >
            {t("workspace.git.loadMore")}
          </button>
        ) : null}
      </div>
    </div>
  )
})

function CommitRow({
  commit,
  selected,
  unpushed,
  time,
  onSelect,
  onAsk,
}: {
  commit: GitCommit
  selected: boolean
  unpushed: boolean
  time: string
  onSelect: () => void
  onAsk: () => void
}) {
  const { t } = useTranslation()
  return (
    <ContextMenu>
      <ContextMenuTrigger
        render={
          <button
            type="button"
            title={commit.subject}
            className={cn(
              "flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-xs transition-colors duration-150 hover:bg-accent",
              selected && "bg-accent"
            )}
            onClick={onSelect}
          />
        }
      >
        <GitCommitHorizontalIcon
          className={cn(
            "size-3.5 shrink-0",
            unpushed ? "text-primary" : "text-muted-foreground/60"
          )}
        />
        <span className="min-w-0 flex-1 truncate">{commit.subject}</span>
        <span className="shrink-0 text-muted-foreground/70">
          {commit.author}
        </span>
        <span className="shrink-0 text-muted-foreground/70 tabular-nums">
          {time}
        </span>
        <span className="shrink-0 font-mono text-muted-foreground/50">
          {commit.short}
        </span>
      </ContextMenuTrigger>
      <ContextMenuContent className="w-48">
        <ContextMenuItem onClick={onAsk}>
          {t("workspace.git.askReview")}
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  )
}
