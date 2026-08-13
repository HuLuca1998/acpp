import { useEffect, useState, type DependencyList } from "react"

/**
 * 一次性异步加载的标准样板：cancelled 守卫 + data/error 双态。
 * deps 变化时重新加载；setData 暴露给调用方做本地更新（删除行等）。
 *
 * 只适合「进页面拉一次」的场景；轮询、分页游标、多来源合并请自己写。
 */
export function useAsyncData<T>(fetcher: () => Promise<T>, deps: DependencyList) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    fetcher()
      .then((d) => {
        if (!cancelled) {
          setData(d)
          setError(null)
        }
      })
      .catch((err: Error) => {
        if (!cancelled) setError(err.message)
      })
    return () => {
      cancelled = true
    }
    // fetcher 是内联箭头，稳定性由 deps 表达——这正是本 hook 的契约。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  return { data, error, setData, setError }
}
