import { useTranslation } from "react-i18next"

import { cn } from "@/lib/utils"
import type { PlanEntry } from "@/types/acp"
import { Spinner } from "@/components/ui/spinner"
import { CircleCheckIcon, CircleIcon, ListTodoIcon } from "lucide-react"

/**
 * agent 的任务计划清单：逐项显示状态（待办/进行中/已完成）与总进度。
 * 数据来自 plan 事件，随每次更新整体替换。
 */
export function PlanCard({ entries }: { entries: PlanEntry[] }) {
  const { t } = useTranslation()
  const done = entries.filter((e) => e.status === "completed").length

  return (
    <div className="rounded-xl border border-border bg-card/50 px-4 py-3">
      <div className="mb-2 flex items-center gap-2 text-sm font-medium">
        <ListTodoIcon className="size-4 text-primary" />
        {t("chat.plan")}
        <span className="ml-auto text-xs font-normal text-muted-foreground tabular-nums">
          {done} / {entries.length}
        </span>
      </div>
      <ul className="flex flex-col gap-1.5">
        {entries.map((entry, i) => (
          <li
            key={i}
            className="flex items-start gap-2 text-sm transition-colors duration-200"
          >
            {entry.status === "completed" ? (
              <CircleCheckIcon className="mt-0.5 size-4 shrink-0 text-success" />
            ) : entry.status === "in_progress" ? (
              <Spinner className="mt-0.5 size-4 shrink-0" />
            ) : (
              <CircleIcon className="mt-0.5 size-4 shrink-0 text-muted-foreground/50" />
            )}
            <span
              className={cn(
                entry.status === "completed" &&
                  "text-muted-foreground line-through",
                entry.status === "in_progress" && "font-medium",
                entry.status === "pending" && "text-muted-foreground"
              )}
            >
              {entry.content}
            </span>
          </li>
        ))}
      </ul>
    </div>
  )
}
