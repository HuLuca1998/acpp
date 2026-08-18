import { useTranslation } from "react-i18next"
import { Area, AreaChart, CartesianGrid, XAxis } from "recharts"

import type { OverviewStats } from "@/types/acp"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart"
import { Skeleton } from "@/components/ui/skeleton"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"

/**
 * 活动趋势：最近 N 天的会话数与消息数。
 *
 * 数据来自后端聚合而不是前端拿列表自己算——列表是分页的，用前 50 条画出
 * 来的「两周趋势」会随每页行数变化，那是一张骗人的图。
 *
 * 用面积图而不是柱状：这两条是连续的量（每天都有值，包括 0），面积能让
 * 「有没有在用」一眼看出形状；柱状更适合离散的分类比较。
 */
export function ActivityChart({
  stats,
  days,
  onDays,
}: {
  stats: OverviewStats | null
  days: number
  onDays: (days: number) => void
}) {
  const { t, i18n } = useTranslation()

  const config = {
    sessions: {
      label: t("overview.chartSessions"),
      color: "var(--chart-1)",
    },
    messages: {
      label: t("overview.chartMessages"),
      color: "var(--chart-2)",
    },
  } satisfies ChartConfig

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("overview.chartTitle")}</CardTitle>
        <CardDescription>{t("overview.chartDescription")}</CardDescription>
        <CardAction>
          <ToggleGroup
            value={[String(days)]}
            onValueChange={(value) => {
              // 单选语义：Base UI 的 ToggleGroup 返回数组，取第一个即可；
              // 再点当前项会返回空数组，那时保持不变（不该让人点出一个
              // 「什么都没选」的状态）。
              const picked = Number(value[0])
              if (picked) onDays(picked)
            }}
            size="sm"
            variant="outline"
          >
            {[7, 14, 30].map((n) => (
              <ToggleGroupItem key={n} value={String(n)}>
                {t("overview.chartDays", { count: n })}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </CardAction>
      </CardHeader>
      <CardContent>
        {!stats ? (
          <Skeleton className="h-56 w-full" />
        ) : (
          <ChartContainer config={config} className="h-56 w-full">
            <AreaChart data={stats.daily} margin={{ left: 4, right: 4 }}>
              <defs>
                {/* 渐变填充：面积图的底色要淡到不抢线条，两条叠加时也不糊。 */}
                {(["sessions", "messages"] as const).map((key) => (
                  <linearGradient
                    key={key}
                    id={`fill-${key}`}
                    x1="0"
                    y1="0"
                    x2="0"
                    y2="1"
                  >
                    <stop
                      offset="5%"
                      stopColor={`var(--color-${key})`}
                      stopOpacity={0.7}
                    />
                    <stop
                      offset="95%"
                      stopColor={`var(--color-${key})`}
                      stopOpacity={0.05}
                    />
                  </linearGradient>
                ))}
              </defs>
              <CartesianGrid vertical={false} />
              <XAxis
                dataKey="date"
                tickLine={false}
                axisLine={false}
                tickMargin={8}
                minTickGap={24}
                tickFormatter={(value: string) =>
                  shortDate(value, i18n.language)
                }
              />
              <ChartTooltip
                content={
                  <ChartTooltipContent
                    labelFormatter={(value) =>
                      shortDate(String(value), i18n.language)
                    }
                    indicator="dot"
                  />
                }
              />
              <Area
                dataKey="sessions"
                type="natural"
                stroke="var(--color-sessions)"
                fill="url(#fill-sessions)"
                stackId="a"
              />
              <Area
                dataKey="messages"
                type="natural"
                stroke="var(--color-messages)"
                fill="url(#fill-messages)"
                stackId="b"
              />
              <ChartLegend content={<ChartLegendContent />} />
            </AreaChart>
          </ChartContainer>
        )}
      </CardContent>
    </Card>
  )
}

/** 轴与提示上的日期：只留月/日，两周的刻度才排得下。 */
function shortDate(value: string, language: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleDateString(language, { month: "short", day: "numeric" })
}
