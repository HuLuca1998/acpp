import { useEffect, useRef, useState } from "react"

/** 进度线扫一趟（从左到右）的时长，毫秒。指示器以它为周期：亮起后至少扫完一整趟才灭。 */
export const LOADING_CYCLE_MS = 500

/**
 * 请求结束时还要再亮多久：补到当前这一趟扫完为止。
 * 刚亮起（elapsed = 0）要扫满一趟；正在第 n 趟中途就等这趟走完；恰好在趟尾就再扫一整趟（避免只剩 0ms 时的抖动）。
 */
export function remainingMs(
  startedAt: number,
  now: number,
  cycle: number
): number {
  const elapsed = Math.max(0, now - startedAt)
  return cycle - (elapsed % cycle)
}

/**
 * 把「正在请求」变成「该显示指示器」：一旦亮起就按整趟计，请求早回来也等这趟扫完再灭；
 * 请求没回来就一直亮（动画循环）。连续两次请求共用同一次起点，不会闪两下。
 *
 * 快请求 30ms 就回来，指示器只闪一帧比不亮更糟——看见了动一下却不知道是什么。
 */
export function useMinLoading(
  active: boolean,
  cycle = LOADING_CYCLE_MS
): boolean {
  const [shown, setShown] = useState(active)
  const startedAt = useRef<number | null>(null)

  // 亮与灭都走定时器：亮是 0ms（避免在 effect 里同步 setState 触发级联渲染），
  // 灭是补到这趟扫完。渲染阶段不读时钟，起点记在 effect 里。
  useEffect(() => {
    if (active) {
      if (startedAt.current === null) startedAt.current = Date.now()
      const timer = setTimeout(() => setShown(true), 0)
      return () => clearTimeout(timer)
    }
    const started = startedAt.current
    const timer = setTimeout(
      () => {
        startedAt.current = null
        setShown(false)
      },
      started === null ? 0 : remainingMs(started, Date.now(), cycle)
    )
    return () => clearTimeout(timer)
  }, [active, cycle])

  return shown
}
