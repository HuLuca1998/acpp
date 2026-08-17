import { memo, useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { AtSignIcon, FileTextIcon } from "lucide-react"

import type { GitDiffView, WorkspaceFile } from "@/types/acp"
import { DiffView } from "@/components/diff-view"
import {
  usePreviewTarget,
  useWorkspace,
} from "@/components/workspace/workspace-context"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Spinner } from "@/components/ui/spinner"

/** 预览渲染的行数上限：更大的文件截断展示，虚拟滚动是 M4 的事。 */
const MAX_RENDER_LINES = 5000

/** diff 渲染的行数上限（同样是虚拟滚动前的兜底）。 */
const DIFF_MAX_LINES = 2000

/**
 * 文件查看器面板：只读、等宽、行号，两种形态——
 * **file** 看文件当前内容，**diff** 看它改了什么（工作区改动或某条提交）。
 *
 * 合成一个面板是刻意的：这两件事是同一个阅读动作的两面，各占一个 tab
 * 只会让人在「现在什么样」和「改了什么」之间来回找。形态由命令总线的
 * 预览目标决定（文件树点文件 → file，变更面板点文件 → diff）。
 */
export const FilePreviewPanel = memo(function FilePreviewPanel() {
  const { t } = useTranslation()
  const ws = useWorkspace()
  const target = usePreviewTarget()
  const path = target?.path ?? null
  const [file, setFile] = useState<WorkspaceFile | null>(null)
  const [diff, setDiff] = useState<GitDiffView | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!target || !ws.sessionId) return
    let stale = false
    setLoading(true)
    setError(null)
    setFile(null)
    setDiff(null)

    const request =
      target.mode === "diff"
        ? target.sha
          ? ws.scope
              .gitCommitFile(ws.sessionId, target.sha, target.path)
              .then((view) => ({ diff: view }))
          : ws.scope
              .gitDiff(ws.sessionId, target.path)
              .then((view) => ({ diff: view }))
        : ws.scope
            .workspaceFile(ws.sessionId, target.path)
            .then((view) => ({ file: view }))

    request
      .then((result) => {
        if (stale) return
        if ("diff" in result) setDiff(result.diff)
        else setFile(result.file)
      })
      .catch((err) => {
        if (!stale) setError(err instanceof Error ? err.message : String(err))
      })
      .finally(() => {
        if (!stale) setLoading(false)
      })
    return () => {
      stale = true
    }
  }, [target, ws.sessionId, ws.scope])

  const { lines, clipped } = useMemo(() => {
    if (!file || file.binary) return { lines: [], clipped: false }
    const all = file.content.split("\n")
    if (all.length > MAX_RENDER_LINES) {
      return { lines: all.slice(0, MAX_RENDER_LINES), clipped: true }
    }
    return { lines: all, clipped: false }
  }, [file])

  if (!path) {
    return (
      <Empty className="h-full justify-center">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <FileTextIcon />
          </EmptyMedia>
          <EmptyTitle className="text-sm">
            {t("workspace.preview.emptyTitle")}
          </EmptyTitle>
          <EmptyDescription className="text-xs">
            {t("workspace.preview.emptyHint")}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className="flex h-full flex-col [contain:strict]">
      <div className="flex h-8 shrink-0 items-center gap-1.5 border-b border-border px-3">
        {loading ? <Spinner className="size-3" /> : null}
        <span
          className="min-w-0 flex-1 truncate font-mono text-xs text-muted-foreground [direction:rtl] [unicode-bidi:plaintext]"
          title={path}
        >
          {path}
        </span>
        {target?.mode === "diff" ? (
          <span className="shrink-0 font-mono text-[10px] text-muted-foreground/70">
            {target.sha
              ? target.sha.slice(0, 7)
              : t("workspace.git.workingTree")}
          </span>
        ) : null}
        <button
          type="button"
          aria-label={t("workspace.refMenu.addReference")}
          title={t("workspace.refMenu.addReference")}
          className="flex size-6 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97]"
          onClick={() => ws.addReference(path)}
        >
          <AtSignIcon className="size-3.5" />
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        {error ? (
          <div className="p-3 text-xs text-destructive">{error}</div>
        ) : diff ? (
          diff.binary ? (
            <div className="p-3 text-xs text-muted-foreground">
              {t("workspace.git.binary")}
            </div>
          ) : (
            <div className="p-2">
              <DiffView
                oldText={diff.oldText}
                newText={diff.newText}
                maxLines={DIFF_MAX_LINES}
              />
            </div>
          )
        ) : file?.binary ? (
          <div className="p-3 text-xs text-muted-foreground">
            {t("workspace.preview.binary")}
          </div>
        ) : file ? (
          <div className="w-max min-w-full py-2 font-mono text-xs leading-5">
            {lines.map((line, i) => (
              <div
                key={i}
                className="flex [contain-intrinsic-block-size:1.25rem] [content-visibility:auto]"
              >
                <span className="w-12 shrink-0 pr-3 text-right text-muted-foreground/60 tabular-nums select-none">
                  {i + 1}
                </span>
                <span className="pr-4 whitespace-pre">{line}</span>
              </div>
            ))}
            {file.truncated || clipped ? (
              <div className="px-12 py-2 text-muted-foreground">
                {t("workspace.preview.truncated")}
              </div>
            ) : null}
          </div>
        ) : null}
      </div>
    </div>
  )
})
