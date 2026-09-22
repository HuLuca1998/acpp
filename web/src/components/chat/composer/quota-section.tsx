import { useTranslation } from "react-i18next"
import { RefreshCwIcon } from "lucide-react"

import { Hint } from "@/components/hint"
import { UsageBar } from "@/components/chat/composer/usage-bar"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { usePlanQuota } from "@/hooks/use-plan-quota"
import type { QuotaFlavor } from "@/lib/chat/usage"
import { capitalize, formatCountdown, formatRelativeTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { PlanQuota, QuotaWindow } from "@/types/system"

/** 5 小时窗的秒数；codex 只给秒数，与它相差不多的都按「5 小时限额」叫。 */
const FIVE_HOURS = 5 * 3600

/**
 * 套餐用量：这个方言的账号在订阅上还剩多少额度——5 小时窗、每周窗、按模型
 * 的周窗各一行，右侧是重置倒计时与已用比例。数据来自后端向服务端取的快照
 *（不是本地账本），挂载即拉、一分钟内复用，右上角可手动重取。
 */
export function QuotaSection({ flavor }: { flavor: QuotaFlavor }) {
  const { t, i18n } = useTranslation()
  const { quota, error, refreshing, refresh } = usePlanQuota(flavor)

  const label = (w: QuotaWindow): string => {
    const hours = w.windowSeconds ? Math.round(w.windowSeconds / 3600) : 0
    switch (w.kind) {
      case "session":
        // codex 报的是秒数：与 5 小时差得远的就照实说几小时。
        return w.windowSeconds && Math.abs(w.windowSeconds - FIVE_HOURS) > 600
          ? t("chat.quota.window", { hours })
          : t("chat.quota.fiveHour")
      case "weekly":
        return t("chat.quota.weekly")
      case "weekly_model":
        return t("chat.quota.weeklyModel", { model: w.model ?? "" })
      default:
        return hours ? t("chat.quota.window", { hours }) : capitalize(w.kind)
    }
  }

  const resetsIn = (iso?: string): string | null => {
    if (!iso) return null
    const ms = new Date(iso).getTime() - Date.now()
    return ms > 0
      ? t("chat.quota.resetsIn", { time: formatCountdown(ms) })
      : null
  }

  return (
    <section className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs font-medium">
          {t("chat.quota.title")}
          {quota?.plan ? (
            <span className="font-normal text-muted-foreground">
              {" · "}
              {capitalize(quota.plan)}
            </span>
          ) : null}
        </span>
        <Hint label={t("chat.quota.refresh")}>
          <button
            type="button"
            aria-label={t("chat.quota.refresh")}
            disabled={refreshing}
            onClick={() => void refresh()}
            className="flex size-5 items-center justify-center rounded-md text-muted-foreground/70 transition-colors duration-150 outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 disabled:opacity-60"
          >
            {refreshing ? (
              <Spinner className="size-3" />
            ) : (
              <RefreshCwIcon className="size-3" />
            )}
          </button>
        </Hint>
      </div>

      {quota ? (
        <QuotaBody
          quota={quota}
          label={label}
          resetsIn={resetsIn}
          cli={flavor}
        />
      ) : error ? (
        <p className="text-[11px] leading-snug text-muted-foreground">
          {t("chat.quota.error", { error })}
        </p>
      ) : (
        // 首次拉取：形状贴近真实内容——两行「标签 + 条」。
        <div className="flex flex-col gap-2" aria-busy>
          {[0, 1].map((i) => (
            <div key={i} className="flex flex-col gap-1.5">
              <div className="flex justify-between">
                <Skeleton className="h-3 w-24" />
                <Skeleton className="h-3 w-16" />
              </div>
              <Skeleton className="h-1 w-full" />
            </div>
          ))}
        </div>
      )}

      {quota ? (
        <p className="text-[11px] leading-snug text-muted-foreground/70">
          {t("chat.quota.updated", {
            time: formatRelativeTime(quota.fetchedAt, i18n.language),
          })}
        </p>
      ) : null}
    </section>
  )
}

function QuotaBody({
  quota,
  label,
  resetsIn,
  cli,
}: {
  quota: PlanQuota
  label: (w: QuotaWindow) => string
  resetsIn: (iso?: string) => string | null
  /** 方言名就是命令名（claude / codex），引导文案里直接用。 */
  cli: QuotaFlavor
}) {
  const { t } = useTranslation()

  if (quota.status !== "ok") {
    const text =
      quota.status === "expired"
        ? t("chat.quota.expired", { cli })
        : quota.status === "not_logged_in"
          ? t("chat.quota.notLoggedIn", { cli })
          : quota.status === "unavailable"
            ? t("chat.quota.unavailable")
            : t("chat.quota.error", { error: quota.error ?? "" })
    return (
      <p className="text-[11px] leading-snug text-muted-foreground">{text}</p>
    )
  }

  return (
    <div className="flex flex-col gap-2">
      {quota.windows.length === 0 ? (
        <p className="text-[11px] leading-snug text-muted-foreground">
          {t("chat.quota.noWindows")}
        </p>
      ) : null}
      {quota.windows.map((w, i) => (
        <QuotaRow
          key={`${w.kind}:${w.model ?? ""}:${i}`}
          label={label(w)}
          percent={w.percent}
          resets={resetsIn(w.resetsAt)}
          active={w.active}
        />
      ))}
      {quota.extra ? (
        <QuotaRow
          label={t("chat.quota.extra")}
          percent={quota.extra.percent}
          resets={null}
        />
      ) : null}
      {quota.credits ? (
        <p className="text-[11px] leading-snug text-muted-foreground tabular-nums">
          {quota.credits.unlimited
            ? t("chat.quota.creditsUnlimited")
            : t("chat.quota.credits", { balance: quota.credits.balance })}
        </p>
      ) : null}
      {quota.provider ? (
        <p className="text-[11px] leading-snug text-muted-foreground">
          {t("chat.quota.providerNote", { provider: quota.provider })}
        </p>
      ) : null}
    </div>
  )
}

/** 一个限额窗口：左标签，右「倒计时 · 百分比」，下面一根条。 */
function QuotaRow({
  label,
  percent,
  resets,
  active,
}: {
  label: string
  percent: number
  resets: string | null
  /** 当前真正卡着的窗口标签用正文色，其余压成次要色。 */
  active?: boolean
}) {
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-baseline justify-between gap-3 text-xs">
        <span
          className={cn(
            "truncate",
            active ? "text-foreground" : "text-muted-foreground"
          )}
        >
          {label}
        </span>
        <span className="flex shrink-0 items-baseline gap-2 tabular-nums">
          {resets ? (
            <span className="text-[11px] text-muted-foreground/70">
              {resets}
            </span>
          ) : null}
          <span className="font-medium text-foreground">
            {Math.round(percent)}%
          </span>
        </span>
      </div>
      <UsageBar percent={percent} />
    </div>
  )
}
