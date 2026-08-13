import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"

import { AgentsCard } from "@/components/overview/agents-card"
import { RecentSessionsCard } from "@/components/overview/recent-sessions-card"
import { SkillUsageCard } from "@/components/overview/skill-usage-card"
import { StatCards } from "@/components/overview/stat-cards"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { api } from "@/lib/api"
import type { Agent, Session, SkillUsage } from "@/types/acp"
import { ActivityIcon, BotIcon } from "lucide-react"

const RECENT_LIMIT = 6

export function Overview() {
  const { t } = useTranslation()
  const [agents, setAgents] = useState<Agent[] | null>(null)
  const [sessions, setSessions] = useState<Session[] | null>(null)
  const [sessionTotal, setSessionTotal] = useState(0)
  const [skillUsage, setSkillUsage] = useState<SkillUsage[]>([])
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    // 概览只需要最近几条 + 总数，别把全部会话拉下来。
    Promise.all([
      api.agents.list(),
      api.sessions.list({ pageSize: RECENT_LIMIT }),
    ])
      .then(([agentRes, sessionRes]) => {
        if (cancelled) return
        setAgents(agentRes.items)
        setSessions(sessionRes.items)
        setSessionTotal(sessionRes.total)
      })
      .catch((err: Error) => {
        if (!cancelled) setError(err.message)
      })
    // 技能使用统计独立取，失败不拖累概览主体。
    api.skills
      .usage()
      .then((res) => {
        if (!cancelled) setSkillUsage(res.items)
      })
      .catch(() => {})
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
                  <Button render={<Link to="/settings?section=claude" />}>
                    {t("overview.configureTools")}
                  </Button>
                </EmptyContent>
              </Empty>
            </CardContent>
          </Card>
        </div>
      </PageShell>
    )
  }

  return (
    <PageShell>
      <StatCards
        agents={agents}
        sessions={sessions}
        sessionTotal={sessionTotal}
      />
      <div className="grid grid-cols-1 items-start gap-4 px-4 md:gap-6 lg:px-6 @4xl/main:grid-cols-[2fr_1fr]">
        <RecentSessionsCard sessions={sessions.slice(0, RECENT_LIMIT)} />
        <AgentsCard agents={agents} />
      </div>
      <div className="px-4 lg:px-6">
        <SkillUsageCard usage={skillUsage} />
      </div>
    </PageShell>
  )
}

function PageShell({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6">{children}</div>
  )
}
