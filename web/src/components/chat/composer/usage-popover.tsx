import { useTranslation } from "react-i18next"

import { formatTokens } from "@/lib/format"
import type { SessionUsageTotals } from "@/lib/chat/usage"
import type { ContextUsage } from "@/hooks/use-chat"
import type { TurnUsage } from "@/types/acp"
import { cn } from "@/lib/utils"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Separator } from "@/components/ui/separator"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"

/**
 * 占用色阶：越满越显眼。低位用品牌色（安静的存在感），过半转注意色，
 * 逼近上限转危险色——那时用户真该考虑开新会话或压缩上下文了。
 */
function usageTone(percent: number): { stroke: string; fill: string } {
  if (percent >= 85) return { stroke: "stroke-destructive", fill: "bg-destructive" }
  if (percent >= 60) return { stroke: "stroke-warning", fill: "bg-warning" }
  return { stroke: "stroke-primary", fill: "bg-primary" }
}

/** 本地化货币；认不出的货币码退化成「数字 + 代码」。 */
function moneyOf(amount: number, currency: string, locale: string): string {
  try {
    return new Intl.NumberFormat(locale, {
      style: "currency",
      currency,
    }).format(amount)
  } catch {
    return `${amount.toFixed(2)} ${currency}`
  }
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

/**
 * 上下文占用条。手写而不是装 shadcn 的 progress：这里要的是一根两像素的
 * 装饰线，progress 组件的 root+indicator 两层结构与 aria 语义在这儿都用不上
 * ——占比数字就在旁边，读屏用户从文字拿到的信息比进度条更准。
 * 接近占满时转 warning 色，那是唯一需要被看见的时刻。
 */
function ContextBar({ percent }: { percent: number }) {
  return (
    <div className="h-1 w-full overflow-hidden rounded-full bg-muted">
      <div
        className={cn(
          "h-full rounded-full transition-[width,background-color] duration-300 ease-fluid",
          usageTone(percent).fill
        )}
        style={{ width: `${Math.min(100, Math.max(2, percent))}%` }}
      />
    </div>
  )
}

/** 环形占用指示器：状态栏上唯一的用量入口，直径 16px。
 *  没有水位数据（刷新后 SSE 事件还没来）时只画底环，按钮照常可点——
 *  面板里还有从转录重建出来的累计数据。 */
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
 * 只呈现协议真给的数据——上下文水位、最近一轮明细、会话累计（各轮相加）、
 * 以及 claude 才有的累计费用。额度/重置时间那类 ACP 没有，不臆造。
 */
export function UsagePopover({
  usage,
  lastUsage,
  totals,
  className,
}: {
  /** 上下文水位；SSE 事件态，刷新后为空——那时面板改用累计做入口。 */
  usage?: ContextUsage | null
  lastUsage?: TurnUsage | null
  totals?: SessionUsageTotals | null
  className?: string
}) {
  const { t, i18n } = useTranslation()
  const percent = usage && usage.size > 0 ? (usage.used / usage.size) * 100 : 0

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
    usage?.cost ? moneyOf(usage.cost.amount, usage.cost.currency, i18n.language) : null,
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
              <span className="text-xs tabular-nums text-muted-foreground">
                {formatTokens(usage.used)} / {formatTokens(usage.size)} ·{" "}
                {Math.round(percent)}%
              </span>
            </div>
            <ContextBar percent={percent} />
          </section>
          ) : null}

          {lastUsage ? (
            <>
              {usage ? <Separator /> : null}
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
                <Row
                  label={t("chat.usage.input")}
                  value={totals.inputTokens}
                />
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
                  value={moneyOf(usage.cost.amount, usage.cost.currency, i18n.language)}
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
