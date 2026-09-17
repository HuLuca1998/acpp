import { useTranslation } from "react-i18next"

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { formatTokens } from "@/lib/format"
import type { UsageTotals } from "@/types/usage"

/**
 * Token 构成：四个分项的占比条 + 数值。
 *
 * 单独成卡而不是并进主图的堆叠柱：缓存读占 96%，堆在一起时另外三项加起来
 * 只有一两个像素。而**决定成本的恰恰是这个比例**——缓存读按输入的 1/10 计、
 * 缓存写按 1.25 倍、输出按 5 倍，所以这张卡真正要回答的是「钱花在哪一项」。
 */
export function UsageComposition({ totals }: { totals: UsageTotals | null }) {
  const { t } = useTranslation()

  if (!totals) {
    return <Skeleton className="h-[248px] rounded-xl" />
  }

  const total = totals.totalTokens
  // key 用字面量联合而不是 string：i18n 的类型增强要靠它把
  // `usage.part.${key}` 收成一个真实存在的 key，写错在编译期就报。
  const parts: {
    key: "cacheRead" | "cacheWrite" | "output" | "input" | "thought"
    value: number
    color: string
    rate: string
  }[] = [
    {
      key: "cacheRead",
      value: totals.cacheReadTokens,
      color: "var(--chart-2)",
      rate: t("usage.rateCacheRead"),
    },
    {
      key: "cacheWrite",
      value: totals.cacheWriteTokens,
      color: "var(--chart-1)",
      rate: t("usage.rateCacheWrite"),
    },
    {
      key: "output",
      value: totals.outputTokens,
      color: "var(--chart-3)",
      rate: t("usage.rateOutput"),
    },
    {
      key: "input",
      value: totals.inputTokens,
      color: "var(--chart-4)",
      rate: t("usage.rateInput"),
    },
    // 思考 token 只有 codex 报，全是 0 的时候不占一行。
    ...(totals.thoughtTokens > 0
      ? [
          {
            key: "thought" as const,
            value: totals.thoughtTokens,
            color: "var(--chart-5)",
            rate: "",
          },
        ]
      : []),
  ]

  const pct = (v: number) => (total === 0 ? 0 : (v / total) * 100)

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("usage.compositionTitle")}</CardTitle>
        <CardDescription>{t("usage.compositionDescription")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div
          className="flex h-2.5 gap-px overflow-hidden rounded-full"
          role="img"
          aria-label={t("usage.compositionTitle")}
        >
          {parts.map((p) => (
            <div
              key={p.key}
              style={{ width: `${pct(p.value)}%`, background: p.color }}
            />
          ))}
        </div>
        <dl className="flex flex-col">
          {parts.map((p) => (
            <div
              key={p.key}
              className="flex items-baseline justify-between border-b border-border py-2 text-xs last:border-b-0"
            >
              <dt className="flex items-center gap-2 text-muted-foreground">
                <span
                  className="size-1.5 shrink-0 rounded-full"
                  style={{ background: p.color }}
                />
                {t(`usage.part.${p.key}`)}
                {p.rate && (
                  <span className="text-[11px] opacity-70">{p.rate}</span>
                )}
              </dt>
              <dd className="font-medium tabular-nums">
                {formatTokens(p.value)}
                <span className="ml-1.5 font-normal text-muted-foreground">
                  {pct(p.value).toFixed(2)}%
                </span>
              </dd>
            </div>
          ))}
        </dl>
      </CardContent>
    </Card>
  )
}
