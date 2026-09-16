import { useEffect, useRef, useState } from "react"
import { RotateCwIcon } from "lucide-react"

import { cn } from "@/lib/utils"
import { Hint } from "@/components/hint"

/** 点一下转多久：够看清「它确实动了」，又不至于假装还在加载。 */
const SPIN_MS = 600

/**
 * 面板头部的刷新按钮。
 *
 * 单独成组件有两个理由。一是**统一**：文件树、查看器、git 面板群各自的
 * 刷新长得一样、位置一样，用户不必在每个面板里重新找。二是**把「转圈」
 * 关在按钮自己身上**：反馈状态若放在面板里，点一次刷新就会把整块正文
 * （可能是几千行代码或一张大表）跟着重渲一遍——这里只有一个 24px 的
 * 按钮，转多少次都无所谓。
 *
 * 转动是定时的，不等请求回来：数据没变时面板本就不该重渲（见查看器的
 * 同内容保留），拿请求的生命周期驱动动画反而会让「刷新了但没变化」
 * 看起来像什么都没发生。
 */
export function PanelRefreshButton({
  label,
  desc,
  onRefresh,
  align = "end",
  className,
}: {
  label: string
  desc?: string
  onRefresh: () => void
  align?: "start" | "center" | "end"
  className?: string
}) {
  const [spinning, setSpinning] = useState(false)
  // 计时器挂在 ref 上而不是 effect 里：连点要能把上一次的计时顶掉，
  // 否则圈会在第一下的点数到时停住，看着像后面几下没生效。
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  useEffect(
    () => () => {
      if (timerRef.current) clearTimeout(timerRef.current)
    },
    []
  )

  return (
    <Hint label={label} desc={desc} align={align}>
      <button
        type="button"
        aria-label={label}
        className={cn(
          "flex size-6 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97]",
          className
        )}
        onClick={() => {
          setSpinning(true)
          if (timerRef.current) clearTimeout(timerRef.current)
          timerRef.current = setTimeout(() => setSpinning(false), SPIN_MS)
          onRefresh()
        }}
      >
        <RotateCwIcon
          className={cn("size-3.5", spinning && "animate-spin")}
        />
      </button>
    </Hint>
  )
}
