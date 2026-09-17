import { useTranslation } from "react-i18next"

import { Hint } from "@/components/hint"
import { StatusDot } from "@/components/status-dot"
import {
  Card,
  CardAction,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { formatCost, formatDuration, formatTokens } from "@/lib/format"
import type { UsageSummary, UsageTotals } from "@/types/usage"
import {
  CircleDollarSignIcon,
  DatabaseZapIcon,
  MessagesSquareIcon,
  TriangleAlertIcon,
  TypeIcon,
} from "lucide-react"

/** 缓存命中率：缓存读占「缓存读 + 新增输入」的比例。 */
function cacheHitRate(t: UsageTotals): number {
  const denom = t.cacheReadTokens + t.inputTokens
  return denom === 0 ? 0 : (t.cacheReadTokens / denom) * 100
}

/** 环比：没有上期或上期为 0 时不给百分比——除以 0 得出的箭头是假的。 */
function delta(current: number, previous: number | undefined): number | null {
  if (previous === undefined || previous === 0) return null
  return ((current - previous) / previous) * 100
}

/**
 * 报表顶部的五张卡。
 *
 * 成本放第一张并用主色：它是这页唯一一个「要不要管」的数字，其余四张都是
 * 解释它的。缓存命中单独占一张不是凑数——实测缓存读占全部 token 的 96%，
 * 而它的单价只有普通输入的 1/10，命中率掉几个点就是几百块的事。
 */
export function UsageKpis({ summary }: { summary: UsageSummary | null }) {
  const { t } = useTranslation()

  if (!summary) {
    return (
      <div className="grid grid-cols-1 gap-4 @xl/main:grid-cols-2 @5xl/main:grid-cols-5">
        {Array.from({ length: 5 }, (_, i) => (
          <Skeleton key={i} className="h-[124px] rounded-xl" />
        ))}
      </div>
    )
  }

  const cur = summary.totals
  const prev = summary.previous
  const hit = cacheHitRate(cur)
  const abnormalRate =
    cur.turns === 0 ? 0 : (cur.abnormalTurns / cur.turns) * 100
  const toolFailRate =
    cur.toolCalls === 0 ? 0 : (cur.toolFailed / cur.toolCalls) * 100

  const cards: {
    key: string
    icon: React.ReactNode
    label: string
    value: string
    /** 卡底那一行：说清这个数字是怎么来的。 */
    foot: React.ReactNode
    lead?: boolean
    trend?: number | null
  }[] = [
    {
      key: "cost",
      icon: <CircleDollarSignIcon />,
      label: t("usage.kpiCost"),
      value: formatCost(cur.costMicro),
      lead: true,
      trend: delta(cur.costMicro, prev?.costMicro),
      foot: (
        <Hint
          label={t("usage.costSourceHint")}
          desc={t("usage.costSourceDesc")}
        >
          <span className="tabular-nums">
            {t("usage.costSplit", {
              reported: formatCost(cur.reportedMicro),
              estimated: formatCost(cur.estimatedMicro),
            })}
            {cur.unpricedTurns > 0 && (
              <span className="ml-1.5">
                {t("usage.unpriced", { count: cur.unpricedTurns })}
              </span>
            )}
          </span>
        </Hint>
      ),
    },
    {
      key: "tokens",
      icon: <TypeIcon />,
      label: t("usage.kpiTokens"),
      value: formatTokens(cur.totalTokens),
      trend: delta(cur.totalTokens, prev?.totalTokens),
      foot: (
        <span className="tabular-nums">
          {t("usage.tokensFoot", {
            cached: formatTokens(cur.cacheReadTokens),
            output: formatTokens(cur.outputTokens),
          })}
        </span>
      ),
    },
    {
      key: "turns",
      icon: <MessagesSquareIcon />,
      label: t("usage.kpiTurns"),
      value: `${cur.turns.toLocaleString()} / ${cur.sessions.toLocaleString()}`,
      trend: delta(cur.turns, prev?.turns),
      foot: (
        <span className="tabular-nums">
          {t("usage.turnsFoot", { duration: formatDuration(cur.durationMs) })}
        </span>
      ),
    },
    {
      key: "cache",
      icon: <DatabaseZapIcon />,
      label: t("usage.kpiCacheHit"),
      value: `${hit.toFixed(1)}%`,
      foot: <span>{t("usage.cacheFoot")}</span>,
    },
    {
      key: "abnormal",
      icon: <TriangleAlertIcon />,
      label: t("usage.kpiAbnormal"),
      value: `${abnormalRate.toFixed(1)}%`,
      foot: (
        <span className="flex items-center gap-1.5 tabular-nums">
          <StatusDot
            tone={
              cur.errorTurns > 0
                ? "destructive"
                : cur.abnormalTurns > 0
                  ? "warning"
                  : "muted"
            }
            label={t("usage.abnormalFoot", {
              turns: cur.abnormalTurns,
              errors: cur.errorTurns,
              tools: toolFailRate.toFixed(1),
            })}
          />
        </span>
      ),
    },
  ]

  return (
    <div className="grid grid-cols-1 gap-4 @xl/main:grid-cols-2 @5xl/main:grid-cols-5">
      {cards.map((card, i) => (
        <Card
          key={card.key}
          data-lead={card.lead ? "" : undefined}
          className="@container/card transition-[opacity,translate] duration-300 ease-snappy data-lead:border-primary/35 starting:translate-y-2 starting:opacity-0 motion-reduce:starting:translate-y-0"
          style={{ transitionDelay: `${i * 45}ms` }}
        >
          <CardHeader>
            <CardDescription>{card.label}</CardDescription>
            <CardTitle
              className={`text-3xl font-semibold tracking-tight tabular-nums ${
                card.lead ? "text-primary" : ""
              }`}
            >
              {card.value}
            </CardTitle>
            <CardAction>
              <span className="flex size-9 items-center justify-center rounded-lg bg-primary/10 text-primary [&_svg]:size-4">
                {card.icon}
              </span>
            </CardAction>
          </CardHeader>
          <CardFooter className="flex-col items-start gap-1 text-xs text-muted-foreground">
            {card.foot}
            {card.trend !== null && card.trend !== undefined && (
              <span className="tabular-nums">
                {t(card.trend >= 0 ? "usage.trendUp" : "usage.trendDown", {
                  pct: Math.abs(card.trend).toFixed(0),
                })}
              </span>
            )}
          </CardFooter>
        </Card>
      ))}
    </div>
  )
}
