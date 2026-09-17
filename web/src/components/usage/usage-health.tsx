import { useTranslation } from "react-i18next"
import { Link } from "react-router"

import { StatusDot } from "@/components/status-dot"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { formatDateTime } from "@/lib/format"
import type { UsageErrorKind, UsageErrors, UsageTotals } from "@/types/usage"

/** 错误细分对应的状态色：过载会自己好，额度与登录要人管。 */
const KIND_TONE: Record<UsageErrorKind, "warning" | "destructive" | "muted"> = {
  overloaded: "warning",
  quota: "destructive",
  auth: "destructive",
  other: "muted",
}

/**
 * 健康面板：异常分三层摆，因为**每层的责任人不一样**。
 *
 * 轮次中止多半是人按了停止（使用习惯），工具调用失败是干活的一部分
 * （AI 自己会重试），只有中间那层 agent 报错才是要人去管的。糊成一个
 * 「错误率」的话，这个判断就做不出来了。
 */
export function UsageHealth({
  totals,
  errors,
}: {
  totals: UsageTotals | null
  errors: UsageErrors | null
}) {
  const { t, i18n } = useTranslation()

  if (!totals || !errors) {
    return <Skeleton className="h-[320px] rounded-xl" />
  }

  const abnormalRate =
    totals.turns === 0 ? 0 : (totals.abnormalTurns / totals.turns) * 100
  const errorRate =
    totals.turns === 0 ? 0 : (totals.errorTurns / totals.turns) * 100
  const toolRate =
    totals.toolCalls === 0 ? 0 : (totals.toolFailed / totals.toolCalls) * 100

  // key 用字面量联合：i18n 的类型增强靠它把 `usage.tier.${key}` 收成
  // 真实存在的 key，写错在编译期就报。
  const tiers: {
    key: "turn" | "agent" | "tool"
    value: number
    detail: string
    tone: "warning" | "destructive" | "muted"
  }[] = [
    {
      key: "turn",
      value: abnormalRate,
      detail: t("usage.tierTurnDetail", {
        abnormal: totals.abnormalTurns,
        turns: totals.turns,
      }),
      tone:
        totals.abnormalTurns > 0 ? ("warning" as const) : ("muted" as const),
    },
    {
      key: "agent",
      value: errorRate,
      detail: t("usage.tierAgentDetail", { count: totals.errorTurns }),
      tone:
        totals.errorTurns > 0 ? ("destructive" as const) : ("muted" as const),
    },
    {
      key: "tool",
      value: toolRate,
      detail: t("usage.tierToolDetail", {
        failed: totals.toolFailed,
        calls: totals.toolCalls,
      }),
      tone: "muted" as const,
    },
  ]

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("usage.healthTitle")}</CardTitle>
        <CardDescription>{t("usage.healthDescription")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        <div className="grid gap-3 @2xl/main:grid-cols-3">
          {tiers.map((tier) => (
            <div
              key={tier.key}
              className="flex flex-col gap-1 rounded-lg border border-border p-3"
            >
              <span className="text-[11px] tracking-wide text-muted-foreground">
                {t(`usage.tier.${tier.key}`)}
              </span>
              <span className="text-2xl font-semibold tabular-nums">
                {tier.value.toFixed(1)}%
              </span>
              <StatusDot tone={tier.tone} label={tier.detail} />
              <span className="text-[11px] text-muted-foreground">
                {t(`usage.tierHint.${tier.key}`)}
              </span>
            </div>
          ))}
        </div>

        {errors.recent.length === 0 ? (
          <p className="text-xs text-muted-foreground">
            {t("usage.noAgentErrors")}
          </p>
        ) : (
          <div className="flex flex-col gap-3">
            <div className="flex flex-wrap gap-2">
              {errors.classes.map((cls) => (
                <span
                  key={cls.kind}
                  className="flex items-center gap-1.5 rounded-md bg-muted px-2 py-1 text-[11px]"
                >
                  <StatusDot
                    tone={KIND_TONE[cls.kind]}
                    label={t(`usage.errorKind.${cls.kind}`)}
                  />
                  <span className="text-muted-foreground tabular-nums">
                    ×{cls.count}
                  </span>
                </span>
              ))}
            </div>
            <div className="overflow-x-auto">
              <table className="w-full text-xs">
                <thead>
                  <tr className="border-b border-border text-muted-foreground">
                    <th className="w-32 py-1.5 pr-3 text-left font-medium">
                      {t("usage.colWhen")}
                    </th>
                    <th className="w-16 py-1.5 pr-3 text-left font-medium">
                      {t("usage.colSession")}
                    </th>
                    <th className="w-20 py-1.5 pr-3 text-left font-medium">
                      {t("usage.colKind")}
                    </th>
                    <th className="py-1.5 text-left font-medium">
                      {t("usage.colMessage")}
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {errors.recent.slice(0, 8).map((ev) => (
                    <tr
                      key={`${ev.sessionId}-${ev.turnSeq}`}
                      className="border-b border-border last:border-b-0"
                    >
                      <td className="py-1.5 pr-3 text-muted-foreground tabular-nums">
                        {formatDateTime(ev.at, i18n.language)}
                      </td>
                      <td className="py-1.5 pr-3">
                        <Link
                          to={`/sessions/${ev.sessionId}`}
                          className="text-primary tabular-nums hover:underline"
                        >
                          #{ev.sessionId}
                        </Link>
                      </td>
                      <td className="py-1.5 pr-3">
                        <StatusDot
                          tone={KIND_TONE[ev.kind]}
                          label={t(`usage.errorKind.${ev.kind}`)}
                        />
                      </td>
                      <td className="py-1.5 font-mono text-[11px] text-muted-foreground">
                        <span className="line-clamp-2">{ev.message}</span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <p className="text-[11px] text-muted-foreground">
              {t("usage.errorCodeNote")}
            </p>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
