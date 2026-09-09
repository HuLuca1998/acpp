import { useTranslation } from "react-i18next"

/**
 * 列表页的页头：标题 + 「共 N 条」，一行。
 *
 * 列表页四区（搜索 / 操作 / 表格 / 翻页，见 DataTable）之上就这一行——
 * 新建、筛选这些按钮不放这里，它们属于操作区和搜索区，六个列表页才会
 * 长得一样。条数跟着标题走而不是跟着分页条：翻到第 3 页时「一共多少条」
 * 仍然是这一页最先想知道的事。
 */
export function ListPageHeader({
  title,
  total,
}: {
  title: string
  /** 给了才显示条数——还在加载时不该显示一个 0。 */
  total?: number
}) {
  const { t } = useTranslation()
  return (
    <div className="flex flex-wrap items-baseline gap-3">
      <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
      {total !== undefined ? (
        // tabular-nums：条数变化时标题不会跟着抖
        <span className="text-xs text-muted-foreground tabular-nums">
          {t("table.total", { total })}
        </span>
      ) : null}
    </div>
  )
}
