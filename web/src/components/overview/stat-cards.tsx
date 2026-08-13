import { useTranslation } from "react-i18next"

import { StatusDot } from "@/components/status-dot"
import {
  Card,
  CardAction,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import type { Agent, Session } from "@/types/acp"
import {
  ActivityIcon,
  BotIcon,
  MessageSquareTextIcon,
  MessagesSquareIcon,
} from "lucide-react"

/** 概览指标卡：agent 数 / 会话数 / 运行中 / 消息数。 */
export function StatCards({
  agents,
  sessions,
  sessionTotal,
}: {
  agents: Agent[]
  sessions: Session[]
  sessionTotal: number
}) {
  const { t } = useTranslation()

  // 「已连接」的真实来源是活着的会话进程：按 agentId 去重计数。
  const connectedCount = new Set(
    sessions.filter((s) => s.running).map((s) => s.agentId)
  ).size
  const activeCount = sessions.filter((s) => s.state === "active").length
  const runningCount = sessions.filter((s) => s.running).length
  const messageCount = sessions.reduce((sum, s) => sum + s.messageCount, 0)

  const stats: {
    key: string
    icon: React.ReactNode
    label: string
    value: number
    hint: React.ReactNode
  }[] = [
    {
      key: "agents",
      icon: <BotIcon />,
      label: t("overview.statAgents"),
      value: agents.length,
      hint:
        connectedCount > 0 ? (
          <StatusDot
            tone="success"
            label={t("overview.connectedCount", { count: connectedCount })}
          />
        ) : (
          <StatusDot tone="muted" label={t("overview.noneConnected")} />
        ),
    },
    {
      key: "sessions",
      icon: <MessagesSquareIcon />,
      label: t("overview.statSessions"),
      value: sessionTotal,
      hint:
        activeCount > 0 ? (
          <StatusDot
            tone="success"
            label={t("overview.activeCount", { count: activeCount })}
          />
        ) : (
          <span>{t("overview.noneActive")}</span>
        ),
    },
    {
      key: "running",
      icon: <ActivityIcon />,
      label: t("overview.statRunning"),
      value: runningCount,
      hint: <span>{t("overview.runningHint")}</span>,
    },
    {
      key: "messages",
      icon: <MessageSquareTextIcon />,
      label: t("overview.statMessages"),
      value: messageCount,
      hint: <span>{t("overview.messagesHint")}</span>,
    },
  ]

  return (
    // 指标卡：入场从下轻浮起 + 逐卡 45ms stagger，仅首次挂载可见。
    <div className="grid grid-cols-1 gap-4 px-4 *:data-[slot=card]:bg-linear-to-t *:data-[slot=card]:from-primary/5 *:data-[slot=card]:to-card lg:px-6 @xl/main:grid-cols-2 @5xl/main:grid-cols-4">
      {stats.map((stat, i) => (
        <Card
          key={stat.key}
          className="@container/card transition-[opacity,translate] duration-300 ease-snappy starting:translate-y-2 starting:opacity-0 motion-reduce:starting:translate-y-0"
          style={{ transitionDelay: `${i * 45}ms` }}
        >
          <CardHeader>
            <CardDescription>{stat.label}</CardDescription>
            <CardTitle className="text-3xl font-semibold tracking-tight tabular-nums">
              {stat.value.toLocaleString()}
            </CardTitle>
            <CardAction>
              {/* 图标芯片：主色轻染的圆角方块，给指标卡一个视觉锚点。 */}
              <span className="flex size-9 items-center justify-center rounded-lg bg-primary/10 text-primary [&_svg]:size-4">
                {stat.icon}
              </span>
            </CardAction>
          </CardHeader>
          <CardFooter className="text-xs text-muted-foreground">
            {stat.hint}
          </CardFooter>
        </Card>
      ))}
    </div>
  )
}
