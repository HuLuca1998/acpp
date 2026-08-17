import { useTranslation } from "react-i18next"
import { GitBranchIcon, RotateCwIcon } from "lucide-react"

import { Spinner } from "@/components/ui/spinner"
import { cn } from "@/lib/utils"
/** git 面板群共用的头部：主标题（多为当前分支）、右侧提示、刷新。 */
export function GitPanelHeader({
  title,
  hint,
  loading,
  onRefresh,
}: {
  title: string
  /** 右侧的次要说明（对比中的两个 ref、选择提示等）。 */
  hint?: string
  loading?: boolean
  onRefresh: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className="flex h-8 shrink-0 items-center gap-1.5 px-2.5 text-xs text-muted-foreground">
      <GitBranchIcon className="size-3.5 shrink-0" />
      <span className="truncate font-mono">{title}</span>
      {hint ? (
        <span className="min-w-0 flex-1 truncate text-right text-[11px] text-muted-foreground/70">
          {hint}
        </span>
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
