import {
  columnVisibilityFeature,
  rowSortingFeature,
  tableFeatures,
} from "@tanstack/react-table"

/**
 * 表格启用的能力。v9 要求显式声明——没声明的 feature 会被 tree-shake 掉，
 * 打包体积只算用到的那几个。
 *
 * 这里只开排序与列显隐：
 *   - 分页由服务端做（LIMIT/OFFSET），不需要 rowPaginationFeature 在客户端切片；
 *   - 筛选同理，列表页真要筛选得走后端查询，客户端筛只能筛当前这一页。
 * 以后要行选择（批量删除）再加 rowSelectionFeature。
 */
export const dataTableFeatures = tableFeatures({
  columnVisibilityFeature,
  rowSortingFeature,
})

export type DataTableFeatures = typeof dataTableFeatures
