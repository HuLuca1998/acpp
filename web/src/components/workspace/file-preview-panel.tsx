import { memo, useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { FileTextIcon } from "lucide-react"

import { api } from "@/lib/api"
import type { WorkspaceFile } from "@/types/acp"
import {
  usePreviewPath,
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

/**
 * 文件预览面板：只读、等宽、行号。行级 content-visibility 让首屏
 * 只付出可视区域的渲染成本，大文件滚动时按需绘制。
 */
export const FilePreviewPanel = memo(function FilePreviewPanel() {
  const { t } = useTranslation()
  const ws = useWorkspace()
  const path = usePreviewPath()
  const [file, setFile] = useState<WorkspaceFile | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!path || !ws.sessionId) return
    let stale = false
    setLoading(true)
    setError(null)
    api.sessions
      .workspaceFile(ws.sessionId, path)
      .then((view) => {
        if (!stale) setFile(view)
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
  }, [path, ws.sessionId])

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
          className="truncate font-mono text-xs text-muted-foreground"
          title={path}
        >
          {path}
        </span>
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        {error ? (
          <div className="p-3 text-xs text-destructive">{error}</div>
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
