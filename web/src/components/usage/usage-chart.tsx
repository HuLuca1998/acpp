import { useTranslation } from "react-i18next"
import { Bar, CartesianGrid, ComposedChart, Line, XAxis, YAxis } from "recharts"

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
import { formatCost, formatTokens } from "@/lib/format"
import type { UsageBucket } from "@/types/usage"

/** 图上画什么。三条量级差得远，所以一次只画一个主角。 */
export type ChartMetric = "tokens" | "cost" | "turns"

/**
 * 每日用量曲线：柱是 token 总量，线是等价成本（右轴）。
 *
 * 两个量级差着六个数量级（几千万 token vs 几十块钱），硬画在同一根轴上
 * 线会贴着地板。所以成本走独立的右轴——它是这页最该被看见的数字，不能
 * 因为单位小就被压没。
 *
 * 柱只画 token 总量而不堆叠四个分项：缓存读占 96%，堆叠图里另外三项加起来
 * 也只有一两个像素，看起来像画错了。分项构成交给旁边那张构成条去说。
 */
export function UsageChart({
  buckets,
  metric,
  onMetric,
}: {
  buckets: UsageBucket[] | null
  metric: ChartMetric
  onMetric: (m: ChartMetric) => void
}) {
  const { t, i18n } = useTranslation()

  const config = {
    primary: {
      label: t(`usage.metric.${metric}`),
      color: metric === "cost" ? "var(--chart-1)" : "var(--chart-2)",
    },
    cost: { label: t("usage.metric.cost"), color: "var(--chart-1)" },
  } satisfies ChartConfig

  // 把合计摊平成图要的形状。成本一路是整数微元，画之前才除。
  const data = (buckets ?? []).map((b) => ({
    date: b.date,
    primary:
      metric === "tokens"
        ? b.totals.totalTokens
        : metric === "cost"
          ? b.totals.costMicro / 1_000_000
          : b.totals.turns,
    cost: b.totals.costMicro / 1_000_000,
  }))

  const formatPrimary = (v: number) =>
    metric === "tokens"
      ? formatTokens(v)
      : metric === "cost"
        ? formatCost(v * 1_000_000)
        : String(v)

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("usage.chartTitle")}</CardTitle>
        <CardDescription>{t("usage.chartDescription")}</CardDescription>
        <CardAction>
          <ToggleGroup
            value={[metric]}
            onValueChange={(value) => {
              // 单选语义：再点当前项会返回空数组，那时保持不变。
              const picked = value[0] as ChartMetric | undefined
              if (picked) onMetric(picked)
            }}
            size="sm"
            variant="outline"
          >
            {(["tokens", "cost", "turns"] as const).map((m) => (
              <ToggleGroupItem key={m} value={m}>
                {t(`usage.metric.${m}`)}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </CardAction>
      </CardHeader>
      <CardContent>
        {!buckets ? (
          <Skeleton className="h-64 w-full" />
        ) : (
          <ChartContainer config={config} className="h-64 w-full">
            <ComposedChart data={data} margin={{ left: 4, right: 4 }}>
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
              <YAxis
                yAxisId="left"
                tickLine={false}
                axisLine={false}
                width={48}
                tickFormatter={formatPrimary}
              />
              {/* 成本单独一根右轴：它的量级比 token 小六个数量级，共用一根
                  轴的话这条线会永远贴着地板。成本模式下主角就是它，不再重复。 */}
              {metric !== "cost" && (
                <YAxis
                  yAxisId="right"
                  orientation="right"
                  tickLine={false}
                  axisLine={false}
                  width={52}
                  tickFormatter={(v: number) => formatCost(v * 1_000_000)}
                />
              )}
              <ChartTooltip
                content={
                  <ChartTooltipContent
                    labelFormatter={(value) =>
                      shortDate(String(value), i18n.language)
                    }
                    // 同一张图上两个系列的单位不同：柱按当前指标、线永远是钱。
                    valueFormatter={(value, name) =>
                      name === "cost"
                        ? formatCost(Number(value) * 1_000_000)
                        : formatPrimary(Number(value))
                    }
                    indicator="dot"
                  />
                }
              />
              <Bar
                yAxisId="left"
                dataKey="primary"
                fill="var(--color-primary)"
                fillOpacity={0.45}
                radius={[4, 4, 0, 0]}
              />
              {metric !== "cost" && (
                <Line
                  yAxisId="right"
                  dataKey="cost"
                  /* 线性而不是 monotone：曲线补齐了没有活动的日子（值为 0），
                     平滑插值会在两个尖峰之间拱出一道驼峰，看起来像那几天也
                     在烧钱。折线的直上直下才是这份数据的真实形状。 */
                  type="linear"
                  stroke="var(--color-cost)"
                  strokeWidth={2}
                  dot={false}
                />
              )}
              <ChartLegend content={<ChartLegendContent />} />
            </ComposedChart>
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
