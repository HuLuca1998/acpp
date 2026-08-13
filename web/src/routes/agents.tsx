import { useTranslation } from "react-i18next"
import { Link } from "react-router"

import { ListPageStates } from "@/components/list-page-states"
import { StatusDot } from "@/components/status-dot"
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { useAsyncData } from "@/hooks/use-async-data"
import { api } from "@/lib/api"
import { capitalize, formatDateTime, formatRelativeTime } from "@/lib/format"
import { AGENT_STATUS_TONE } from "@/lib/status-tone"
import { BotIcon, PlusIcon } from "lucide-react"

export function Agents() {
  const { t, i18n } = useTranslation()
  const { data: agents, error } = useAsyncData(
    () => api.agents.list().then((res) => res.items),
    []
  )

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
            {error || agents === null || agents.length === 0 ? (
              <ListPageStates
                icon={<BotIcon />}
                error={error}
                loading={agents === null}
                emptyTitle={t("agents.empty")}
                emptyHint={t("agents.emptyHint")}
                emptyAction={
                  <Button size="sm" render={<Link to="/agents/new" />}>
                    <PlusIcon data-icon="inline-start" />
                    {t("agents.add")}
                  </Button>
                }
              />
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
                          tone={AGENT_STATUS_TONE[agent.status]}
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

