import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { api } from "@/lib/api"
import type { UsageQuery } from "@/types/usage"
import { CircleDollarSignIcon, InfoIcon, RefreshCwIcon } from "lucide-react"

/** 时间范围的取值。 */
export type UsageRange = "7d" | "14d" | "30d" | "90d" | "all"

const RANGES: UsageRange[] = ["7d", "14d", "30d", "90d", "all"]

/**
 * 页头：标题 + 时间范围 + 口径说明（+ owner 的重算按钮）。
 *
 * 口径说明常驻而不是收进问号：这一页的金额是**等价成本**，不是账单——
 * 两条 runtime 走的都是订阅登录。这句话藏起来，数字就会被当成真花了多少钱。
 */
export function UsageFilters({
  range,
  onRange,
  filters,
  onFilters,
  isOwner,
  onEditPrices,
  onBackfilled,
}: {
  range: UsageRange
  onRange: (r: UsageRange) => void
  filters: UsageQuery
  onFilters: (f: UsageQuery) => void
  isOwner: boolean
  onEditPrices: () => void
  onBackfilled: () => void
}) {
  const { t } = useTranslation()
  const [backfilling, setBackfilling] = useState(false)

  const runBackfill = () => {
    setBackfilling(true)
    api.usage
      .backfill()
      .then((res) => {
        toast.success(
          t("usage.backfillDone", {
            sessions: res.sessions,
            turns: res.turns,
            seconds: (res.elapsedMs / 1000).toFixed(1),
          })
        )
        onBackfilled()
      })
      .catch((err: Error) => toast.error(err.message))
      .finally(() => setBackfilling(false))
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-end gap-3">
        <div className="flex-1">
          <h1 className="text-xl font-semibold tracking-tight">
            {t("usage.title")}
          </h1>
          <p className="mt-1 text-xs text-muted-foreground">
            {isOwner ? t("usage.subtitleOwner") : t("usage.subtitleTenant")}
          </p>
        </div>
        <ToggleGroup
          value={[range]}
          onValueChange={(value) => {
            // 单选语义：再点当前项会返回空数组，那时保持不变。
            const picked = value[0] as UsageRange | undefined
            if (picked) onRange(picked)
          }}
          size="sm"
          variant="outline"
        >
          {RANGES.map((r) => (
            <ToggleGroupItem key={r} value={r}>
              {t(`usage.range.${r}`)}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
        {isOwner && (
          <Button variant="outline" size="sm" onClick={onEditPrices}>
            <CircleDollarSignIcon data-icon="inline-start" />
            {t("usage.prices.edit")}
          </Button>
        )}
        {isOwner && (
          <Button
            variant="outline"
            size="sm"
            onClick={runBackfill}
            disabled={backfilling}
          >
            <RefreshCwIcon
              data-icon="inline-start"
              className={backfilling ? "animate-spin" : undefined}
            />
            {t("usage.backfill")}
          </Button>
        )}
      </div>

      <Alert>
        <InfoIcon />
        <AlertDescription>
          <strong className="font-medium text-foreground">
            {t("usage.costBasisLead")}
          </strong>{" "}
          {t("usage.costBasis")}
        </AlertDescription>
      </Alert>

      {/* 维度筛选先留一个「清空」出口：下钻面板（按项目/身份点进来）会把
          值写进 filters，这里要有办法退回全量。 */}
      {hasFilters(filters) && (
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <span>{t("usage.filtered", { desc: describe(filters) })}</span>
          <Button variant="ghost" size="sm" onClick={() => onFilters({})}>
            {t("usage.clearFilters")}
          </Button>
        </div>
      )}
    </div>
  )
}

function hasFilters(f: UsageQuery): boolean {
  return Boolean(f.tenant ?? f.flavor ?? f.project ?? f.origin ?? f.model)
}

function describe(f: UsageQuery): string {
  return [f.tenant, f.flavor, f.project, f.origin, f.model]
    .filter(Boolean)
    .join(" · ")
}
