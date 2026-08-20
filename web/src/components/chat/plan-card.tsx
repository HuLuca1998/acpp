import { useState } from "react"
import { useTranslation } from "react-i18next"

import { cn } from "@/lib/utils"
import type { PlanEntry } from "@/types/acp"
import { Spinner } from "@/components/ui/spinner"
import {
  ChevronRightIcon,
  CircleCheckIcon,
  CircleIcon,
  ListTodoIcon,
} from "lucide-react"

/** 计划条目列表：实时卡与历史快照卡共用同一份渲染。 */
function PlanEntryList({ entries }: { entries: PlanEntry[] }) {
  return (
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
  )
}

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
      <PlanEntryList entries={entries} />
    </div>
  )
}

/**
 * 计划的历史快照（转录重建的 plan 消息）：默认折叠成一行进度，点开看全量。
 * 历史里计划已经定格，摊开只会把消息流顶得很长。
 */
export function PlanHistoryCard({ entries }: { entries: PlanEntry[] }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const done = entries.filter((e) => e.status === "completed").length

  return (
    <div className="rounded-xl border border-border bg-card/50 px-4 py-3">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className="flex w-full items-center gap-2 text-sm font-medium outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
      >
        <ListTodoIcon className="size-4 text-primary" />
        {t("chat.plan")}
        <span className="ml-auto text-xs font-normal text-muted-foreground tabular-nums">
          {done} / {entries.length}
        </span>
        <ChevronRightIcon
          className={cn(
            "size-4 text-muted-foreground transition-transform duration-150 ease-snappy",
            open && "rotate-90"
          )}
        />
      </button>
      {open ? (
        <div className="mt-2">
          <PlanEntryList entries={entries} />
        </div>
      ) : null}
    </div>
  )
}
