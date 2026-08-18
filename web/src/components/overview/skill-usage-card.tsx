import { useTranslation } from "react-i18next"
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts"
import { Link } from "react-router"

import { Button } from "@/components/ui/button"
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
} from "@/components/ui/chart"
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
import type { SkillUsage } from "@/types/acp"
import { ArrowRightIcon, PuzzleIcon } from "lucide-react"

/** 技能使用统计卡：被 AI 调用最多的技能，次数条相对最高值。 */
export function SkillUsageCard({ usage }: { usage: SkillUsage[] }) {
  const { t } = useTranslation()

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("overview.skillUsageTitle")}</CardTitle>
        <CardDescription>{t("overview.skillUsageDescription")}</CardDescription>
        <CardAction>
          <Button size="sm" variant="ghost" render={<Link to="/skills" />}>
            {t("nav.viewAll")}
            <ArrowRightIcon data-icon="inline-end" />
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-1 flex-col justify-center">
        {usage.length === 0 ? (
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <PuzzleIcon />
              </EmptyMedia>
              <EmptyTitle>{t("overview.skillUsageEmpty")}</EmptyTitle>
              <EmptyDescription>
                {t("overview.skillUsageEmptyHint")}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <ChartContainer
            config={{
              count: {
                label: t("overview.skillUsageCount"),
                color: "var(--chart-1)",
              },
            }}
            className="h-48 w-full"
          >
            {/* 横向柱状：技能名长短不一，竖着放标签会挤成斜的。 */}
            <BarChart
              data={usage}
              layout="vertical"
              margin={{ left: 4, right: 16 }}
            >
              <CartesianGrid horizontal={false} />
              <XAxis type="number" dataKey="count" hide />
              <YAxis
                type="category"
                dataKey="name"
                tickLine={false}
                axisLine={false}
                width={140}
                tick={{ fontSize: 12 }}
              />
              <ChartTooltip content={<ChartTooltipContent hideLabel />} />
              <Bar
                dataKey="count"
                fill="var(--color-count)"
                radius={4}
                barSize={20}
              />
            </BarChart>
          </ChartContainer>
        )}
      </CardContent>
    </Card>
  )
}
