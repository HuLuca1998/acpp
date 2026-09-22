import { usageTone } from "@/lib/chat/usage"
import { cn } from "@/lib/utils"

/**
 * 用量条。手写而不是装 shadcn 的 progress：这里要的是一根两像素的装饰线，
 * progress 组件的 root+indicator 两层结构与 aria 语义在这儿都用不上——占比
 * 数字就在旁边，读屏用户从文字拿到的信息比进度条更准。
 */
export function UsageBar({ percent }: { percent: number }) {
  return (
    <div className="h-1 w-full overflow-hidden rounded-full bg-muted">
      <div
        className={cn(
          "h-full rounded-full transition-[width,background-color] duration-300 ease-fluid",
          usageTone(percent).fill
        )}
        style={{ width: `${Math.min(100, Math.max(2, percent))}%` }}
      />
    </div>
  )
}
