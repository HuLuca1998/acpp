import { useTranslation } from "react-i18next"

import { formatTokens } from "@/lib/format"
import {
  quotaFlavorOf,
  usageTone,
  type SessionUsageTotals,
} from "@/lib/chat/usage"
import type { ContextUsage } from "@/hooks/use-chat"
import type { AgentFlavor, TurnUsage } from "@/types/acp"
import { cn } from "@/lib/utils"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { Separator } from "@/components/ui/separator"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { QuotaSection } from "@/components/chat/composer/quota-section"
import { UsageBar } from "@/components/chat/composer/usage-bar"

/** 货币格式化器缓存：语言 + 币种就那么几组，没必要每次重建。 */
const moneyFormatters = new Map<string, Intl.NumberFormat | null>()

/** 本地化货币；认不出的货币码退化成「数字 + 代码」。 */
function moneyOf(amount: number, currency: string, locale: string): string {
  const key = `${locale}|${currency}`
  let formatter = moneyFormatters.get(key)
  if (formatter === undefined) {
    try {
      formatter = new Intl.NumberFormat(locale, {
        style: "currency",
        currency,
      })
    } catch {
      // 认不出的币种：记成 null，下次不再试。
      formatter = null
    }
    moneyFormatters.set(key, formatter)
  }
  return formatter
    ? formatter.format(amount)
    : `${amount.toFixed(2)} ${currency}`
}

/** 面板里的一行：左标签右数值，数值等宽防跳动。 */
function Row({
  label,
  value,
  strong = false,
}: {
  label: string
  /** 数字走 k/M/B 缩写（精确值进 title），字符串原样（如货币）。 */
  value: number | string
  strong?: boolean
}) {
  const isNum = typeof value === "number"
  return (
    <div className="flex items-baseline justify-between gap-4 text-xs">
      <span className="text-muted-foreground">{label}</span>
      <span
        title={isNum ? value.toLocaleString() : undefined}
        className={cn(
          "tabular-nums",
          strong ? "font-medium text-foreground" : "text-foreground/90"
        )}
      >
        {isNum ? formatTokens(value) : value}
      </span>
    </div>
  )
}

/** 环形占用指示器：状态栏上唯一的用量入口，直径 16px。
 *  没有水位数据（刷新后 SSE 事件还没来、或会话还没开始）时只画底环，按钮
 *  照常可点——面板里还有从转录重建出来的累计数据与账号的套餐水位。 */
function UsageRing({ percent, active }: { percent: number; active: boolean }) {
  const r = 6
  const circumference = 2 * Math.PI * r
  const dash = (Math.min(100, Math.max(0, percent)) / 100) * circumference
  return (
    <svg viewBox="0 0 16 16" className="size-4 shrink-0" aria-hidden>
      <circle
        cx="8"
        cy="8"
        r={r}
        fill="none"
        strokeWidth="2.5"
        className="stroke-current opacity-25"
      />
      {active ? (
        <circle
          cx="8"
          cy="8"
          r={r}
          fill="none"
          strokeWidth="2.5"
          strokeLinecap="round"
          strokeDasharray={`${dash} ${circumference}`}
          transform="rotate(-90 8 8)"
          className={cn(
            "transition-[stroke-dasharray,stroke] duration-500 ease-fluid",
            usageTone(percent).stroke
          )}
        />
      ) : null}
    </svg>
  )
}

/**
 * 用量详情面板：点状态栏的上下文占比展开。
 *
 * 自上而下：上下文水位 → 账号的套餐限额（claude / codex 才有，向服务端取）
 * → 最近一轮明细 → 会话累计（各轮相加）→ claude 才有的累计费用。前两段说的
 * 是「还能用多少」，后三段说的是「已经用了多少」。
 */
export function UsagePopover({
  usage,
  lastUsage,
  totals,
  flavor,
  className,
}: {
  /** 上下文水位；SSE 事件态，刷新后为空——那时面板改用累计做入口。 */
  usage?: ContextUsage | null
  lastUsage?: TurnUsage | null
  totals?: SessionUsageTotals | null
  /** 会话的 agent 方言：claude / codex 才查得到套餐限额，其余不显示那一段。 */
  flavor?: AgentFlavor
  className?: string
}) {
  const { t, i18n } = useTranslation()
  const percent = usage && usage.size > 0 ? (usage.used / usage.size) * 100 : 0
  const quotaFlavor = quotaFlavorOf(flavor)

  // 悬停摘要：一行说清最要紧的——占用比例（有水位时）或会话累计，
  // 有费用就缀在后面。明细留给点开的面板。
  const summary = [
    usage
      ? t("chat.status.context", {
          used: formatTokens(usage.used),
          size: formatTokens(usage.size),
          percent: Math.round(percent),
        })
      : totals
        ? t("chat.usage.summaryTotal", {
            total: formatTokens(totals.totalTokens),
            count: totals.turns,
          })
        : t("chat.usage.open"),
    usage?.cost
      ? moneyOf(usage.cost.amount, usage.cost.currency, i18n.language)
      : null,
  ]
    .filter(Boolean)
    .join(" · ")

  return (
    <Popover>
      <Tooltip>
        <TooltipTrigger
          render={
            <PopoverTrigger
              render={
                <button
                  type="button"
                  aria-label={t("chat.usage.open")}
                  className={cn(
                    "flex shrink-0 items-center rounded-md text-muted-foreground/70 transition-colors duration-150 outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50",
                    className
                  )}
                />
              }
            />
          }
        >
          <UsageRing percent={percent} active={!!usage} />
        </TooltipTrigger>
        <TooltipContent>{summary}</TooltipContent>
      </Tooltip>
      <PopoverContent align="end" side="top" className="w-72 p-3">
        <div className="flex flex-col gap-3">
          {usage ? (
            <section className="flex flex-col gap-1.5">
              <div className="flex items-baseline justify-between gap-4">
                <span className="text-xs font-medium">
                  {t("chat.usage.context")}
                </span>
                <span className="text-xs text-muted-foreground tabular-nums">
                  {formatTokens(usage.used)} / {formatTokens(usage.size)} ·{" "}
                  {Math.round(percent)}%
                </span>
              </div>
              <UsageBar percent={percent} />
            </section>
          ) : null}

          {quotaFlavor ? (
            <>
              {usage ? <Separator /> : null}
              {/* 弹层关了就卸载，所以「打开即拉取」由它自己的挂载完成；
                  按方言加 key，换 agent 即重挂。 */}
              <QuotaSection key={quotaFlavor} flavor={quotaFlavor} />
            </>
          ) : null}

          {lastUsage ? (
            <>
              {usage || quotaFlavor ? <Separator /> : null}
              <section className="flex flex-col gap-1.5">
                <div className="text-xs font-medium">
                  {t("chat.usage.lastTurn")}
                </div>
                <Row
                  label={t("chat.usage.input")}
                  value={lastUsage.inputTokens}
                />
                <Row
                  label={t("chat.usage.output")}
                  value={lastUsage.outputTokens}
                />
                <Row
                  label={t("chat.usage.cached")}
                  value={lastUsage.cachedReadTokens}
                />
                <Row
                  label={t("chat.usage.total")}
                  value={lastUsage.totalTokens}
                  strong
                />
              </section>
            </>
          ) : null}

          {totals ? (
            <>
              <Separator />
              <section className="flex flex-col gap-1.5">
                <div className="text-xs font-medium">
                  {t("chat.usage.session", { count: totals.turns })}
                </div>
                <Row label={t("chat.usage.input")} value={totals.inputTokens} />
                <Row
                  label={t("chat.usage.output")}
                  value={totals.outputTokens}
                />
                <Row
                  label={t("chat.usage.cached")}
                  value={totals.cachedReadTokens}
                />
                <Row
                  label={t("chat.usage.total")}
                  value={totals.totalTokens}
                  strong
                />
              </section>
            </>
          ) : null}

          {usage?.cost ? (
            <>
              <Separator />
              <section className="flex flex-col gap-1">
                <Row
                  label={t("chat.usage.cost")}
                  value={moneyOf(
                    usage.cost.amount,
                    usage.cost.currency,
                    i18n.language
                  )}
                  strong
                />
                <p className="text-[11px] leading-snug text-muted-foreground/80">
                  {t("chat.usage.costNote")}
                </p>
              </section>
            </>
          ) : null}
        </div>
      </PopoverContent>
    </Popover>
  )
}
