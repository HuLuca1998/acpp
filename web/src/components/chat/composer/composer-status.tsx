import { useTranslation } from "react-i18next"

import type { ContextUsage } from "@/hooks/use-chat"
import { useIdentity } from "@/hooks/identity-context"
import { displayPath, formatTokens } from "@/lib/format"
import { FolderIcon, GitBranchIcon, PencilIcon } from "lucide-react"

/**
 * 输入卡下沿的状态栏：工作目录、git 分支、上下文用量。
 * 一行小字，缺什么就不显示什么，永远不喧宾夺主。
 * 新老会话共用；草稿态传 onPickCwd，工作目录变成可点击修改——
 * 同一个组件同一个位置，差异只有可不可编辑。
 */
export function ComposerStatus({
  cwd,
  gitBranch,
  branchSlot,
  worktreeSlot,
  usage,
  onPickCwd,
}: {
  cwd?: string
  gitBranch?: string
  /** 可交互的分支控件（BranchPicker）；给了它就不再渲染纯文本分支。 */
  branchSlot?: React.ReactNode
  /** 草稿态的 worktree 开关，跟在分支之后。 */
  worktreeSlot?: React.ReactNode
  usage?: ContextUsage | null
  /** 草稿态：点击工作目录打开目录选择器；老会话不传，纯展示。 */
  onPickCwd?: () => void
}) {
  const { t } = useTranslation()
  const { identity } = useIdentity()
  // 访客看到的是 `~/...`：完整路径里带着这台机器主人的用户名，对他没用。
  const shownCwd = displayPath(cwd ?? "", identity?.root)
  if (!cwd && !gitBranch && !branchSlot && !worktreeSlot && !usage) return null

  return (
    <div className="flex items-center gap-3 text-xs text-muted-foreground/80">
      {cwd && onPickCwd ? (
        <button
          type="button"
          title={t("sessions.form.cwdHint")}
          className="group flex min-w-0 items-center gap-1 rounded-md transition-colors duration-150 hover:text-foreground"
          onClick={onPickCwd}
        >
          <FolderIcon className="size-3 shrink-0" />
          <span className="truncate font-mono">{shownCwd}</span>
          <PencilIcon className="size-3 shrink-0 opacity-0 transition-opacity duration-150 group-hover:opacity-100" />
        </button>
      ) : cwd ? (
        <span className="flex min-w-0 items-center gap-1" title={cwd}>
          <FolderIcon className="size-3 shrink-0" />
          <span className="truncate font-mono">{shownCwd}</span>
        </span>
      ) : null}
      {branchSlot ??
        (gitBranch ? (
          <span className="flex shrink-0 items-center gap-1">
            <GitBranchIcon className="size-3" />
            <span className="font-mono">{gitBranch}</span>
          </span>
        ) : null)}
      {worktreeSlot}
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
