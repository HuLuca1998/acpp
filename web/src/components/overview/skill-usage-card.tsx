import { useTranslation } from "react-i18next"
import { Link } from "react-router"

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
import type { SkillUsage } from "@/types/acp"
import { ArrowRightIcon, PuzzleIcon } from "lucide-react"

/** 技能使用统计卡：被 AI 调用最多的技能，次数条相对最高值。 */
export function SkillUsageCard({ usage }: { usage: SkillUsage[] }) {
  const { t } = useTranslation()
  const max = Math.max(1, ...usage.map((s) => s.count))

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
      <CardContent>
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
          <div className="flex flex-col gap-2">
            {usage.map((s) => (
              <Link
                key={s.name}
                to={`/skills/${s.name}`}
                className="group flex items-center gap-3 rounded-lg px-2 py-1.5 transition-colors duration-150 ease-snappy hover:bg-muted/60"
              >
                <span className="w-40 shrink-0 truncate font-mono text-xs">
                  {s.name}
                </span>
                {/* 次数条：相对最高值的占比，纯视觉参照。 */}
                <span className="flex h-2 flex-1 overflow-hidden rounded-full bg-muted">
                  <span
                    className="h-full rounded-full bg-primary/70"
                    style={{ width: `${(s.count / max) * 100}%` }}
                  />
                </span>
                <span className="w-10 shrink-0 text-right text-sm tabular-nums">
                  {s.count.toLocaleString()}
                </span>
              </Link>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
