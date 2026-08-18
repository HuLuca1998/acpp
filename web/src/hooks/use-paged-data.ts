import { useCallback, useState } from "react"

import { useAsyncData } from "@/hooks/use-async-data"
import type { Paged } from "@/types/acp"

/**
 * 分页列表的标准接线：page/pageSize 状态 + 按它们拉数据 + 就地增删改。
 *
 * 每个列表页各写一遍这段逻辑必然会长歪——有的忘了改每页行数要回第一页，
 * 有的删到最后一条留在空页上。收在一处，全部列表页的行为就是同一套。
 *
 * 标识默认取 `id`，技能这类用名字当主键的传 keyOf 覆盖。
 */
export function usePagedData<T>(
  fetcher: (params: { page: number; pageSize: number }) => Promise<Paged<T>>,
  options?: {
    pageSize?: number
    keyOf?: (item: T) => string | number
  }
) {
  const keyOf = options?.keyOf ?? ((item: T) => (item as { id: number }).id)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(options?.pageSize ?? 20)
  const { data, error, setData, setError } = useAsyncData(
    () => fetcher({ page, pageSize }),
    [page, pageSize]
  )

  /** 就地替换一条（编辑保存后刷新那一行，不重拉整页）。 */
  const replace = useCallback(
    (item: T) => {
      setData((prev) => {
        if (!prev) return prev
        const at = prev.items.findIndex((i) => keyOf(i) === keyOf(item))
        // 新建的落在当前页尾：翻页重拉时它会归位到真正的位置。
        if (at < 0) {
          return { ...prev, items: [...prev.items, item], total: prev.total + 1 }
        }
        return { ...prev, items: prev.items.with(at, item) }
      })
    },
    // keyOf 是调用方的内联箭头，稳定性由本 hook 的契约保证（同 useAsyncData）。
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [setData]
  )

  /** 改一条的部分字段（乐观更新用）。 */
  const patch = useCallback(
    (key: string | number, update: (item: T) => T) => {
      setData((prev) =>
        prev
          ? {
              ...prev,
              items: prev.items.map((i) => (keyOf(i) === key ? update(i) : i)),
            }
          : prev
      )
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [setData]
  )

  /** 删掉一条。删空当前页时退回上一页——空页比空列表更让人困惑。 */
  const remove = useCallback(
    (key: string | number) => {
      setData((prev) => {
        if (!prev) return prev
        const items = prev.items.filter((i) => keyOf(i) !== key)
        if (items.length === 0 && page > 1) setPage(page - 1)
        return { ...prev, items, total: Math.max(prev.total - 1, 0) }
      })
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [setData, page]
  )

  return {
    items: data?.items ?? null,
    total: data?.total ?? 0,
    error,
    page,
    pageSize,
    setPage,
    setPageSize,
    setData,
    setError,
    replace,
    patch,
    remove,
  }
}
