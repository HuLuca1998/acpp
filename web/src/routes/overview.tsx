import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"

import { api } from "@/lib/api"
import { formatDateTime, formatRelativeTime } from "@/lib/format"
import type { Agent, Session, SessionState } from "@/types/acp"
import { StatusDot, type StatusTone } from "@/components/status-dot"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import {
  ActivityIcon,
  ArrowRightIcon,
  BotIcon,
  MessageSquareTextIcon,
  MessagesSquareIcon,
  PlusIcon,
} from "lucide-react"

const RECENT_LIMIT = 6
const AGENT_LIMIT = 4

const SESSION_TONE: Record<SessionState, StatusTone> = {
  active: "success",
  idle: "muted",
  ended: "muted",
  error: "destructive",
}

const AGENT_TONE: Record<Agent["status"], StatusTone> = {
  connected: "success",
  idle: "muted",
  disabled: "muted",
  error: "destructive",
}

export function Overview() {
  const { t, i18n } = useTranslation()
  const [agents, setAgents] = useState<Agent[] | null>(null)
  const [sessions, setSessions] = useState<Session[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    Promise.all([api.agents.list(), api.sessions.list()])
      .then(([agentRes, sessionRes]) => {
        if (cancelled) return
        setAgents(agentRes.items)
        setSessions(sessionRes.items)
      })
      .catch((err: Error) => {
        if (!cancelled) setError(err.message)
      })
    return () => {
      cancelled = true
    }
  }, [])

  if (error) {
    return (
      <PageShell>
        <div className="px-4 lg:px-6">
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <ActivityIcon />
              </EmptyMedia>
              <EmptyTitle>{t("common.loadFailed")}</EmptyTitle>
              <EmptyDescription>{error}</EmptyDescription>
            </EmptyHeader>
          </Empty>
        </div>
      </PageShell>
    )
  }

  if (agents === null || sessions === null) {
    return (
      <PageShell>
        <div className="grid grid-cols-1 gap-4 px-4 lg:px-6 @xl/main:grid-cols-2 @5xl/main:grid-cols-4">
          {Array.from({ length: 4 }, (_, i) => (
            <Skeleton key={i} className="h-28 rounded-xl" />
          ))}
        </div>
        <div className="px-4 lg:px-6">
          <Skeleton className="h-64 rounded-xl" />
        </div>
      </PageShell>
    )
  }

  // 首次使用：一个 agent 都没有时，用引导卡替代空指标，指向正确的第一步。
  if (agents.length === 0) {
    return (
      <PageShell>
        <div className="px-4 lg:px-6">
          <Card>
            <CardContent>
              <Empty>
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <BotIcon />
                  </EmptyMedia>
                  <EmptyTitle>{t("overview.onboardTitle")}</EmptyTitle>
                  <EmptyDescription>
                    {t("overview.onboardHint")}
                  </EmptyDescription>
                </EmptyHeader>
                <EmptyContent>
                  <Button render={<Link to="/agents/new" />}>
                    <PlusIcon data-icon="inline-start" />
                    {t("overview.addAgent")}
                  </Button>
                </EmptyContent>
              </Empty>
            </CardContent>
          </Card>
        </div>
      </PageShell>
    )
  }

  const connectedCount = agents.filter((a) => a.status === "connected").length
  const activeCount = sessions.filter((s) => s.state === "active").length
  const runningCount = sessions.filter((s) => s.running).length
  const messageCount = sessions.reduce((sum, s) => sum + s.messageCount, 0)
  const recent = sessions.slice(0, RECENT_LIMIT)

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
      value: sessions.length,
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
    <PageShell>
      {/* 指标卡：入场从下轻浮起 + 逐卡 45ms stagger，仅首次挂载可见。 */}
      <div className="grid grid-cols-1 gap-4 px-4 *:data-[slot=card]:bg-linear-to-t *:data-[slot=card]:from-primary/5 *:data-[slot=card]:to-card *:data-[slot=card]:shadow-xs lg:px-6 @xl/main:grid-cols-2 @5xl/main:grid-cols-4 dark:*:data-[slot=card]:bg-card">
        {stats.map((stat, i) => (
          <Card
            key={stat.key}
            className="@container/card transition-[opacity,translate] duration-300 ease-snappy starting:translate-y-2 starting:opacity-0 motion-reduce:starting:translate-y-0"
            style={{ transitionDelay: `${i * 45}ms` }}
          >
            <CardHeader>
              <CardDescription className="flex items-center gap-1.5 [&_svg]:size-3.5">
                {stat.icon}
                {stat.label}
              </CardDescription>
              <CardTitle className="text-2xl font-semibold tabular-nums @[250px]/card:text-3xl">
                {stat.value.toLocaleString()}
              </CardTitle>
            </CardHeader>
            <CardFooter className="text-xs text-muted-foreground">
              {stat.hint}
            </CardFooter>
          </Card>
        ))}
      </div>

      <div className="grid grid-cols-1 items-start gap-4 px-4 md:gap-6 lg:px-6 @4xl/main:grid-cols-[2fr_1fr]">
        {/* 最近会话 */}
        <Card>
          <CardHeader>
            <CardTitle>{t("overview.recentTitle")}</CardTitle>
            <CardDescription>{t("overview.recentDescription")}</CardDescription>
            <CardAction>
              <Button
                size="sm"
                variant="ghost"
                render={<Link to="/sessions" />}
              >
                {t("nav.viewAll")}
                <ArrowRightIcon data-icon="inline-end" />
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent>
            {recent.length === 0 ? (
              <Empty>
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <MessagesSquareIcon />
                  </EmptyMedia>
                  <EmptyTitle>{t("overview.noSessions")}</EmptyTitle>
                  <EmptyDescription>
                    {t("overview.noSessionsHint")}
                  </EmptyDescription>
                </EmptyHeader>
                <EmptyContent>
                  <Button size="sm" render={<Link to="/sessions/new" />}>
                    <PlusIcon data-icon="inline-start" />
                    {t("overview.newSession")}
                  </Button>
                </EmptyContent>
              </Empty>
            ) : (
              <div className="-mx-2 flex flex-col">
                {recent.map((session) => (
                  <Link
                    key={session.id}
                    to={`/sessions/${session.id}`}
                    className="group flex items-center gap-3 rounded-lg px-2 py-2.5 transition-colors duration-150 ease-snappy hover:bg-muted/60"
                  >
                    <StatusDot
                      tone={
                        session.running
                          ? "success"
                          : SESSION_TONE[session.state]
                      }
                      pulse={session.running}
                    />
                    <span className="flex min-w-0 flex-1 flex-col">
                      <span className="truncate text-sm font-medium">
                        {session.title ||
                          `${t("common.unnamed")} #${session.id}`}
                      </span>
                      <span className="truncate text-xs text-muted-foreground">
                        {session.agentName} ·{" "}
                        {t("overview.messagesCount", {
                          count: session.messageCount,
                        })}
                      </span>
                    </span>
                    <span
                      className="shrink-0 text-xs text-muted-foreground tabular-nums"
                      title={formatDateTime(session.updatedAt, i18n.language)}
                    >
                      {formatRelativeTime(session.updatedAt, i18n.language)}
                    </span>
                    <ArrowRightIcon className="size-3.5 shrink-0 -translate-x-1 text-muted-foreground opacity-0 transition-[opacity,translate] duration-150 ease-snappy group-hover:translate-x-0 group-hover:opacity-100" />
                  </Link>
                ))}
              </div>
            )}
          </CardContent>
        </Card>

        {/* Agent 状态 */}
        <Card>
          <CardHeader>
            <CardTitle>{t("overview.agentsTitle")}</CardTitle>
            <CardDescription>{t("overview.agentsDescription")}</CardDescription>
            <CardAction>
              <Button size="sm" variant="ghost" render={<Link to="/agents" />}>
                {t("overview.manageAgents")}
                <ArrowRightIcon data-icon="inline-end" />
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent>
            <div className="-mx-2 flex flex-col">
              {agents.slice(0, AGENT_LIMIT).map((agent) => (
                <Link
                  key={agent.id}
                  to={`/agents/${agent.id}`}
                  className="group flex items-center gap-3 rounded-lg px-2 py-2.5 transition-colors duration-150 ease-snappy hover:bg-muted/60"
                >
                  <StatusDot
                    tone={AGENT_TONE[agent.status]}
                    pulse={agent.status === "connected"}
                  />
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="truncate text-sm font-medium">
                      {agent.name}
                    </span>
                    <span className="truncate font-mono text-xs text-muted-foreground">
                      {[agent.command, ...agent.args].join(" ")}
                    </span>
                  </span>
                </Link>
              ))}
            </div>
          </CardContent>
          <CardFooter>
            <Button
              size="sm"
              variant="outline"
              className="w-full"
              render={<Link to="/agents/new" />}
            >
              <PlusIcon data-icon="inline-start" />
              {t("overview.addAgent")}
            </Button>
          </CardFooter>
        </Card>
      </div>
    </PageShell>
  )
}

function PageShell({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6">{children}</div>
  )
}
