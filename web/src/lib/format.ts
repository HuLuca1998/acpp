/** 时间显示的纯函数。列表与概览统一用相对时间，悬停 title 再给完整时间。 */

const UNITS: { unit: Intl.RelativeTimeFormatUnit; ms: number }[] = [
  { unit: "year", ms: 365 * 24 * 60 * 60 * 1000 },
  { unit: "month", ms: 30 * 24 * 60 * 60 * 1000 },
  { unit: "week", ms: 7 * 24 * 60 * 60 * 1000 },
  { unit: "day", ms: 24 * 60 * 60 * 1000 },
  { unit: "hour", ms: 60 * 60 * 1000 },
  { unit: "minute", ms: 60 * 1000 },
]

/** "3 分钟前 / 昨天"。一分钟内显示"刚刚"（由 numeric:auto 的 second=0 给出）。 */
export function formatRelativeTime(iso: string, locale: string): string {
  const diff = new Date(iso).getTime() - Date.now()
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" })
  for (const { unit, ms } of UNITS) {
    if (Math.abs(diff) >= ms) {
      return rtf.format(Math.round(diff / ms), unit)
    }
  }
  return rtf.format(0, "second")
}

/** 完整本地时间，用在 title 悬停提示或详情处。 */
export function formatDateTime(iso: string, locale: string): string {
  return new Date(iso).toLocaleString(locale, {
    dateStyle: "medium",
    timeStyle: "short",
  })
}

/** token 数缩写："32.2k"、"1.0M"；千以下原样。精确值放 title 里。 */
export function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`
  return String(n)
}

/** 首字母大写：动态拼 i18n key（"idle" → "statusIdle"）用。 */
export function capitalize(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1)
}
