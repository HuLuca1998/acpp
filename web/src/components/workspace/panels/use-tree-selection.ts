import { useCallback, useRef, useState } from "react"

/**
 * 文件树的多选。
 *
 * **只选文件，不选目录**：批量动作（加引用、打包下载、复制链接）对目录
 * 要么没意义、要么另有入口——目录本来就能整个打成 zip。让目录参与选择，
 * Shift 范围选会把一堆无处可用的路径一起卷进来。
 *
 * 交互沿用 macOS/VSCode 的既有肌肉记忆，普通点击的行为**不变**：
 *   - 普通点击 → 清空选择并打开（原来什么样还什么样，不打断日常使用）
 *   - ⌘/Ctrl 点击 → 切换这一项，不打开
 *   - Shift 点击 → 从锚点到这一项的区间，不打开
 */
export function useTreeSelection() {
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  // 锚点存 ref 不进 state：它只在下一次 Shift 点击时被读，本身不影响渲染，
  // 进 state 等于每次点击都多一轮重绘。
  const anchorRef = useRef<string | null>(null)

  const clear = useCallback(() => {
    anchorRef.current = null
    setSelected((prev) => (prev.size === 0 ? prev : new Set()))
  }, [])

  /** ⌘/Ctrl 点击：切换单项，并把锚点挪到这里。 */
  const toggle = useCallback((path: string) => {
    anchorRef.current = path
    setSelected((prev) => {
      const next = new Set(prev)
      if (!next.delete(path)) next.add(path)
      return next
    })
  }, [])

  /**
   * Shift 点击：选中锚点到目标之间的所有**文件**。
   *
   * files 是当前可见顺序里的文件路径序列——区间按「屏幕上看到的顺序」算，
   * 不是按字母序或树的深度，否则用户框出来的和他看到的对不上。
   */
  const selectRange = useCallback((files: string[], path: string) => {
    const anchor = anchorRef.current
    const to = files.indexOf(path)
    if (to < 0) return
    const from = anchor ? files.indexOf(anchor) : -1
    if (from < 0) {
      // 没有锚点（第一次就按住 Shift）：退化成选中这一项。
      anchorRef.current = path
      setSelected(new Set([path]))
      return
    }
    const [lo, hi] = from <= to ? [from, to] : [to, from]
    setSelected(new Set(files.slice(lo, hi + 1)))
  }, [])

  /** 普通点击：选择归零（随后由调用方去打开这个文件）。 */
  const selectOnly = useCallback((path: string) => {
    anchorRef.current = path
    setSelected((prev) => (prev.size === 0 ? prev : new Set()))
  }, [])

  /**
   * 一次批量动作要作用在哪些路径上。
   *
   * 右键点在选中项上 → 整个选择；点在选中之外 → 只有那一项（并且不动
   * 选择本身）。这是 Finder 与 VSCode 的共同约定，照做能省掉一句说明。
   */
  const targetsFor = useCallback(
    (path: string): string[] => (selected.has(path) ? [...selected] : [path]),
    [selected]
  )

  return { selected, clear, toggle, selectRange, selectOnly, targetsFor }
}
