import { useState } from "react"
import { useTranslation } from "react-i18next"

import type { PendingPermission } from "@/types/acp"
import { MarkdownContent } from "@/components/chat/markdown"
import { Button } from "@/components/ui/button"
import { ClipboardCheckIcon } from "lucide-react"

/**
 * 「计划完成」审批卡：渲染 markdown 计划全文，按钮是统一档位词汇
 * （不是 agent 的英文原文）。选项由后端 adapter 翻译，词汇表外的档
 * 已被丢弃；level 为空的选项是「继续规划」。
 */
export function PlanReviewCard({
  permission,
  onResolve,
}: {
  permission: PendingPermission
  onResolve: (optionId: string, choiceName: string) => void
}) {
  const { t } = useTranslation()
  const [submitted, setSubmitted] = useState(false)

  const review = permission.planReview
  if (!review) return null

  const runChoices = review.choices.filter((c) => c.level)
  const keepPlanning = review.choices.find((c) => !c.level)

  const pick = (optionId: string, name: string) => {
    setSubmitted(true)
    onResolve(optionId, name)
  }

  return (
    <div className="rounded-xl border border-border bg-card p-4 shadow-sm">
      <div className="mb-3 flex items-center gap-2 text-sm font-medium">
        <ClipboardCheckIcon className="size-4 text-primary" />
        {t("chat.planReview.title")}
      </div>

      <div className="mb-3 max-h-80 overflow-y-auto rounded-lg border border-border bg-background/50 px-3 py-2">
        <MarkdownContent>{review.plan}</MarkdownContent>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        {runChoices.map((choice) => {
          const name = t("chat.planReview.run", {
            level: t(`chat.settings.level.${choice.level!}`),
          })
          return (
            <Button
              key={choice.optionId}
              size="sm"
              disabled={submitted}
              onClick={() => pick(choice.optionId, name)}
            >
              {name}
            </Button>
          )
        })}
        {keepPlanning ? (
          <Button
            size="sm"
            variant="ghost"
            className="ml-auto text-muted-foreground"
            disabled={submitted}
            onClick={() =>
              pick(keepPlanning.optionId, t("chat.planReview.keepPlanning"))
            }
          >
            {t("chat.planReview.keepPlanning")}
          </Button>
        ) : null}
      </div>
    </div>
  )
}
