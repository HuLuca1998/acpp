import { useTranslation } from "react-i18next"

import type { ContextUsage } from "@/hooks/use-chat"
import type { SessionUsageTotals } from "@/lib/chat/usage"
import { UsagePopover } from "@/components/chat/composer/usage-popover"
import { useIdentity } from "@/hooks/identity-context"
import { displayPath } from "@/lib/format"
import type { TurnUsage } from "@/types/acp"
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
  lastUsage,
  totals,
  onPickCwd,
}: {
  cwd?: string
  gitBranch?: string
  /** 可交互的分支控件（BranchPicker）；给了它就不再渲染纯文本分支。 */
  branchSlot?: React.ReactNode
  /** 草稿态的 worktree 开关，跟在分支之后。 */
  worktreeSlot?: React.ReactNode
  usage?: ContextUsage | null
  /** 最近一轮的 token 计量（turn_end 携带），悬停给完整四项。 */
  lastUsage?: TurnUsage | null
  /** 会话累计用量（历史各轮相加），点开用量面板时展示。 */
  totals?: SessionUsageTotals | null
  /** 草稿态：点击工作目录打开目录选择器；老会话不传，纯展示。 */
  onPickCwd?: () => void
}) {
  const { t } = useTranslation()
  const { identity } = useIdentity()
  // 访客看到的是 `~/...`：完整路径里带着这台机器主人的用户名，对他没用。
  const shownCwd = displayPath(cwd ?? "", identity?.root)
  if (
    !cwd &&
    !gitBranch &&
    !branchSlot &&
    !worktreeSlot &&
    !usage &&
    !lastUsage &&
    !totals
  )
    return null

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
      {/* 右侧只留一个环形按钮：占用比例一眼可见，明细（本轮/累计/费用）
          悬停给摘要、点击展开面板——状态栏不该被一串数字占满。 */}
      {(usage && usage.size > 0) || totals ? (
        <UsagePopover
          usage={usage}
          lastUsage={lastUsage}
          totals={totals}
          className="ml-auto"
        />
      ) : null}
    </div>
  )
}
