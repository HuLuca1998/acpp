import type { TableState } from "@tanstack/react-table"

import type { DataTableFeatures } from "@/components/data-table/data-table-features"

/**
 * 表格状态的两个切片别名。v9 的状态类型都带 features 泛型，各页写全
 * 一长串太吵——这里取好，页面只 import 名字。
 */
export type SortingState = NonNullable<TableState<DataTableFeatures>["sorting"]>
export type VisibilityState = NonNullable<
  TableState<DataTableFeatures>["columnVisibility"]
>
