import { useState } from "react"
import { useTable, type ColumnDef, type RowData } from "@tanstack/react-table"

import "@/components/data-table/data-table-meta"

import { cn } from "@/lib/utils"
import { DataPagination } from "@/components/data-pagination"
import { dataTableFeatures } from "@/components/data-table/data-table-features"
import { DataTableToolbar } from "@/components/data-table/data-table-toolbar"
import type {
  SortingState,
  VisibilityState,
} from "@/components/data-table/data-table-state"
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
  empty,
  toolbar,
  onPage,
  onPageSize,
  onSorting,
  onRowClick,
  rowClassName,
}: {
  columns: ColumnDef<typeof dataTableFeatures, TData, unknown>[]
  data: TData[] | null
  total: number
  page: number
  pageSize: number
  sorting: SortingState
  /**
   * 没有行可画时整个替换成它。传 ListPageStates：加载中、出错、空列表
   * 三种都归它管，各页文案不同。
   */
  empty?: React.ReactNode
  /** 工具栏左侧的自定义内容（搜索框、筛选等）。 */
  toolbar?: React.ReactNode
  onPage: (page: number) => void
  onPageSize: (size: number) => void
  onSorting: (sorting: SortingState) => void
  onRowClick?: (row: TData) => void
  rowClassName?: (row: TData) => string | undefined
}) {
  // 列显隐自己持有：它不进请求，是一个人此刻想看什么，不是配置也不是
  // 页面状态。放出去只会让六个列表页各写一遍同样的 useState。
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({})
  const table = useTable({
    features: dataTableFeatures,
    data: data ?? [],
    columns,
    state: { sorting, columnVisibility },
    onSortingChange: (updater) =>
      onSorting(typeof updater === "function" ? updater(sorting) : updater),
    onColumnVisibilityChange: setColumnVisibility,
    // 排序由服务端做，表格只负责把状态交出去。
    manualSorting: true,
  })

  // 没有行可画就整个让给三态壳：加载中的骨架、出错的说明、空列表的
  // 下一步 CTA 都在 ListPageStates 里（AGENTS.md 的硬规则），表格自己
  // 不该再造一套。
  if (data === null || data.length === 0) {
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
                <TableHead
                  key={header.id}
                  className={header.column.columnDef.meta?.className}
                >
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
                // relative 是拉伸链接（after:absolute inset-0）的落脚点，
                // 本项目所有列表行的主链接都是这个模式（AGENTS.md §5.5）。
                // transform 是给 WebKit 的兜底：WKWebView（桌面壳）不把 tr 的
                // relative 当 absolute 后代的定位基准，链接的 ::after 会相对
                // 外层容器铺满整张表、最后一行叠在最顶——表现为整表只有最后
                // 一行可点。任何非 none 的 transform 建立的定位基准两家引擎
                // 都认，对 Chrome 无行为差异。
                "group relative [transform:translate(0)]",
                onRowClick && "cursor-pointer",
                rowClassName?.(row.original)
              )}
              onClick={onRowClick ? () => onRowClick(row.original) : undefined}
            >
              {row.getVisibleCells().map((cell) => (
                <TableCell
                  key={cell.id}
                  className={cell.column.columnDef.meta?.className}
                >
                  <table.FlexRender cell={cell} />
                </TableCell>
              ))}
            </TableRow>
          ))}
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
