import { useTranslation } from "react-i18next"

import type { SqlStatementResult } from "@/types/acp"
import { cn } from "@/lib/utils"
import { Badge } from "@/components/ui/badge"
import { AlertCircleIcon, DatabaseIcon } from "lucide-react"

/**
 * 一次 SQL 执行的结果视图：语句 + 字段 + 可滚动的数据。
 *
 * 对话里的 AI 查询与配置页的 SQL 控制台共用同一个组件——同一份数据
 * 在两处长得不一样，是最容易让人读错的那种不一致。
 *
 * 表格自己滚（横向纵向都是），不把外层撑开：结果动辄几十列几百行，
 * 让它顶开对话流会毁掉整页的阅读节奏。
 */

/** 结果表格的最大高度：一屏能看十几行，再多就该在表格内部滚。 */
const TABLE_MAX_HEIGHT = "max-h-80"

export function SqlResultView({
  results,
  className,
}: {
  results: SqlStatementResult[]
  className?: string
}) {
  return (
    <div className={cn("flex flex-col gap-3", className)}>
      {results.map((result, index) => (
        <StatementResult key={index} index={index} result={result} />
      ))}
    </div>
  )
}

function StatementResult({
  index,
  result,
}: {
  index: number
  result: SqlStatementResult
}) {
  const { t } = useTranslation()
  const failed = Boolean(result.error)

  return (
    <div
      className={cn(
        "overflow-hidden rounded-lg border",
        failed ? "border-destructive/40" : "border-border"
      )}
    >
      {/* 语句行：等宽、可换行——SQL 被截断成一行看不出跑了什么。 */}
      <div className="flex items-start gap-1.5 border-b border-border bg-muted/50 px-2.5 py-1.5">
        <span className="mt-0.5 shrink-0 font-mono text-xs text-muted-foreground tabular-nums">
          {index + 1}
        </span>
        <code className="min-w-0 flex-1 font-mono text-xs leading-5 break-all whitespace-pre-wrap">
          {result.statement}
        </code>
        <StatementBadge result={result} />
      </div>

      {failed ? (
        <div className="flex items-start gap-1.5 px-2.5 py-2 text-xs text-destructive">
          <AlertCircleIcon className="mt-0.5 size-3.5 shrink-0" />
          <span className="min-w-0 break-all whitespace-pre-wrap">
            {result.error}
          </span>
        </div>
      ) : result.kind === "query" ? (
        <ResultTable result={result} />
      ) : (
        <div className="px-2.5 py-2 text-xs text-muted-foreground tabular-nums">
          {t("db.affectedRows", { count: result.affected ?? 0 })}
          {result.lastInsertId
            ? ` · ${t("db.lastInsertId", { id: result.lastInsertId })}`
            : null}
        </div>
      )}
    </div>
  )
}

function StatementBadge({ result }: { result: SqlStatementResult }) {
  const { t } = useTranslation()
  if (result.error) {
    return (
      <Badge variant="destructive" className="shrink-0">
        {t("db.failed")}
      </Badge>
    )
  }
  return (
    <span className="shrink-0 font-mono text-xs text-muted-foreground tabular-nums">
      {result.elapsedMs}ms
    </span>
  )
}

function ResultTable({ result }: { result: SqlStatementResult }) {
  const { t } = useTranslation()

  if (!result.columns || result.columns.length === 0 || result.rowCount === 0) {
    return (
      <div className="flex items-center gap-1.5 px-2.5 py-2 text-xs text-muted-foreground">
        <DatabaseIcon className="size-3.5" />
        {t("db.noRows")}
      </div>
    )
  }

  return (
    <>
      <div className={cn("overflow-auto", TABLE_MAX_HEIGHT)}>
        <table className="w-full border-collapse text-xs">
          {/* 表头吸顶：往下翻几十行还知道每列是什么。 */}
          <thead className="sticky top-0 z-10 bg-muted">
            <tr>
              {result.columns.map((col) => (
                <th
                  key={col}
                  scope="col"
                  className="border-b border-border px-2.5 py-1.5 text-left font-medium whitespace-nowrap"
                >
                  {col}
                </th>
              ))}
            </tr>
          </thead>
          <tbody className="font-mono">
            {(result.rows ?? []).map((row, ri) => (
              <tr key={ri} className="border-b border-border/50 last:border-0">
                {row.map((cell, ci) => (
                  <td
                    key={ci}
                    className={cn(
                      "max-w-80 truncate px-2.5 py-1 align-top tabular-nums",
                      cell === null && "text-muted-foreground italic"
                    )}
                    // 单元格截断显示，完整值给到悬停——宽表格里一个长
                    // json 字段就能把其他列全挤没。
                    title={cell === null ? "NULL" : String(cell)}
                  >
                    {cell === null ? "NULL" : String(cell)}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="border-t border-border px-2.5 py-1 text-xs text-muted-foreground tabular-nums">
        {t("db.rowCount", { count: result.rowCount })}
        {result.truncated ? ` · ${t("db.truncated")}` : null}
      </div>
    </>
  )
}
