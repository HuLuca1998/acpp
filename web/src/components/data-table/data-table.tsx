import { useTable, type ColumnDef, type RowData } from "@tanstack/react-table"
import { useTranslation } from "react-i18next"

import { cn } from "@/lib/utils"
import { DataPagination } from "@/components/data-pagination"
import { dataTableFeatures } from "@/components/data-table/data-table-features"
import { DataTableToolbar } from "@/components/data-table/data-table-toolbar"
import type { SortingState, VisibilityState } from "@/components/data-table/data-table-state"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

/**
 * 列表页共用的数据表：表头排序、列显隐、分页，全部对接**服务端**。
 *
 * 排序走服务端是必须的：客户端排序在分页列表上是错的——它只会把当前这
 * 一页的 20 条重排一遍，用户以为看到的是「全部里最大的」，其实是「这
 * 20 条里最大的」。那种错比没有排序更坏，所以 manualSorting 与服务端
 * 分页一起用，状态回传给调用方去请求下一页。
 *
 * 行的交互（点开、右键、拉伸链接）由各页通过 onRowClick / rowClassName
 * 决定——那是每张表自己的语义，不该由通用组件规定。
 */
export function DataTable<TData extends RowData>({
  columns,
  data,
  total,
  page,
  pageSize,
  sorting,
  columnVisibility,
  empty,
  toolbar,
  onPage,
  onPageSize,
  onSorting,
  onColumnVisibility,
  onRowClick,
  rowClassName,
}: {
  columns: ColumnDef<typeof dataTableFeatures, TData, unknown>[]
  data: TData[] | null
  total: number
  page: number
  pageSize: number
  sorting: SortingState
  columnVisibility: VisibilityState
  /** 无数据时显示的内容（三态壳由调用方给，各页文案不同）。 */
  empty?: React.ReactNode
  /** 工具栏左侧的自定义内容（搜索框、筛选等）。 */
  toolbar?: React.ReactNode
  onPage: (page: number) => void
  onPageSize: (size: number) => void
  onSorting: (sorting: SortingState) => void
  onColumnVisibility: (visibility: VisibilityState) => void
  onRowClick?: (row: TData) => void
  rowClassName?: (row: TData) => string | undefined
}) {
  const { t } = useTranslation()
  const table = useTable({
    features: dataTableFeatures,
    data: data ?? [],
    columns,
    state: { sorting, columnVisibility },
    onSortingChange: (updater) =>
      onSorting(typeof updater === "function" ? updater(sorting) : updater),
    onColumnVisibilityChange: (updater) =>
      onColumnVisibility(
        typeof updater === "function" ? updater(columnVisibility) : updater
      ),
    // 排序由服务端做，表格只负责把状态交出去。
    manualSorting: true,
  })

  if (data !== null && data.length === 0) {
    return <>{empty}</>
  }

  return (
    <div className="flex flex-col gap-3">
      <DataTableToolbar table={table} extra={toolbar} />

      <Table>
        <TableHeader>
          {table.getHeaderGroups().map((group) => (
            <TableRow key={group.id}>
              {group.headers.map((header) => (
                <TableHead key={header.id}>
                  {header.isPlaceholder ? null : (
                    <table.FlexRender header={header} />
                  )}
                </TableHead>
              ))}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {table.getRowModel().rows.map((row) => (
            <TableRow
              key={row.id}
              className={cn(
                "group",
                onRowClick && "cursor-pointer",
                rowClassName?.(row.original)
              )}
              onClick={onRowClick ? () => onRowClick(row.original) : undefined}
            >
              {row.getVisibleCells().map((cell) => (
                <TableCell key={cell.id}>
                  <table.FlexRender cell={cell} />
                </TableCell>
              ))}
            </TableRow>
          ))}
          {data === null ? (
            <TableRow>
              <TableCell
                colSpan={columns.length}
                className="h-20 text-center text-sm text-muted-foreground"
              >
                {t("common.loadFailed")}
              </TableCell>
            </TableRow>
          ) : null}
        </TableBody>
      </Table>

      <DataPagination
        total={total}
        page={page}
        pageSize={pageSize}
        onPage={onPage}
        onPageSize={onPageSize}
        className="pt-0"
      />
    </div>
  )
}
