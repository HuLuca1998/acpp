import { useTranslation } from "react-i18next"

import { formatDateTime, formatRelativeTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { McpToolStat } from "@/types/acp"

/** 按工具聚合的统计。次数降序，一眼看出哪个工具真被用起来了。 */
export function CallStats({ stats }: { stats: McpToolStat[] }) {
  const { t, i18n } = useTranslation()
  return (
    <div className="overflow-hidden rounded-lg border">
      <table className="w-full text-sm">
        <thead className="bg-muted/50 text-xs text-muted-foreground">
          <tr>
            <th className="px-3 py-2 text-left font-medium">
              {t("tools.calls.tool")}
            </th>
            <th className="px-3 py-2 text-right font-medium">
              {t("tools.calls.count")}
            </th>
            <th className="px-3 py-2 text-right font-medium">
              {t("tools.calls.errors")}
            </th>
            <th className="px-3 py-2 text-right font-medium">
              {t("tools.calls.avgMs")}
            </th>
            <th className="px-3 py-2 text-right font-medium">
              {t("tools.calls.lastUsed")}
            </th>
          </tr>
        </thead>
        <tbody className="divide-y">
          {stats.map((stat) => (
            <tr key={`${stat.server}/${stat.tool}`}>
              <td className="px-3 py-1.5 font-mono text-xs">{stat.tool}</td>
              <td className="px-3 py-1.5 text-right tabular-nums">
                {stat.count}
              </td>
              <td
                className={cn(
                  "px-3 py-1.5 text-right tabular-nums",
                  stat.errorCount > 0 && "text-warning"
                )}
              >
                {stat.errorCount}
              </td>
              <td className="px-3 py-1.5 text-right tabular-nums">
                {stat.avgMs}
              </td>
              <td
                className="px-3 py-1.5 text-right text-xs text-muted-foreground tabular-nums"
                title={formatDateTime(stat.lastUsedAt, i18n.language)}
              >
                {formatRelativeTime(stat.lastUsedAt, i18n.language)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
