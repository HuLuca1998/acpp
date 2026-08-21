import { useState } from "react"
import { useTranslation } from "react-i18next"

import type { TableView } from "@/types/acp"
import { cn } from "@/lib/utils"

/**
 * 表格预览：csv/tsv 与 xlsx 共用的渲染器，照着 Excel 的样子画。
 *
 * 服务端已经把两者摊平成同一个形状（csv 是只有一页的工作簿），所以这里
 * 不关心文件是什么格式，只管画网格：顶上是列标 A/B/C，左边是行号，两条
 * 都钉住不随滚动跑掉。
 *
 * 刻意**不**把首行当表头——csv 的第一行未必是标题，可能就是数据。给它
 * 一个和别人不一样的样式等于替用户下判断，而 Excel 从不这么干：第 1 行
 * 就是第 1 行，是不是标题人一眼就看出来了。
 */
export function TablePreview({ view }: { view: TableView }) {
  const { t } = useTranslation()
  const [active, setActive] = useState(0)
  const sheet = view.sheets[active] ?? view.sheets[0]

  if (!sheet || sheet.rows.length === 0) {
    return (
      <div className="p-3 text-xs text-muted-foreground">
        {t("workspace.preview.tableEmpty")}
      </div>
    )
  }

  // 列数取全表最宽的一行：csv 尾列不齐是常态，按首行算会把后面的数据切掉。
  const columns = Math.max(...sheet.rows.map((row) => row.length))

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="min-h-0 flex-1 overflow-auto">
        <table className="w-max border-separate border-spacing-0 font-mono text-xs">
          <thead>
            <tr>
              {/* 左上角那格：行号列与列标行的交叉点，两向都钉住。 */}
              <th className="sticky top-0 left-0 z-30 border-r border-b border-border bg-muted/60 px-2 py-1" />
              {Array.from({ length: columns }, (_, c) => (
                <th
                  key={c}
                  className="sticky top-0 z-20 min-w-16 border-r border-b border-border bg-muted/60 px-2 py-1 text-center font-normal text-muted-foreground"
                >
                  {columnLabel(c)}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {sheet.rows.map((row, r) => (
              <tr key={r} className="group/row">
                <td className="sticky left-0 z-10 border-r border-b border-border bg-muted/60 px-2 py-1 text-right text-muted-foreground tabular-nums">
                  {r + 1}
                </td>
                {Array.from({ length: columns }, (_, c) => {
                  const value = row[c] ?? ""
                  return (
                    <td
                      key={c}
                      title={value}
                      className={cn(
                        "max-w-96 truncate border-r border-b border-border px-2 py-1 group-hover/row:bg-muted/30",
                        // 数字靠右——这是表格能一眼比大小的前提，Excel 也
                        // 是这么排的。
                        isNumeric(value) && "text-right tabular-nums"
                      )}
                    >
                      {value}
                    </td>
                  )
                })}
              </tr>
            ))}
          </tbody>
        </table>
        {sheet.truncated ? (
          <div className="px-3 py-2 text-xs text-muted-foreground">
            {t("workspace.preview.truncated")}
          </div>
        ) : null}
      </div>

      {/* 多工作表的切页条放在底部——Excel 的 sheet 标签就在那儿。 */}
      {view.sheets.length > 1 ? (
        <div className="flex shrink-0 gap-1 border-t border-border bg-muted/20 px-2 py-1">
          {view.sheets.map((s, i) => (
            <button
              key={s.name + i}
              type="button"
              aria-pressed={i === active}
              onClick={() => setActive(i)}
              className={cn(
                "rounded-md px-2 py-0.5 text-xs transition-colors duration-150 ease-snappy",
                i === active
                  ? "bg-background text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground"
              )}
            >
              {s.name}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  )
}

/** 列标：0→A、25→Z、26→AA，与 Excel 一致。 */
function columnLabel(index: number): string {
  let label = ""
  let n = index
  while (n >= 0) {
    label = String.fromCharCode(65 + (n % 26)) + label
    n = Math.floor(n / 26) - 1
  }
  return label
}

/** 看起来是数字就靠右：允许千分位、小数、负号、百分号与货币符号。 */
function isNumeric(value: string): boolean {
  const s = value.trim()
  if (!s) return false
  return (
    /^[-+]?[¥$€£]?\d{1,3}(,\d{3})*(\.\d+)?%?$/.test(s) ||
    /^[-+]?\d*\.?\d+([eE][-+]?\d+)?%?$/.test(s)
  )
}
