import { useTranslation } from "react-i18next"

import { cn } from "@/lib/utils"
import {
  Pagination,
  PaginationContent,
  PaginationEllipsis,
  PaginationItem,
  PaginationLink,
  PaginationNext,
  PaginationPrevious,
} from "@/components/ui/pagination"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"

/**
 * 列表页共用的分页条：每页行数 + 当前区间/总数 + 页码导航，靠右下角。
 *
 * 换成真分页而不是「加载更多」：翻到第几页、一共几页、总共多少条，这三个
 * 问题「加载更多」一个都答不上——它只会让人一直点到底。
 *
 * 只有一页时整条不渲染：一页装得下的列表，页码栏是纯噪音。
 */

/** 可选的每页行数。20 起步——再少翻页比看内容还费劲。 */
const PAGE_SIZES = [20, 50, 100] as const

/** 页码按钮最多显示几个（不含首尾与省略号）。 */
const WINDOW = 5

export function DataPagination({
  total,
  page,
  pageSize,
  onPage,
  onPageSize,
  className,
}: {
  total: number
  page: number
  pageSize: number
  onPage: (page: number) => void
  onPageSize: (size: number) => void
  className?: string
}) {
  const { t } = useTranslation()
  const pages = Math.max(Math.ceil(total / pageSize), 1)
  if (total === 0) return null

  const from = (page - 1) * pageSize + 1
  const to = Math.min(page * pageSize, total)

  return (
    <div
      className={cn(
        "flex flex-wrap items-center justify-end gap-x-4 gap-y-2 pt-3",
        className
      )}
    >
      <span className="text-xs text-muted-foreground tabular-nums">
        {t("pagination.range", { from, to, total })}
      </span>

      <div className="flex items-center gap-1.5">
        <span className="text-xs text-muted-foreground">
          {t("pagination.perPage")}
        </span>
        <Select
          value={String(pageSize)}
          onValueChange={(v) => {
            // 改每页行数后回到第一页：留在第 7 页但总共只剩 3 页很怪。
            onPageSize(Number(v))
            onPage(1)
          }}
        >
          <SelectTrigger size="sm" className="w-[4.5rem]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {PAGE_SIZES.map((size) => (
              <SelectItem key={size} value={String(size)}>
                {size}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {pages > 1 ? (
        <Pagination className="mx-0 w-auto">
          <PaginationContent>
            <PaginationItem>
              <PaginationPrevious
                text={t("pagination.previous")}
                aria-disabled={page <= 1}
                className={cn(page <= 1 && "pointer-events-none opacity-40")}
                onClick={() => onPage(page - 1)}
              />
            </PaginationItem>
            {pageNumbers(page, pages).map((n, i) =>
              n === null ? (
                <PaginationItem key={`gap-${i}`}>
                  <PaginationEllipsis />
                </PaginationItem>
              ) : (
                <PaginationItem key={n}>
                  <PaginationLink
                    isActive={n === page}
                    onClick={() => onPage(n)}
                  >
                    {n}
                  </PaginationLink>
                </PaginationItem>
              )
            )}
            <PaginationItem>
              <PaginationNext
                text={t("pagination.next")}
                aria-disabled={page >= pages}
                className={cn(
                  page >= pages && "pointer-events-none opacity-40"
                )}
                onClick={() => onPage(page + 1)}
              />
            </PaginationItem>
          </PaginationContent>
        </Pagination>
      ) : null}
    </div>
  )
}

/**
 * 页码序列：首页、尾页固定露出，当前页周围留一段窗口，中间用 null 表示
 * 省略号。页数少时全列——1..7 全摆出来比让人猜省心。
 */
function pageNumbers(page: number, pages: number): (number | null)[] {
  if (pages <= WINDOW + 2) {
    return Array.from({ length: pages }, (_, i) => i + 1)
  }

  const half = Math.floor(WINDOW / 2)
  let start = Math.max(page - half, 2)
  const end = Math.min(start + WINDOW - 1, pages - 1)
  start = Math.max(Math.min(start, end - WINDOW + 1), 2)

  const out: (number | null)[] = [1]
  if (start > 2) out.push(null)
  for (let n = start; n <= end; n++) out.push(n)
  if (end < pages - 1) out.push(null)
  out.push(pages)
  return out
}
