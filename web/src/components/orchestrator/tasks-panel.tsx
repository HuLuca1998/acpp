import { memo } from "react"
import { useTranslation } from "react-i18next"

import { useOrchCtx } from "@/components/orchestrator/orch-context"
import { StatusDot } from "@/components/status-dot"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { api } from "@/lib/api"
import { formatRelativeTime } from "@/lib/format"
import type { OrchTask } from "@/types/acp"
import { ListTodoIcon, SquareIcon } from "lucide-react"

function taskTone(state: OrchTask["state"]) {
  switch (state) {
    case "running":
      return "success" as const
    case "failed":
      return "destructive" as const
    default:
      return "muted" as const
  }
}

function formatDuration(ms: number) {
  if (ms <= 0) return ""
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`
  return `${Math.round(ms / 60_000)}m${Math.round((ms % 60_000) / 1000)}s`
}

/**
 * 任务列表面板（adr-006 的常驻派发流）：每次 spawn 一行，实时更新。
 * 子会话不自动弹出——点行打开（或聚焦）对应的任务面板，关掉面板任务照跑。
 */
export const OrchTasksPanel = memo(function OrchTasksPanel() {
  const { t, i18n } = useTranslation()
  const { chat, openTaskPanel } = useOrchCtx()

  if (chat.tasks.length === 0) {
    return (
      <Empty className="h-full justify-center bg-background">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <ListTodoIcon />
          </EmptyMedia>
          <EmptyTitle>{t("orch.tasks.empty")}</EmptyTitle>
          <EmptyDescription>{t("orch.tasks.emptyHint")}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  // 新任务在上：正在跑的事优先被看见。
  const tasks = [...chat.tasks].reverse()

  return (
    <ScrollArea className="h-full bg-background">
      <ul className="flex flex-col gap-1 p-2">
        {tasks.map((task) => (
          <li key={task.id} className="group relative">
            <button
              type="button"
              className="flex w-full flex-col gap-1 rounded-md border border-border/60 bg-card px-3 py-2 text-left transition-colors duration-150 ease-snappy hover:bg-accent"
              onClick={() => openTaskPanel(task.id)}
            >
              <span className="flex items-center gap-2 text-sm">
                <StatusDot
                  tone={taskTone(task.state)}
                  pulse={task.state === "running"}
                />
                <span className="font-medium">{task.roleName}</span>
                <span className="ml-auto text-xs text-muted-foreground tabular-nums">
                  {task.state === "running"
                    ? formatRelativeTime(task.createdAt, i18n.language)
                    : formatDuration(task.durationMs)}
                </span>
              </span>
              <span className="line-clamp-2 text-xs text-muted-foreground">
                {task.task}
              </span>
              {task.state !== "running" && task.result ? (
                <span className="line-clamp-1 text-xs">
                  {task.state === "failed" ? (
                    <span className="text-destructive">{task.result}</span>
                  ) : (
                    task.result
                  )}
                </span>
              ) : null}
            </button>
            {task.state === "running" ? (
              <Button
                size="icon-sm"
                variant="ghost"
                aria-label={t("orch.tasks.cancel")}
                className="absolute right-2 bottom-2 text-muted-foreground transition-colors hover:text-destructive"
                onClick={() => void api.orchestrator.taskCancel(task.id)}
              >
                <SquareIcon />
              </Button>
            ) : null}
          </li>
        ))}
      </ul>
    </ScrollArea>
  )
})
