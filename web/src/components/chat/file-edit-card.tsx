import { useContext, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"

import { lineDiff } from "@/lib/line-diff"
import { Hint } from "@/components/hint"
import { cn } from "@/lib/utils"
import { DiffView } from "@/components/diff-view"
import type { ToolCallPayload } from "@/components/chat/tool-call"
import { WorkspaceContext } from "@/components/workspace/workspace-context"
import { Badge } from "@/components/ui/badge"
import { Spinner } from "@/components/ui/spinner"
import { ChevronRightIcon, FileDiffIcon } from "lucide-react"

/** 一个文件的编辑摘要：路径 + 增删行数（lineDiff 与 diff 视图同一套算法）。 */
interface EditSummary {
  path: string
  oldText: string
  newText: string
  added: number
  removed: number
}

function summarize(payload: ToolCallPayload): EditSummary[] {
  return (payload.content ?? [])
    .filter((c) => c.type === "diff" && typeof c.newText === "string")
    .map((c) => {
      const oldText = c.oldText ?? ""
      const newText = c.newText ?? ""
      let added = 0
      let removed = 0
      for (const line of lineDiff(oldText, newText)) {
        if (line.type === "add") added++
        else if (line.type === "del") removed++
      }
      return { path: c.path ?? "", oldText, newText, added, removed }
    })
}

/**
 * 文件编辑的独立消息条（不折叠进「思考与工具调用」）：
 * 「已编辑 <文件名> +a -d」，点文件名打开工作区预览，点行首箭头展开 diff。
 */
export function FileEditCard({
  payload,
  status,
}: {
  payload: ToolCallPayload
  status?: string
}) {
  const { t } = useTranslation()
  // 不在工作区宿主里时降级为纯展示（文件名不可点）。
  const ws = useContext(WorkspaceContext)
  const [open, setOpen] = useState(false)
  const edits = useMemo(() => summarize(payload), [payload])
  if (edits.length === 0) return null

  return (
    <div className="flex flex-col gap-1.5">
      {edits.map((edit) => (
        <div key={edit.path} className="flex flex-col gap-2">
          <div className="flex min-h-4 items-center gap-2 text-sm">
            <Hint label={t("chat.fileEdit.toggle")} align="start">
              <button
                type="button"
                aria-expanded={open}
                aria-label={t("chat.fileEdit.toggle")}
                onClick={() => setOpen((o) => !o)}
                className="shrink-0 rounded-sm text-muted-foreground transition-colors outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
              >
                <ChevronRightIcon
                  className={cn(
                    "size-4 transition-transform",
                    open && "rotate-90"
                  )}
                />
              </button>
            </Hint>
            <FileDiffIcon className="size-4 shrink-0 text-muted-foreground" />
            <span className="shrink-0 text-muted-foreground">
              {t("chat.fileEdit.edited")}
            </span>
            <button
              type="button"
              title={edit.path}
              disabled={!ws}
              onClick={() => ws?.openPreview(edit.path)}
              className="min-w-0 truncate rounded-sm text-left font-mono outline-none enabled:hover:underline enabled:focus-visible:ring-2 enabled:focus-visible:ring-ring/50"
            >
              {edit.path.split("/").pop()}
            </button>
            <span className="flex shrink-0 items-center gap-1 font-mono text-xs tabular-nums">
              <span className="text-primary">+{edit.added}</span>
              <span className="text-destructive">-{edit.removed}</span>
            </span>
            {status === "in_progress" || status === "pending" ? (
              <Spinner className="size-3 shrink-0 text-muted-foreground" />
            ) : status === "failed" ? (
              <Badge variant="destructive" className="shrink-0">
                {t("chat.toolStatus.failed")}
              </Badge>
            ) : null}
          </div>
          {open ? (
            <div className="pl-6 transition-[opacity,translate] duration-200 ease-snappy starting:-translate-y-0.5 starting:opacity-0 motion-reduce:starting:translate-y-0">
              <DiffView
                path={edit.path}
                oldText={edit.oldText}
                newText={edit.newText}
              />
            </div>
          ) : null}
        </div>
      ))}
    </div>
  )
}
