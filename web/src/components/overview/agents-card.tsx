import { useTranslation } from "react-i18next"
import { Link } from "react-router"

import { StatusDot } from "@/components/status-dot"
import { AGENT_STATUS_TONE } from "@/lib/status-tone"
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
import type { Agent } from "@/types/acp"
import { ArrowRightIcon, PlusIcon } from "lucide-react"

const AGENT_LIMIT = 4

/** Agent 状态卡：前几个 agent 的健康状况，整行可点进配置页。 */
export function AgentsCard({ agents }: { agents: Agent[] }) {
  const { t } = useTranslation()

  return (
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
              <StatusDot tone={AGENT_STATUS_TONE[agent.status]} />
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
  )
}
