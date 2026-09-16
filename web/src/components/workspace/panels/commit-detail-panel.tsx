import { memo, useCallback, useRef, useState, type ReactNode } from "react"
import { useTranslation } from "react-i18next"
import type { IDockviewPanelProps } from "dockview-react"

import { useVisibleLoad } from "@/hooks/use-panel-visible"
import { GitPanelHeader } from "@/components/workspace/panels/git-parts"
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
export const CommitDetailPanel = memo(function CommitDetailPanel(
  props: IDockviewPanelProps
) {
  const { t, i18n } = useTranslation()
  const ws = useWorkspace()
  const selection = useGitSelection()
  const git = useGitOverview()

  const [commit, setCommit] = useState<GitCommit | null>(null)
  const [compare, setCompare] = useState<GitCompare | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // stale 守卫：切选择比请求回来快是常事，没有它旧结果会盖掉新结果，
  // 面板显示的就是上一次点的那条。
  const seqRef = useRef(0)

  const comparing = selection.refs.length === 2
  const [base, head] = selection.refs

  const load = useCallback(() => {
    if (!ws.ready) return
    const seq = ++seqRef.current
    const fresh = () => seqRef.current === seq

    if (comparing) {
      ws.scope
        .gitCompare(ws.sessionId, base, head)
        .then((data) => {
          if (!fresh()) return
          setCompare(data)
          setCommit(null)
          setError(null)
        })
        .catch((err: Error) => fresh() && setError(err.message))
        .finally(() => fresh() && setLoading(false))
      return
    }

    if (!selection.sha) {
      // 退出对比 / 取消选择：把两份结果都清掉，否则面板还挂着上一次的
      // 对比摘要，看上去像是还选着。
      setCompare(null)
      setCommit(null)
      setError(null)
      // 看工作区时这一栏的内容来自共享的 git 汇总，刷新落在它身上。
      ws.refreshGit()
      return
    }

    ws.scope
      .gitCommit(ws.sessionId, selection.sha)
      .then((detail) => {
        if (!fresh()) return
        setCommit(detail.commit)
        setCompare(null)
        setError(null)
      })
      .catch((err: Error) => fresh() && setError(err.message))
      .finally(() => fresh() && setLoading(false))
  }, [ws, comparing, base, head, selection.sha])

  // 选择一变就重拉（load 的引用跟着选择走），工作区刷新广播同样带动它：
  // 看的若是工作区那条，提交与分支在 agent 手里随时会变。
  // 藏在 tab 后面时不白拉，切回来补一次。
  useVisibleLoad(props.api, ws.onWorkspaceRefresh, load)

  const refresh = useCallback(() => {
    setLoading(true)
    load()
  }, [load])

  if (!ws.ready) {
    return (
      <PanelEmptyState
        title={t("workspace.tree.draftTitle")}
        description={t("workspace.tree.draftHint")}
      />
    )
  }
  const title = comparing
    ? `${base} → ${head}`
    : selection.sha
      ? selection.sha.slice(0, 7)
      : t("workspace.git.workingTree")

  // 头部（含刷新）在任何状态下都得在：加载失败、正在拉、什么都没选的
  // 时候，恰恰是最需要「再读一遍」的时候。
  const shell = (body: ReactNode) => (
    <div className="flex h-full flex-col">
      <GitPanelHeader
        title={title}
        loading={loading || git.loading}
        onRefresh={refresh}
      />
      <div className="min-h-0 flex-1 overflow-y-auto">{body}</div>
    </div>
  )

  if (error) {
    return shell(
      <PanelEmptyState title={t("common.loadFailed")} description={error} />
    )
  }
  if (loading) {
    return shell(
      <div className="flex h-full items-center justify-center">
        <Spinner className="size-4 text-muted-foreground" />
      </div>
    )
  }

  if (compare) {
    return shell(
      <div className="flex flex-col gap-3 p-3 text-xs">
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
    return shell(
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

  return shell(
    <div className="flex flex-col gap-3 p-3">
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
