import { useCallback, useEffect, useRef, useState } from "react"

/**
 * 侧栏外框：可拖的宽度，以及折叠后的悬停浮出。
 *
 * 两件事都不下沉到 `components/ui/sidebar.tsx`——那是 shadcn 托管区，
 * 定制优先在外层解决（ui/AGENTS.md）。这里只出状态，样式由调用方拼类名。
 */

const WIDTH_KEY = "acpp.sidebar.width"
const WIDTH_MIN = 200
const WIDTH_MAX = 420
const WIDTH_DEFAULT = 288

/**
 * 指针离开后再收回的宽限。从折叠钮滑向菜单的路上会短暂脱离两者，
 * 立刻收回就会闪一下（规范 §5.6）。
 */
const PEEK_CLOSE_DELAY = 180

function loadWidth(): number {
  const raw = Number(localStorage.getItem(WIDTH_KEY))
  if (!Number.isFinite(raw) || raw <= 0) return WIDTH_DEFAULT
  return Math.min(WIDTH_MAX, Math.max(WIDTH_MIN, raw))
}

export function useSidebarFrame() {
  const [width, setWidth] = useState(loadWidth)
  const [peek, setPeek] = useState(false)

  /**
   * 刚点完折叠时指针还停在钮上，浮层会立刻弹回来——那不是用户要看菜单，
   * 只是他手还没挪开。点击后上锁，指针真正离开一次才重新允许浮出。
   */
  const armed = useRef(true)
  const closeTimer = useRef(0)

  useEffect(() => () => window.clearTimeout(closeTimer.current), [])

  const openPeek = useCallback(() => {
    if (!armed.current) return
    window.clearTimeout(closeTimer.current)
    setPeek(true)
  }, [])

  const closePeek = useCallback(() => {
    window.clearTimeout(closeTimer.current)
    closeTimer.current = window.setTimeout(() => setPeek(false), PEEK_CLOSE_DELAY)
  }, [])

  /** 指针离开折叠钮：解锁，下次悬停可以正常浮出。 */
  const releasePeekLock = useCallback(() => {
    armed.current = true
    closePeek()
  }, [closePeek])

  /** 折叠状态被切换：立刻收起浮层并上锁。 */
  const lockPeek = useCallback(() => {
    armed.current = false
    window.clearTimeout(closeTimer.current)
    setPeek(false)
  }, [])

  /** 拖右沿调宽。指针捕获保证拖出窗口再拖回来也不丢。 */
  const startResize = useCallback((event: React.PointerEvent<HTMLElement>) => {
    event.preventDefault()
    const handle = event.currentTarget
    const startX = event.clientX
    const startWidth = handle.parentElement?.getBoundingClientRect().width ?? WIDTH_DEFAULT
    handle.setPointerCapture(event.pointerId)
    document.body.dataset.resizing = "true"

    const move = (e: PointerEvent) => {
      const next = Math.min(
        WIDTH_MAX,
        Math.max(WIDTH_MIN, startWidth + e.clientX - startX)
      )
      setWidth(next)
    }
    const done = () => {
      delete document.body.dataset.resizing
      handle.removeEventListener("pointermove", move)
      handle.removeEventListener("pointerup", done)
      handle.removeEventListener("pointercancel", done)
      // 拖完才落盘：拖动过程每帧写一次 localStorage 是同步 IO，会掉帧。
      setWidth((w) => {
        localStorage.setItem(WIDTH_KEY, String(w))
        return w
      })
    }
    handle.addEventListener("pointermove", move)
    handle.addEventListener("pointerup", done)
    handle.addEventListener("pointercancel", done)
  }, [])

  const resetWidth = useCallback(() => {
    setWidth(WIDTH_DEFAULT)
    localStorage.setItem(WIDTH_KEY, String(WIDTH_DEFAULT))
  }, [])

  return {
    width,
    startResize,
    resetWidth,
    peek,
    openPeek,
    closePeek,
    releasePeekLock,
    lockPeek,
  }
}
