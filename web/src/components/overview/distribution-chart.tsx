import { useTranslation } from "react-i18next"
import { Cell, Pie, PieChart } from "recharts"

import type { OverviewStats } from "@/types/acp"
import {
  Card,
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

/**
 * 会话分布：按工具、按状态两个环形图。
 *
 * 用环形而不是柱状：这两组都是「整体的构成」（加起来就是全部会话），
 * 环形把「占比」直接画成了角度；柱状更适合比较绝对值。
 *
 * 只有一类时不画图——一个 100% 的圆环什么也没说明，直接给数字更诚实。
 */
export function DistributionChart({ stats }: { stats: OverviewStats | null }) {
  const { t } = useTranslation()

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("overview.distributionTitle")}</CardTitle>
        <CardDescription>
          {t("overview.distributionDescription")}
        </CardDescription>
      </CardHeader>
      <CardContent className="grid flex-1 content-center gap-4 sm:grid-cols-2">
        {!stats ? (
          <>
            <Skeleton className="h-48 w-full" />
            <Skeleton className="h-48 w-full" />
          </>
        ) : (
          <>
            <Donut title={t("overview.byAgent")} data={stats.byAgent} />
            <Donut title={t("overview.byState")} data={stats.byState} />
          </>
        )}
      </CardContent>
    </Card>
  )
}

function Donut({
  title,
  data,
}: {
  title: string
  data: { name: string; count: number }[]
}) {
  const { t } = useTranslation()

  if (data.length === 0) {
    return (
      <div className="flex h-48 flex-col items-center justify-center gap-1 text-sm text-muted-foreground">
        <span>{title}</span>
        <span className="text-xs">{t("overview.noSessions")}</span>
      </div>
    )
  }

  // 图例与配色都按名字建索引：chart 组件靠 config 的 key 找颜色与标签。
  const config: ChartConfig = Object.fromEntries(
    data.map((d, i) => [
      d.name,
      { label: d.name, color: `var(--chart-${(i % 5) + 1})` },
    ])
  )

  return (
    <div className="flex flex-col items-center gap-1">
      <span className="text-xs text-muted-foreground">{title}</span>
      <ChartContainer config={config} className="h-48 w-full">
        <PieChart>
          <ChartTooltip content={<ChartTooltipContent hideLabel />} />
          <Pie
            data={data}
            dataKey="count"
            nameKey="name"
            innerRadius={36}
            outerRadius={58}
            strokeWidth={2}
          >
            {data.map((d) => (
              <Cell key={d.name} fill={`var(--color-${d.name})`} />
            ))}
          </Pie>
          <ChartLegend content={<ChartLegendContent nameKey="name" />} />
        </PieChart>
      </ChartContainer>
    </div>
  )
}
