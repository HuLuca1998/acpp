import { memo, useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"

import { PanelEmptyState } from "@/components/workspace/panels/panel-empty-state"
import {
  useGitOverview,
  useGitSelection,
  useWorkspace,
} from "@/components/workspace/workspace-context"
import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import { formatDateTime } from "@/lib/format"
import type { GitCommit, GitCompare } from "@/types/acp"
import { SparklesIcon } from "lucide-react"

/**
 * 详情面板（右下）：选中提交的完整信息，或两个 ref 的对比摘要。
 *
 * 与变更面板刻意分开而不是上下拼在一个面板里——提交说明是要读的文字，
 * 文件清单是要点的列表，两者的滚动节奏完全不同（GoLand 也是分开的）。
 */
export const CommitDetailPanel = memo(function CommitDetailPanel() {
  const { t, i18n } = useTranslation()
  const ws = useWorkspace()
  const selection = useGitSelection()
  const git = useGitOverview()

  const [commit, setCommit] = useState<GitCommit | null>(null)
  const [compare, setCompare] = useState<GitCompare | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const comparing = selection.refs.length === 2
  const [base, head] = selection.refs

  const load = useCallback(() => {
    if (!ws.sessionId) return

    if (comparing) {
      ws.scope
        .gitCompare(ws.sessionId, base, head)
        .then((data) => {
          setCompare(data)
          setCommit(null)
          setError(null)
        })
        .catch((err: Error) => setError(err.message))
        .finally(() => setLoading(false))
      return
    }

    if (!selection.sha) {
      return
    }
    ws.scope
      .gitCommit(ws.sessionId, selection.sha)
      .then((detail) => {
        setCommit(detail.commit)
        setCompare(null)
        setError(null)
      })
      .catch((err: Error) => setError(err.message))
      .finally(() => setLoading(false))
  }, [ws, comparing, base, head, selection.sha])

  useEffect(() => {
    load()
  }, [load])

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
  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner className="size-4 text-muted-foreground" />
      </div>
    )
  }

  if (compare) {
    return (
      <div className="flex h-full flex-col gap-3 overflow-y-auto p-3 text-xs">
        <div>
          <p className="font-mono text-sm">
            {compare.base} → {compare.head}
          </p>
          <p className="pt-1 text-muted-foreground tabular-nums">
            {t("workspace.git.aheadBehind", {
              ahead: compare.ahead,
              behind: compare.behind,
            })}
            {" · "}
            {t("workspace.git.fileCount", { count: compare.files.length })}
          </p>
        </div>
        <Button
          size="sm"
          variant="outline"
          className="self-start"
          onClick={() =>
            ws.askAI(
              t("workspace.git.promptCompare", {
                base: compare.base,
                head: compare.head,
              })
            )
          }
        >
          <SparklesIcon data-icon="inline-start" />
          {t("workspace.git.askCompare")}
        </Button>
        <ul className="flex flex-col gap-1 text-muted-foreground">
          {compare.commits.slice(0, 30).map((item) => (
            <li key={item.sha} className="truncate" title={item.subject}>
              <span className="font-mono text-muted-foreground/60">
                {item.short}
              </span>{" "}
              {item.subject}
            </li>
          ))}
        </ul>
      </div>
    )
  }

  if (!commit) {
    // 没选提交 = 看工作区：给出当前分支的一句话状态，别留一块空白。
    const overview = git.data
    return (
      <PanelEmptyState
        title={t("workspace.git.workingTree")}
        description={
          overview
            ? t("workspace.git.workingTreeDesc", {
                branch: overview.branch ?? "",
                files: overview.files.length,
              })
            : undefined
        }
      />
    )
  }

  return (
    <div className="flex h-full flex-col gap-3 overflow-y-auto p-3">
      <p className="text-sm leading-relaxed font-medium">{commit.subject}</p>
      <div className="flex flex-col gap-0.5 text-xs text-muted-foreground">
        <span className="font-mono">{commit.sha}</span>
        <span>
          {commit.author}
          {" · "}
          <span className="tabular-nums">
            {formatDateTime(
              new Date(commit.time * 1000).toISOString(),
              i18n.language
            )}
          </span>
        </span>
      </div>
      <Button
        size="sm"
        variant="outline"
        className="self-start"
        onClick={() =>
          ws.askAI(
            t("workspace.git.promptCommit", {
              sha: commit.short,
              subject: commit.subject,
            })
          )
        }
      >
        <SparklesIcon data-icon="inline-start" />
        {t("workspace.git.askReview")}
      </Button>
    </div>
  )
})
