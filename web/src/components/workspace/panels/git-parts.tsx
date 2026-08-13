import { useTranslation } from "react-i18next"
import { GitBranchIcon, RotateCwIcon } from "lucide-react"

import { Spinner } from "@/components/ui/spinner"
import { cn } from "@/lib/utils"
import type { GitOverview } from "@/types/acp"

/** diff / commit 面板共用的头部：分支、领先/落后、刷新。 */
export function GitPanelHeader({
  overview,
  loading,
  onRefresh,
}: {
  overview: GitOverview
  loading: boolean
  onRefresh: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className="flex h-8 shrink-0 items-center gap-1.5 px-2.5 text-xs text-muted-foreground">
      <GitBranchIcon className="size-3.5 shrink-0" />
      <span className="truncate font-mono">{overview.branch}</span>
      {overview.ahead > 0 ? (
        <span className="shrink-0 tabular-nums">↑{overview.ahead}</span>
      ) : null}
      {overview.behind > 0 ? (
        <span className="shrink-0 tabular-nums">↓{overview.behind}</span>
      ) : null}
      <span className="flex-1" />
      {loading ? <Spinner className="size-3" /> : null}
      <button
        type="button"
        aria-label={t("workspace.git.refresh")}
        className="flex size-6 items-center justify-center rounded-md transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97]"
        onClick={onRefresh}
      >
        <RotateCwIcon className="size-3.5" />
      </button>
    </div>
  )
}

/** 变更类型字母：A 新增 / D 删除 / 其余修改类，色调与 DiffView 呼应。 */
export function StatusLetter({ status }: { status: string }) {
  return (
    <span
      className={cn(
        "w-3 shrink-0 text-center",
        status === "A" && "text-primary",
        status === "D" && "text-destructive",
        status !== "A" && status !== "D" && "text-muted-foreground"
      )}
    >
      {status}
    </span>
  )
}

/** +n -m 行数统计；-1（二进制等）不显示。 */
export function ChangeStat({
  added,
  deleted,
}: {
  added: number
  deleted: number
}) {
  return (
    <span className="shrink-0 tabular-nums">
      {added >= 0 ? <span className="text-primary">+{added}</span> : null}{" "}
      {deleted >= 0 ? (
        <span className="text-destructive">-{deleted}</span>
      ) : null}
    </span>
  )
}
