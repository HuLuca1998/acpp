import { useTranslation } from "react-i18next"

import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { formatCost, formatDuration, formatTokens } from "@/lib/format"
import type { UsageDimension, UsageGroupRow, UsageTotals } from "@/types/usage"

/** owner 能按身份分，租客那一栏对他不存在——不是灰掉，是不渲染。 */
const OWNER_DIMENSIONS: UsageDimension[] = [
  "project",
  "tenant",
  "flavor",
  "model",
  "origin",
  "session",
]
const TENANT_DIMENSIONS: UsageDimension[] = OWNER_DIMENSIONS.filter(
  (d) => d !== "tenant"
)

/** 缓存命中率：缓存读占「缓存读 + 新增输入」的比例。 */
function hitRate(t: UsageTotals): number {
  const denom = t.cacheReadTokens + t.inputTokens
  return denom === 0 ? 0 : (t.cacheReadTokens / denom) * 100
}

/**
 * 分组明细。六个维度是同一条 SQL 换 group by，所以这里也只有一张表——
 * 切维度不换页，表头与列都不变。
 *
 * 成本列带 ≈ 的是折算出来的（codex 不报费用）；一整组都没有价时显示
 * 「未计价」而不是 $0.00——零和「不知道」在账目上是两件事。
 */
export function UsageBreakdown({
  rows,
  dimension,
  onDimension,
  isOwner,
  tenantNames,
  onDrill,
}: {
  rows: UsageGroupRow[] | null
  dimension: UsageDimension
  onDimension: (d: UsageDimension) => void
  isOwner: boolean
  /** 身份 id → 名字。owner 看按身份分组时用，租客用不到。 */
  tenantNames: Map<string, string>
  /** 点某一行下钻成筛选条件。 */
  onDrill: (dimension: UsageDimension, key: string) => void
}) {
  const { t } = useTranslation()
  const dimensions = isOwner ? OWNER_DIMENSIONS : TENANT_DIMENSIONS

  const label = (key: string): string => {
    if (key === "") return t("usage.noKey")
    if (dimension === "tenant") {
      return key === "0"
        ? t("usage.ownerIdentity")
        : (tenantNames.get(key) ?? `#${key}`)
    }
    if (dimension === "session") return `#${key}`
    if (dimension === "origin") return t(`usage.origin.${originKey(key)}`)
    return key
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("usage.breakdownTitle")}</CardTitle>
        <CardDescription>{t("usage.breakdownDescription")}</CardDescription>
        <CardAction>
          <ToggleGroup
            value={[dimension]}
            onValueChange={(value) => {
              const picked = value[0] as UsageDimension | undefined
              if (picked) onDimension(picked)
            }}
            size="sm"
            variant="outline"
          >
            {dimensions.map((d) => (
              <ToggleGroupItem key={d} value={d}>
                {t(`usage.by.${d}`)}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </CardAction>
      </CardHeader>
      <CardContent>
        {!rows ? (
          <Skeleton className="h-64 w-full" />
        ) : rows.length === 0 ? (
          <p className="py-8 text-center text-sm text-muted-foreground">
            {t("usage.breakdownEmpty")}
          </p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-xs text-muted-foreground">
                  <th className="py-2 pr-3 text-left font-medium">
                    {t(`usage.by.${dimension}`)}
                  </th>
                  <th className="py-2 pr-3 text-right font-medium">
                    {t("usage.colTurns")}
                  </th>
                  <th className="py-2 pr-3 text-right font-medium">
                    {t("usage.colSessions")}
                  </th>
                  <th className="py-2 pr-3 text-right font-medium">
                    {t("usage.colTokens")}
                  </th>
                  <th className="w-24 py-2 pr-3 text-left font-medium">
                    {t("usage.colCacheHit")}
                  </th>
                  <th className="py-2 pr-3 text-right font-medium">
                    {t("usage.colOutput")}
                  </th>
                  <th className="py-2 pr-3 text-right font-medium">
                    {t("usage.colAbnormal")}
                  </th>
                  <th className="py-2 pr-3 text-right font-medium">
                    {t("usage.colDuration")}
                  </th>
                  <th className="py-2 text-right font-medium">
                    {t("usage.colCost")}
                  </th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => {
                  const d = row.totals
                  const estimated =
                    d.estimatedMicro > 0 && d.reportedMicro === 0
                  const unpriced = d.costMicro === 0 && d.unpricedTurns > 0
                  return (
                    <tr
                      key={row.key}
                      className="border-b border-border last:border-b-0 hover:bg-accent/40"
                    >
                      <td className="py-2 pr-3">
                        <button
                          type="button"
                          className="max-w-[24ch] truncate text-left font-mono text-xs hover:text-primary hover:underline"
                          onClick={() => onDrill(dimension, row.key)}
                        >
                          {label(row.key)}
                        </button>
                      </td>
                      <td className="py-2 pr-3 text-right tabular-nums">
                        {d.turns.toLocaleString()}
                      </td>
                      <td className="py-2 pr-3 text-right text-muted-foreground tabular-nums">
                        {d.sessions.toLocaleString()}
                      </td>
                      <td className="py-2 pr-3 text-right tabular-nums">
                        {formatTokens(d.totalTokens)}
                      </td>
                      <td className="py-2 pr-3">
                        <span className="flex items-center gap-2">
                          <span className="h-1.5 min-w-8 flex-1 overflow-hidden rounded-full bg-muted">
                            <span
                              className="block h-full rounded-full bg-chart-2"
                              style={{ width: `${hitRate(d)}%` }}
                            />
                          </span>
                          <span className="text-[11px] text-muted-foreground tabular-nums">
                            {hitRate(d).toFixed(1)}%
                          </span>
                        </span>
                      </td>
                      <td className="py-2 pr-3 text-right text-muted-foreground tabular-nums">
                        {formatTokens(d.outputTokens)}
                      </td>
                      <td className="py-2 pr-3 text-right tabular-nums">
                        {d.abnormalTurns > 0 || d.errorTurns > 0 ? (
                          <span
                            className={
                              d.errorTurns > 0
                                ? "text-destructive"
                                : "text-warning"
                            }
                          >
                            {d.abnormalTurns + d.errorTurns}
                          </span>
                        ) : (
                          <span className="text-muted-foreground">0</span>
                        )}
                      </td>
                      <td className="py-2 pr-3 text-right text-muted-foreground tabular-nums">
                        {formatDuration(d.durationMs)}
                      </td>
                      <td className="py-2 text-right font-medium tabular-nums">
                        {unpriced ? (
                          <span className="font-normal text-muted-foreground">
                            {t("usage.noPrice")}
                          </span>
                        ) : (
                          <>
                            {estimated && (
                              <span className="mr-0.5 font-normal text-muted-foreground">
                                ≈
                              </span>
                            )}
                            {formatCost(d.costMicro)}
                          </>
                        )}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

/** 来源的 i18n key：后端给的是 ui / ask / discord / cron，认不出就落 other。 */
function originKey(value: string): "ui" | "ask" | "discord" | "cron" | "other" {
  switch (value) {
    case "ui":
    case "ask":
    case "discord":
    case "cron":
      return value
    default:
      return "other"
  }
}
