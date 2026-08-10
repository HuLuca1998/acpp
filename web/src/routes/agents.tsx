import { useEffect, useState } from "react"
import { Link } from "react-router"

import { api } from "@/lib/api"
import type { Agent, AgentStatus } from "@/types/acp"
import { Badge } from "@/components/ui/badge"
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

const STATUS_VARIANT: Record<
  AgentStatus,
  "default" | "secondary" | "outline" | "destructive"
> = {
  connected: "default",
  idle: "secondary",
  disabled: "outline",
  error: "destructive",
}

export function Agents() {
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
            <CardTitle>Agents</CardTitle>
            <CardDescription>
              已注册的 ACP agent，通过 stdio 启动并完成 initialize 握手。
            </CardDescription>
            <CardAction>
              <Button size="sm" render={<Link to="/agents/new" />}>
                <PlusIcon data-icon="inline-start" />
                Add agent
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
                  <EmptyTitle>加载失败</EmptyTitle>
                  <EmptyDescription>{error}</EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : agents === null ? (
              <div className="flex flex-col gap-2">
                <Skeleton className="h-9 w-full" />
                <Skeleton className="h-9 w-full" />
                <Skeleton className="h-9 w-full" />
              </div>
            ) : agents.length === 0 ? (
              <Empty>
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <BotIcon />
                  </EmptyMedia>
                  <EmptyTitle>还没有 agent</EmptyTitle>
                  <EmptyDescription>
                    添加一个可执行命令（如 claude-code-acp）来接入第一个 agent。
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Name</TableHead>
                    <TableHead>Command</TableHead>
                    <TableHead>Working dir</TableHead>
                    <TableHead>Status</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {agents.map((agent) => (
                    <TableRow key={agent.id}>
                      <TableCell className="font-medium">
                        <Link to={`/agents/${agent.id}`}>{agent.name}</Link>
                      </TableCell>
                      <TableCell className="font-mono text-xs">
                        {[agent.command, ...agent.args].join(" ")}
                      </TableCell>
                      <TableCell className="text-muted-foreground">
                        {agent.cwd || "—"}
                      </TableCell>
                      <TableCell>
                        <Badge variant={STATUS_VARIANT[agent.status]}>
                          {agent.status}
                        </Badge>
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
