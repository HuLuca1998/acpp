import { memo } from "react"
import { useTranslation } from "react-i18next"
import type { IDockviewPanelProps } from "dockview-react"

import { ChatStream } from "@/components/chat/chat-stream"
import { useOrchCtx } from "@/components/orchestrator/orch-context"
import { StatusDot } from "@/components/status-dot"
import { useTaskChat } from "@/hooks/use-task-chat"
import { cn } from "@/lib/utils"

/**
 * 任务子会话面板：从任务列表拖出/点开，只读观察子代理的完整工作过程
 * （权限/提问仍可裁决）。关闭面板不影响任务运行。
 */
export const OrchTaskPanel = memo(function OrchTaskPanel(
  props: IDockviewPanelProps
) {
  const { t } = useTranslation()
  const taskId = (props.params as { taskId?: number })?.taskId ?? 0
  const { chat: orchChat } = useOrchCtx()
  const chat = useTaskChat(taskId)

  const task = orchChat.tasks.find((item) => item.id === taskId)

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <div className="flex items-center gap-2 border-b border-border/60 px-3 py-2 text-xs text-muted-foreground">
        <StatusDot
          tone={
            task?.state === "running"
              ? "success"
              : task?.state === "failed"
                ? "destructive"
                : "muted"
          }
          pulse={task?.state === "running"}
        />
        <span className="font-medium text-foreground">{task?.roleName}</span>
        <span
          className={cn(
            "truncate",
            task?.state === "failed" && "text-destructive"
          )}
        >
          {task
            ? t(`orch.tasks.state.${task.state}` as never)
            : t("orch.tasks.gone")}
        </span>
        {task?.tokensUsed ? (
          <span className="ml-auto shrink-0 tabular-nums">
            {task.tokensUsed.toLocaleString()} tokens
          </span>
        ) : null}
      </div>
      <div className="min-h-0 flex-1 overflow-hidden">
        <ChatStream chat={chat} />
      </div>
    </div>
  )
})
