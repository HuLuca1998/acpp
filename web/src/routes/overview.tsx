import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"

import { ActivityChart } from "@/components/overview/activity-chart"
import { DistributionChart } from "@/components/overview/distribution-chart"
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
import type { Agent, OverviewStats, Session, SkillUsage } from "@/types/acp"
import { ActivityIcon, BotIcon } from "lucide-react"

const RECENT_LIMIT = 6

export function Overview() {
  const { t } = useTranslation()
  const [agents, setAgents] = useState<Agent[] | null>(null)
  const [sessions, setSessions] = useState<Session[] | null>(null)
  const [skillUsage, setSkillUsage] = useState<SkillUsage[]>([])
  const [stats, setStats] = useState<OverviewStats | null>(null)
  const [days, setDays] = useState(14)
  const [error, setError] = useState<string | null>(null)

  // 趋势数据随天数窗口重取。它与主体分开加载：图表慢一点不该拖住指标卡。
  useEffect(() => {
    let cancelled = false
    api.sessions
      .overview(days)
      .then((res) => {
        if (!cancelled) setStats(res)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [days])

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
      <StatCards agents={agents} sessions={sessions} overview={stats} />
      <div className="px-4 lg:px-6">
        <ActivityChart stats={stats} days={days} onDays={setDays} />
      </div>
      {/* 不用 items-start：那会让两张卡各按自身高度长，底边永远参差。
          默认 stretch 才能对齐，卡内的图表再各自撑满。 */}
      <div className="grid grid-cols-1 gap-4 px-4 md:gap-6 lg:px-6 @4xl/main:grid-cols-2">
        <DistributionChart stats={stats} />
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
