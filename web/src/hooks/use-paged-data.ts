import { useCallback, useState } from "react"

import { useAsyncData } from "@/hooks/use-async-data"
import type { Paged, PageQuery } from "@/types/acp"

/**
 * 排序状态。结构与 TanStack Table 的 SortingState 一致（可直接互传），
 * 但这里不 import 它——hooks 不依赖 components，而且这份状态的真正消费者
 * 是后端的 `?sort=&order=`，不是表格组件。
 *
 * 只取第一项：多列排序在服务端要拼多段 ORDER BY，收益远不抵复杂度。
 */
export type SortState = { id: string; desc: boolean }[]

/**
 * 分页列表的标准接线：page/pageSize/排序状态 + 按它们拉数据 + 就地增删改。
 *
 * 每个列表页各写一遍这段逻辑必然会长歪——有的忘了改每页行数要回第一页，
 * 有的删到最后一条留在空页上。收在一处，全部列表页的行为就是同一套。
 *
 * **排序走服务端**：客户端排序在分页列表上是错的，它只会把当前这一页重排
 * 一遍，用户以为看到的是「全部里最大的」，其实是「这 20 条里最大的」。
 *
 * 标识默认取 `id`，技能这类用名字当主键的传 keyOf 覆盖。
 */
export function usePagedData<T>(
  fetcher: (params: PageQuery) => Promise<Paged<T>>,
  options?: {
    pageSize?: number
    keyOf?: (item: T) => string | number
  }
) {
  const keyOf = options?.keyOf ?? ((item: T) => (item as { id: number }).id)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(options?.pageSize ?? 20)
  const [sorting, setSorting] = useState<SortState>([])
  const sort = sorting[0]
  const { data, error, setData, setError } = useAsyncData(
    () =>
      fetcher({
        page,
        pageSize,
        sort: sort?.id,
        // 没排序就别带 order——一个孤零零的 order=asc 只会让 URL 更难读。
        order: sort ? (sort.desc ? "desc" : "asc") : undefined,
      }),
    [page, pageSize, sort?.id, sort?.desc]
  )

  /** 换排序回第一页：换了排序还停在第 5 页，那已经是另一批数据了。 */
  const changeSorting = useCallback((next: SortState) => {
    setSorting(next)
    setPage(1)
  }, [])

  /** 就地替换一条（编辑保存后刷新那一行，不重拉整页）。 */
  const replace = useCallback(
    (item: T) => {
      setData((prev) => {
        if (!prev) return prev
        const at = prev.items.findIndex((i) => keyOf(i) === keyOf(item))
        // 新建的落在当前页尾：翻页重拉时它会归位到真正的位置。
        if (at < 0) {
          return {
            ...prev,
            items: [...prev.items, item],
            total: prev.total + 1,
          }
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
    sorting,
    setPage,
    setPageSize,
    setSorting: changeSorting,
    setData,
    setError,
    replace,
    patch,
    remove,
  }
}
