import type { RowData, Table } from "@tanstack/react-table"

import "@/components/data-table/data-table-meta"
import { useTranslation } from "react-i18next"

import type { DataTableFeatures } from "@/components/data-table/data-table-features"
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
import { Settings2Icon } from "lucide-react"

/**
 * 表格工具栏：左边留给各页自己的搜索/筛选，右边是列显隐。
 *
 * 列显隐对宽表格是真需求（数据库那张表六列，只想看项目和库的时候把地址
 * 关掉就清爽了），但它不该记在服务端——那是一个人此刻的看法，不是配置。
 */
export function DataTableToolbar<TData extends RowData>({
  table,
  extra,
}: {
  table: Table<DataTableFeatures, TData>
  extra?: React.ReactNode
}) {
  const { t } = useTranslation()
  const columns = table
    .getAllColumns()
    .filter((c) => c.getCanHide() && c.columnDef.meta?.label)

  return (
    <div className="flex items-center gap-2">
      {extra}
      {columns.length > 0 ? (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button variant="outline" size="sm" className="ml-auto">
                <Settings2Icon data-icon="inline-start" />
                {t("table.columns")}
              </Button>
            }
          />
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
  )
}
