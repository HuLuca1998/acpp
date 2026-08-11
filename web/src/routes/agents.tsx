import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"

import { api } from "@/lib/api"
import { formatDateTime, formatRelativeTime } from "@/lib/format"
import type { Agent, AgentStatus } from "@/types/acp"
import { StatusDot, type StatusTone } from "@/components/status-dot"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { BotIcon, PlusIcon } from "lucide-react"

const STATUS_TONE: Record<AgentStatus, StatusTone> = {
  connected: "success",
  idle: "muted",
  disabled: "muted",
  error: "destructive",
}

export function Agents() {
  const { t, i18n } = useTranslation()
  const [agents, setAgents] = useState<Agent[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    api.agents
      .list()
      .then((res) => {
        if (!cancelled) setAgents(res.items)
      })
      .catch((err: Error) => {
        if (!cancelled) setError(err.message)
      })
    return () => {
      cancelled = true
    }
  }, [])

  return (
    <div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6">
      <div className="px-4 lg:px-6">
        <Card>
          <CardHeader>
            <CardTitle>{t("agents.title")}</CardTitle>
            <CardDescription>{t("agents.description")}</CardDescription>
            <CardAction>
              <Button size="sm" render={<Link to="/agents/new" />}>
                <PlusIcon data-icon="inline-start" />
                {t("agents.add")}
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent>
            {error ? (
              <Empty>
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <BotIcon />
                  </EmptyMedia>
                  <EmptyTitle>{t("common.loadFailed")}</EmptyTitle>
                  <EmptyDescription>{error}</EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : agents === null ? (
              <div className="flex flex-col gap-2">
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-10 w-full" />
              </div>
            ) : agents.length === 0 ? (
              <Empty>
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <BotIcon />
                  </EmptyMedia>
                  <EmptyTitle>{t("agents.empty")}</EmptyTitle>
                  <EmptyDescription>{t("agents.emptyHint")}</EmptyDescription>
                </EmptyHeader>
                <EmptyContent>
                  <Button size="sm" render={<Link to="/agents/new" />}>
                    <PlusIcon data-icon="inline-start" />
                    {t("agents.add")}
                  </Button>
                </EmptyContent>
              </Empty>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t("agents.name")}</TableHead>
                    <TableHead>{t("agents.command")}</TableHead>
                    <TableHead>{t("agents.cwd")}</TableHead>
                    <TableHead>{t("agents.status")}</TableHead>
                    <TableHead className="text-right">
                      {t("agents.updated")}
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {agents.map((agent) => (
                    <TableRow key={agent.id} className="group relative">
                      <TableCell className="font-medium">
                        {/* 拉伸链接铺满整行：视觉上整行可点，语义仍是 <a>。 */}
                        <Link
                          to={`/agents/${agent.id}`}
                          className="after:absolute after:inset-0"
                        >
                          {agent.name}
                        </Link>
                      </TableCell>
                      <TableCell className="max-w-72 truncate font-mono text-xs text-muted-foreground">
                        {[agent.command, ...agent.args].join(" ")}
                      </TableCell>
                      <TableCell className="max-w-56 truncate font-mono text-xs text-muted-foreground">
                        {agent.cwd || t("common.none")}
                      </TableCell>
                      <TableCell>
                        <StatusDot
                          tone={STATUS_TONE[agent.status]}
                          pulse={agent.status === "connected"}
                          label={t(
                            `agents.status${capitalize(agent.status)}` as never,
                            { defaultValue: agent.status }
                          )}
                        />
                      </TableCell>
                      <TableCell
                        className="text-right text-muted-foreground tabular-nums"
                        title={formatDateTime(agent.updatedAt, i18n.language)}
                      >
                        {formatRelativeTime(agent.updatedAt, i18n.language)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

function capitalize(value: string) {
  return value.charAt(0).toUpperCase() + value.slice(1)
}
