import { useMemo } from "react"
import { useTranslation } from "react-i18next"
import { FileDiffIcon } from "lucide-react"

import { lineDiff } from "@/lib/line-diff"
import { cn } from "@/lib/utils"

/**
 * 行级 diff 视图：聊天工具调用与工作区 diff/commit 面板共用。
 * maxLines 之外的行不渲染（超大 diff 的兜底，虚拟滚动是 M4 的事）。
 */
export function DiffView({
  path,
  oldText,
  newText,
  maxLines,
  className,
}: {
  path?: string
  oldText: string
  newText: string
  maxLines?: number
  className?: string
}) {
  const { t } = useTranslation()
  const all = useMemo(() => lineDiff(oldText, newText), [oldText, newText])
  const lines = maxLines && all.length > maxLines ? all.slice(0, maxLines) : all
  const clipped = all.length - lines.length

  return (
    <div
      className={cn(
        "overflow-hidden rounded-lg border border-border",
        className
      )}
    >
      {path ? (
        <div
          className="flex items-center gap-1.5 border-b border-border bg-muted/50 px-2.5 py-1.5 font-mono text-xs text-muted-foreground"
          title={path}
        >
          <FileDiffIcon className="size-3.5 shrink-0" />
          <span className="truncate [direction:rtl] [unicode-bidi:plaintext]">
            {path}
          </span>
        </div>
      ) : null}
      <pre className="max-h-72 overflow-auto bg-background/50 py-1 font-mono text-xs leading-5">
        {lines.map((line, idx) => (
          <div
            key={idx}
            className={cn(
              "px-2.5 whitespace-pre-wrap",
              line.type === "del" &&
                "bg-destructive/10 text-destructive dark:bg-destructive/15",
              line.type === "add" &&
                "bg-primary/10 text-primary dark:bg-primary/15",
              line.type === "same" && "text-muted-foreground"
            )}
          >
            {line.type === "del" ? "- " : line.type === "add" ? "+ " : "  "}
            {line.text}
          </div>
        ))}
        {clipped > 0 ? (
          <div className="px-2.5 text-muted-foreground">
            {t("workspace.diff.clipped", { count: clipped })}
          </div>
        ) : null}
      </pre>
    </div>
  )
}
