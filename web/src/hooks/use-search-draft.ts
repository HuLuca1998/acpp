import { useCallback, useState } from "react"

/**
 * 搜索区的状态：草稿（输入框里的）与已提交（拿去请求的）两份。
 * `set` 改草稿，`commit` 把草稿变成已提交，`reset` 两份一起清空。
 * 页面把 `values` 放进 usePagedData 的 deps 里，提交或重置就重拉。
 */
export function useSearchDraft<T extends Record<string, string>>(initial: T) {
  const [draft, setDraft] = useState<T>(initial)
  const [values, setValues] = useState<T>(initial)
  const set = useCallback(
    <K extends keyof T>(key: K, value: T[K]) =>
      setDraft((d) => ({ ...d, [key]: value })),
    []
  )
  const commit = useCallback(() => setValues(draft), [draft])
  const reset = useCallback(() => {
    setDraft(initial)
    setValues(initial)
    // initial 是调用方写在 JSX 里的字面量，内容不变；按引用比较只会让它每次都重建。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  return { draft, values, set, commit, reset }
}
