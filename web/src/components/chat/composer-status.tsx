import { useTranslation } from "react-i18next"

import type { ContextUsage } from "@/hooks/use-chat"
import { formatTokens } from "@/lib/format"
import { FolderIcon, GitBranchIcon } from "lucide-react"

/**
 * 输入卡下沿的状态栏：工作目录、git 分支、上下文用量。
 * 一行小字，缺什么就不显示什么，永远不喧宾夺主。
 */
export function ComposerStatus({
  cwd,
  gitBranch,
  usage,
}: {
  cwd?: string
  gitBranch?: string
  usage?: ContextUsage | null
}) {
  const { t } = useTranslation()
  if (!cwd && !gitBranch && !usage) return null

  return (
    <div className="flex items-center gap-3 text-xs text-muted-foreground/80">
      {cwd ? (
        <span className="flex min-w-0 items-center gap-1" title={cwd}>
          <FolderIcon className="size-3 shrink-0" />
          <span className="truncate font-mono">{cwd}</span>
        </span>
      ) : null}
      {gitBranch ? (
        <span className="flex shrink-0 items-center gap-1">
          <GitBranchIcon className="size-3" />
          <span className="font-mono">{gitBranch}</span>
        </span>
      ) : null}
      {usage && usage.size > 0 ? (
        <span
          className="ml-auto shrink-0 tabular-nums"
          title={`${usage.used.toLocaleString()} / ${usage.size.toLocaleString()} tokens`}
        >
          {t("chat.status.context", {
            used: formatTokens(usage.used),
            size: formatTokens(usage.size),
            percent: Math.round((usage.used / usage.size) * 100),
          })}
        </span>
      ) : null}
    </div>
  )
}
