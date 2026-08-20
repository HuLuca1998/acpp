import { useTranslation } from "react-i18next"

import { Hint } from "@/components/hint"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { SplitIcon, XIcon } from "lucide-react"

/**
 * 草稿态的 worktree 开关（adr-007）：勾上就在所选仓库下开
 * `worktrees/<名字>`，会话的工作目录指向那里，主工作区不受打扰。
 *
 * 名字预填一个带时间戳的默认值——多数人只是想要「一个干净的地方」，
 * 不想为它取名；要取也就在原地改。
 */
export function WorktreeToggle({
  value,
  onChange,
}: {
  /** 空串表示不用 worktree。 */
  value: string
  onChange: (value: string) => void
}) {
  const { t } = useTranslation()

  if (!value) {
    return (
      <Hint
        label={t("chat.branch.useWorktree")}
        desc={t("chat.branch.useWorktreeDesc")}
      >
        <button
          type="button"
          className="flex shrink-0 items-center gap-1 rounded-md text-xs text-muted-foreground/80 transition-colors duration-150 hover:text-foreground"
          onClick={() => onChange(defaultWorktreeName())}
        >
          <SplitIcon className="size-3" />
          <span>{t("chat.branch.useWorktree")}</span>
        </button>
      </Hint>
    )
  }

  return (
    <span className="flex shrink-0 items-center gap-1">
      <SplitIcon className="size-3 text-muted-foreground/80" />
      <Input
        value={value}
        aria-label={t("chat.branch.worktreePlaceholder")}
        className="h-6 w-36 font-mono text-xs"
        onChange={(e) => onChange(e.target.value)}
      />
      <Hint label={t("chat.branch.dropWorktree")}>
        <Button
          size="icon-sm"
          variant="ghost"
          className="size-6"
          aria-label={t("chat.branch.dropWorktree")}
          onClick={() => onChange("")}
        >
          <XIcon />
        </Button>
      </Hint>
    </span>
  )
}

/** `wt-0817-1530`：短、可读、当天多开也不撞名。 */
function defaultWorktreeName(): string {
  const now = new Date()
  const pad = (n: number) => String(n).padStart(2, "0")
  return `wt-${pad(now.getMonth() + 1)}${pad(now.getDate())}-${pad(now.getHours())}${pad(now.getMinutes())}`
}
