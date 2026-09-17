import { useMemo, useState } from "react"
import { useTranslation } from "react-i18next"

import { UsageChart, type ChartMetric } from "@/components/usage/usage-chart"
import { UsageComposition } from "@/components/usage/usage-composition"
import { UsageFilters, type UsageRange } from "@/components/usage/usage-filters"
import { UsageKpis } from "@/components/usage/usage-kpis"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { useAsyncData } from "@/hooks/use-async-data"
import { useIsOwner } from "@/hooks/identity-context"
import { api } from "@/lib/api"
import type { UsageQuery } from "@/types/usage"
import { ChartColumnIcon } from "lucide-react"

/** 时间范围的天数；"all" 是全量（后端认 from=0）。 */
const RANGE_DAYS: Record<Exclude<UsageRange, "all">, number> = {
  "7d": 7,
  "14d": 14,
  "30d": 30,
  "90d": 90,
}

/** 把范围选择翻成后端认的 from/to（本地时区的日界）。 */
function rangeQuery(range: UsageRange): Pick<UsageQuery, "from" | "to"> {
  if (range === "all") return { from: "0" }
  const days = RANGE_DAYS[range]
  const end = new Date()
  const start = new Date(end)
  start.setDate(start.getDate() - (days - 1))
  return { from: isoDay(start), to: isoDay(end) }
}

function isoDay(d: Date): string {
  // 不用 toISOString：那是 UTC，会把「今天」在东八区提前一天切掉。
  const m = `${d.getMonth() + 1}`.padStart(2, "0")
  const day = `${d.getDate()}`.padStart(2, "0")
  return `${d.getFullYear()}-${m}-${day}`
}

/**
 * 用量页。owner 看全部身份的合计，租客只看得到自己的——**同一个页面**，
 * 范围由后端按身份收（前端连筛选器都不渲染，不是灰掉）。
 */
export function Usage() {
  const { t } = useTranslation()
  const isOwner = useIsOwner()

  const [range, setRange] = useState<UsageRange>("14d")
  const [metric, setMetric] = useState<ChartMetric>("tokens")
  const [filters, setFilters] = useState<UsageQuery>({})

  const query = useMemo<UsageQuery>(
    () => ({ ...filters, ...rangeQuery(range) }),
    [filters, range]
  )
  // 依赖用序列化后的 query：对象每次渲染都是新的引用，直接进依赖数组会
  // 让 effect 每帧重跑。
  const key = JSON.stringify(query)

  const summary = useAsyncData(() => api.usage.summary(query), [key])
  const series = useAsyncData(
    () => api.usage.series({ ...query, bucket: "day" }),
    [key]
  )

  const error = summary.error ?? series.error
  if (error) {
    return (
      <PageShell>
        <div className="px-4 lg:px-6">
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <ChartColumnIcon />
              </EmptyMedia>
              <EmptyTitle>{t("common.loadFailed")}</EmptyTitle>
              <EmptyDescription>{error}</EmptyDescription>
            </EmptyHeader>
          </Empty>
        </div>
      </PageShell>
    )
  }

  const totals = summary.data?.totals ?? null
  const empty = totals !== null && totals.turns === 0

  return (
    <PageShell>
      <div className="px-4 lg:px-6">
        <UsageFilters
          range={range}
          onRange={setRange}
          filters={filters}
          onFilters={setFilters}
          isOwner={isOwner}
          onBackfilled={() => {
            summary.reload()
            series.reload()
          }}
        />
      </div>

      {empty ? (
        <div className="px-4 lg:px-6">
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <ChartColumnIcon />
              </EmptyMedia>
              <EmptyTitle>{t("usage.emptyTitle")}</EmptyTitle>
              <EmptyDescription>
                {isOwner ? t("usage.emptyOwnerHint") : t("usage.emptyHint")}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        </div>
      ) : (
        <>
          <div className="px-4 lg:px-6">
            <UsageKpis summary={summary.data} />
          </div>
          <div className="grid gap-4 px-4 lg:px-6 @4xl/main:grid-cols-[1.6fr_1fr]">
            <UsageChart
              buckets={series.data?.items ?? null}
              metric={metric}
              onMetric={setMetric}
            />
            <UsageComposition totals={totals} />
          </div>
        </>
      )}
    </PageShell>
  )
}

function PageShell({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6">{children}</div>
  )
}
