import type { RowData, Table } from "@tanstack/react-table"

import "@/components/data-table/data-table-meta"
import { useTranslation } from "react-i18next"

import type { DataTableFeatures } from "@/components/data-table/data-table-features"
import { Hint } from "@/components/hint"
import { LOADING_CYCLE_MS } from "@/hooks/use-min-loading"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { RefreshCwIcon, Settings2Icon } from "lucide-react"

/**
 * 列表页的操作区：左边是改变数据的按钮（新建、批量操作），右边是只改
 * 「怎么看」的两个图标（刷新、列显隐）。左右分开是有意的——右边那组在
 * 每张表上都一样，固定在同一个角落，手才记得住。
 *
 * 列显隐对宽表格是真需求（数据库那张表六列，只想看项目和库的时候把地址
 * 关掉就清爽了），但它不该记在服务端——那是一个人此刻的看法，不是配置。
 */
export function DataTableToolbar<TData extends RowData>({
  table,
  actions,
  fetching = false,
  onReload,
}: {
  table: Table<DataTableFeatures, TData>
  /** 左侧：新建、批量操作等改变数据的按钮。 */
  actions?: React.ReactNode
  /** 请求中：刷新图标转圈，与进度线同一节奏。 */
  fetching?: boolean
  /** 给了才出刷新按钮。 */
  onReload?: () => void
}) {
  const { t } = useTranslation()
  const columns = table
    .getAllColumns()
    .filter((c) => c.getCanHide() && c.columnDef.meta?.label)

  if (!actions && !onReload && columns.length === 0) return null

  return (
    <div className="flex flex-wrap items-center gap-2">
      {actions}
      <div className="ml-auto flex items-center gap-1">
        {onReload ? (
          <Hint label={t("table.refresh")}>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={t("table.refresh")}
              onClick={onReload}
            >
              <RefreshCwIcon
                className={fetching ? "animate-spin" : undefined}
                style={
                  fetching
                    ? { animationDuration: `${LOADING_CYCLE_MS}ms` }
                    : undefined
                }
              />
            </Button>
          </Hint>
        ) : null}
        {columns.length > 0 ? (
          <DropdownMenu>
            <Hint label={t("table.columns")}>
              <DropdownMenuTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={t("table.columns")}
                  >
                    <Settings2Icon />
                  </Button>
                }
              />
            </Hint>
            <DropdownMenuContent align="end" className="min-w-40">
              {/* Label 必须待在 Group 里：Base UI 的 GroupLabel 要从
                  MenuGroupContext 取 id 去挂 aria-labelledby，裸放会直接抛。 */}
              <DropdownMenuGroup>
                <DropdownMenuLabel>{t("table.columns")}</DropdownMenuLabel>
                <DropdownMenuSeparator />
                {columns.map((column) => (
                  <DropdownMenuCheckboxItem
                    key={column.id}
                    checked={column.getIsVisible()}
                    onCheckedChange={(v) => column.toggleVisibility(Boolean(v))}
                  >
                    {column.columnDef.meta?.label}
                  </DropdownMenuCheckboxItem>
                ))}
              </DropdownMenuGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        ) : null}
      </div>
    </div>
  )
}
