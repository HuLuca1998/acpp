import { useCallback, useEffect, useState, type DependencyList } from "react"

/**
 * 一次性异步加载的标准样板：cancelled 守卫 + data/error 双态。
 * deps 变化时重新加载；setData 暴露给调用方做本地更新（删除行等）。
 * reload 按同样的参数再拉一次——旧数据留在原地，拉回来再整体替换，
 * 列表不会先清空再闪回来。
 *
 * 只适合「进页面拉一次」的场景；轮询、分页游标、多来源合并请自己写。
 */
export function useAsyncData<T>(
  fetcher: () => Promise<T>,
  deps: DependencyList
) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)
  // 只是个计数器：每加一就让下面的 effect 重跑一遍，值本身没有意义。
  const [version, setVersion] = useState(0)
  const reload = useCallback(() => setVersion((v) => v + 1), [])

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
  }, [version, ...deps])

  return { data, error, setData, setError, reload }
}
