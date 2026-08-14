import { memo, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { ChevronRightIcon } from "lucide-react"

import { cn } from "@/lib/utils"
import { formatRelativeTime } from "@/lib/format"
import type {
  GitCommit,
  GitCommitDetail,
  GitDiffView,
  GitFileChange,
} from "@/types/acp"
import { DiffView } from "@/components/diff-view"
import {
  ChangeStat,
  GitPanelHeader,
  StatusLetter,
} from "@/components/workspace/panels/git-parts"
import { PanelEmptyState } from "@/components/workspace/panels/panel-empty-state"
import {
  useGitOverview,
  useWorkspace,
} from "@/components/workspace/workspace-context"
import { Spinner } from "@/components/ui/spinner"

const COMMIT_DIFF_MAX_LINES = 2000

/**
 * 未推送 commit 面板：`@{u}..HEAD` 列表；展开一条拉文件清单，点文件
 * 再拉该提交前后全文。无 upstream 时退化为最近提交并明确标注。
 */
export const CommitsPanel = memo(function CommitsPanel() {
  const { t, i18n } = useTranslation()
  const ws = useWorkspace()
  const git = useGitOverview()

  const pulled = useRef(false)
  useEffect(() => {
    if (pulled.current) return
    if (ws.sessionId && git.data === null && !git.loading) {
      pulled.current = true
      ws.refreshGit()
    }
  }, [ws, git])

  if (!ws.sessionId) {
    return (
      <PanelEmptyState
        title={t("workspace.tree.draftTitle")}
        description={t("workspace.tree.draftHint")}
      />
    )
  }

  if (git.error) {
    return (
      <PanelEmptyState
        title={t("workspace.git.loadFailed")}
        description={git.error}
      />
    )
  }

  if (git.data && !git.data.isRepo) {
    return <PanelEmptyState title={t("workspace.git.notRepo")} />
  }

  if (git.data === null) {
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner className="size-4 text-muted-foreground" />
      </div>
    )
  }

  const { commits, upstream } = git.data
  return (
    <div className="flex h-full flex-col [contain:strict]">
      <GitPanelHeader
        overview={git.data}
        loading={git.loading}
        onRefresh={ws.refreshGit}
      />
      <div className="min-h-0 flex-1 overflow-auto px-1.5 pb-2">
        {!upstream ? (
          <div className="px-2 pb-1 text-xs text-muted-foreground">
            {t("workspace.git.noUpstream")}
          </div>
        ) : null}
        {commits.length === 0 ? (
          <div className="px-2 py-4 text-xs text-muted-foreground">
            {t("workspace.git.allPushed")}
          </div>
        ) : (
          commits.map((commit) => (
            <CommitRow
              key={commit.sha}
              sessionId={ws.sessionId}
              commit={commit}
              locale={i18n.language}
            />
          ))
        )}
      </div>
    </div>
  )
})

/** 一条提交：短 hash + 标题 + 相对时间；展开列文件清单。 */
function CommitRow({
  sessionId,
  commit,
  locale,
}: {
  sessionId: number
  commit: GitCommit
  locale: string
}) {
  const ws = useWorkspace()
  const [open, setOpen] = useState(false)
  const [detail, setDetail] = useState<GitCommitDetail | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open || detail !== null) return
    let stale = false
    ws.scope
      .gitCommit(sessionId, commit.sha)
      .then((d) => {
        if (!stale) setDetail(d)
      })
      .catch((err) => {
        if (!stale) setError(err instanceof Error ? err.message : String(err))
      })
    return () => {
      stale = true
    }
  }, [open, detail, sessionId, commit.sha, ws.scope])

  return (
    <div>
      <button
        type="button"
        title={`${commit.short} · ${commit.author}`}
        className="flex h-6 w-full items-center gap-1.5 rounded-md px-1.5 text-xs transition-colors duration-150 ease-snappy hover:bg-muted"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
      >
        <ChevronRightIcon
          className={cn(
            "size-3.5 shrink-0 text-muted-foreground transition-transform duration-150 ease-snappy",
            open && "rotate-90"
          )}
        />
        <span className="shrink-0 font-mono text-muted-foreground">
          {commit.short}
        </span>
        <span className="min-w-0 flex-1 truncate text-left text-foreground/90">
          {commit.subject}
        </span>
        <span className="shrink-0 text-muted-foreground tabular-nums">
          {formatRelativeTime(
            new Date(commit.time * 1000).toISOString(),
            locale
          )}
        </span>
      </button>
      {open ? (
        <div className="py-0.5 pl-6">
          {error ? (
            <div className="px-1 text-xs text-destructive">{error}</div>
          ) : detail === null ? (
            <div className="flex h-8 items-center px-1">
              <Spinner className="size-3 text-muted-foreground" />
            </div>
          ) : (
            detail.files.map((file) => (
              <CommitFileRow
                key={file.path}
                sessionId={sessionId}
                sha={commit.sha}
                file={file}
              />
            ))
          )}
        </div>
      ) : null}
    </div>
  )
}

/** 提交内的一个文件：点开看该提交前后的 diff。 */
function CommitFileRow({
  sessionId,
  sha,
  file,
}: {
  sessionId: number
  sha: string
  file: GitFileChange
}) {
  const { t } = useTranslation()
  const ws = useWorkspace()
  const [open, setOpen] = useState(false)
  const [diff, setDiff] = useState<GitDiffView | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open || diff !== null) return
    let stale = false
    ws.scope
      .gitCommitFile(sessionId, sha, file.path)
      .then((view) => {
        if (!stale) setDiff(view)
      })
      .catch((err) => {
        if (!stale) setError(err instanceof Error ? err.message : String(err))
      })
    return () => {
      stale = true
    }
  }, [open, diff, sessionId, sha, file.path, ws.scope])

  return (
    <div>
      <button
        type="button"
        title={file.path}
        className="flex h-6 w-full items-center gap-1.5 rounded-md px-1.5 font-mono text-xs transition-colors duration-150 ease-snappy hover:bg-muted"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
      >
        <ChevronRightIcon
          className={cn(
            "size-3 shrink-0 text-muted-foreground transition-transform duration-150 ease-snappy",
            open && "rotate-90"
          )}
        />
        <StatusLetter status={file.status} />
        <span className="min-w-0 flex-1 truncate text-left text-foreground/90 [direction:rtl] [unicode-bidi:plaintext]">
          {file.path}
        </span>
        <ChangeStat added={file.added} deleted={file.deleted} />
      </button>
      {open ? (
        <div className="py-1 pr-1 pl-5">
          {error ? (
            <div className="px-1 text-xs text-destructive">{error}</div>
          ) : diff === null ? (
            <div className="flex h-8 items-center px-1">
              <Spinner className="size-3 text-muted-foreground" />
            </div>
          ) : diff.binary ? (
            <div className="px-1 text-xs text-muted-foreground">
              {t("workspace.git.binary")}
            </div>
          ) : (
            <DiffView
              oldText={diff.oldText}
              newText={diff.newText}
              maxLines={COMMIT_DIFF_MAX_LINES}
            />
          )}
        </div>
      ) : null}
    </div>
  )
}
