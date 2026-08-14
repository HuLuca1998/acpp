import { memo, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { ChevronRightIcon } from "lucide-react"

import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { GitDiffView, GitFileChange, GitOverview } from "@/types/acp"
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

/** 单文件 diff 渲染的行数上限（虚拟滚动前的兜底）。 */
const DIFF_MAX_LINES = 2000

/**
 * 工作区 diff 面板：文件级折叠列表，点开才拉单文件 diff（懒加载）。
 * 数据来自共享的 gitStore，turn 结束与手动刷新都会重取。
 */
export const DiffPanel = memo(function DiffPanel() {
  const { t } = useTranslation()
  const ws = useWorkspace()
  const git = useGitOverview()

  // 惰性首拉：面板第一次可见才请求，符合「待命 tab 只挂壳」的初始预算。
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

  const files = git.data.files
  return (
    <div className="flex h-full flex-col [contain:strict]">
      <GitPanelHeader
        overview={git.data}
        loading={git.loading}
        onRefresh={ws.refreshGit}
      />
      <div className="min-h-0 flex-1 overflow-auto px-1.5 pb-2">
        {files.length === 0 ? (
          <div className="px-2 py-4 text-xs text-muted-foreground">
            {t("workspace.git.clean")}
          </div>
        ) : (
          files.map((file) => (
            <DiffFileRow
              key={file.path}
              sessionId={ws.sessionId}
              file={file}
              version={git.data}
            />
          ))
        )}
      </div>
    </div>
  )
})

/** 一个文件的变更行：状态字母 + 路径 + 行数统计；展开时懒加载 diff。 */
function DiffFileRow({
  sessionId,
  file,
  version,
}: {
  sessionId: number
  file: GitFileChange
  /** gitStore 快照引用：刷新后引用变化，展开中的行重新拉取。 */
  version: GitOverview | null
}) {
  const [open, setOpen] = useState(false)
  const [diff, setDiff] = useState<GitDiffView | null>(null)
  const [error, setError] = useState<string | null>(null)
  const { t } = useTranslation()

  useEffect(() => {
    if (!open) return
    let stale = false
    api.sessions
      .gitDiff(sessionId, file.path)
      .then((view) => {
        if (!stale) {
          setDiff(view)
          setError(null)
        }
      })
      .catch((err) => {
        if (!stale) setError(err instanceof Error ? err.message : String(err))
      })
    return () => {
      stale = true
    }
  }, [open, sessionId, file.path, version])

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
            "size-3.5 shrink-0 text-muted-foreground transition-transform duration-150 ease-snappy",
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
        <div className="py-1 pr-1 pl-6">
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
              maxLines={DIFF_MAX_LINES}
            />
          )}
        </div>
      ) : null}
    </div>
  )
}
