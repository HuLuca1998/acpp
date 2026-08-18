import type { Column, RowData } from "@tanstack/react-table"

import { cn } from "@/lib/utils"
import type { DataTableFeatures } from "@/components/data-table/data-table-features"
import { Button } from "@/components/ui/button"
import {
  ArrowDownIcon,
  ArrowUpIcon,
  ChevronsUpDownIcon,
} from "lucide-react"

/**
 * 可排序的表头单元。不可排序的列直接给文字——一个点了没反应的按钮
 * 比纯文字更让人困惑。
 *
 * 三态循环：无序 → 升 → 降 → 无序。回到无序是有意义的一档（退回后端的
 * 默认排序，比如会话按更新时间倒序），不该让人只能在升降之间来回切。
 */
export function DataTableHeader<TData extends RowData>({
  column,
  title,
  className,
}: {
  column: Column<DataTableFeatures, TData, unknown>
  title: string
  className?: string
}) {
  if (!column.getCanSort()) {
    return <span className={className}>{title}</span>
  }

  const sorted = column.getIsSorted()
  const Icon =
    sorted === "asc"
      ? ArrowUpIcon
      : sorted === "desc"
        ? ArrowDownIcon
        : ChevronsUpDownIcon

  return (
    <Button
      variant="ghost"
      size="sm"
      className={cn("-ml-2 h-7 px-2 font-medium", className)}
      onClick={() => {
        if (sorted === "asc") column.toggleSorting(true)
        else if (sorted === "desc") column.clearSorting()
        else column.toggleSorting(false)
      }}
    >
      {title}
      <Icon
        className={cn("size-3.5", !sorted && "text-muted-foreground/50")}
        data-icon="inline-end"
      />
    </Button>
  )
}
