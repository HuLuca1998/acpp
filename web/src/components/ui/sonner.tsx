"use client"

import { useSyncExternalStore } from "react"

import { useTheme } from "@/components/shell/theme-provider"
import { Toaster as Sonner, type ToasterProps } from "sonner"
import {
  CircleCheckIcon,
  InfoIcon,
  TriangleAlertIcon,
  OctagonXIcon,
  Loader2Icon,
} from "lucide-react"

/**
 * 平行可见的 toast 数：上限 3（iOS 通知横幅的同款克制），矮窗口再收紧——
 * toast 从顶部往下堆，小屏幕上堆满三条会把大半个工作区盖住。溢出的并不会
 * 丢，sonner 自己折叠成一叠，hover 展开。
 */
function subscribeResize(fn: () => void) {
  window.addEventListener("resize", fn)
  return () => window.removeEventListener("resize", fn)
}

function visibleToastCount() {
  if (window.innerHeight < 500) return 1
  if (window.innerHeight < 760) return 2
  return 3
}

const Toaster = ({ ...props }: ToasterProps) => {
  const { theme } = useTheme()
  const visibleToasts = useSyncExternalStore(
    subscribeResize,
    visibleToastCount,
    () => 3
  )

  return (
    <Sonner
      theme={theme as ToasterProps["theme"]}
      // 顶部居中：右下角是人眼最容易略过的位置，而这里出现的都是要被看见
      // 的东西（等你决策、出错了）。调用方可用 props 覆盖。
      position="top-center"
      visibleToasts={visibleToasts}
      className="toaster group"
      icons={{
        success: <CircleCheckIcon className="size-4" />,
        info: <InfoIcon className="size-4" />,
        warning: <TriangleAlertIcon className="size-4" />,
        error: <OctagonXIcon className="size-4" />,
        loading: <Loader2Icon className="size-4 animate-spin" />,
      }}
      style={
        {
          "--normal-bg": "var(--popover)",
          "--normal-text": "var(--popover-foreground)",
          "--normal-border": "var(--border)",
          "--border-radius": "var(--radius)",
        } as React.CSSProperties
      }
      toastOptions={{
        classNames: {
          toast: "cn-toast",
        },
      }}
      {...props}
    />
  )
}

export { Toaster }
