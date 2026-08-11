import { cn } from "@/lib/utils"

export type StatusTone = "success" | "warning" | "destructive" | "muted"

/**
 * 状态点 + 文字标签：列表页与概览统一的状态语言。
 * 比填充式 Badge 安静，颜色只出现在 6px 的点上，符合桌面控制台的克制基调。
 */
export function StatusDot({
  tone,
  pulse = false,
  label,
  className,
}: {
  tone: StatusTone
  /** 有活着的进程/连接时开启呼吸，传达"正在运行"的存在感。 */
  pulse?: boolean
  /** 不传则只渲染状态点本身（用在行首等紧凑位置）。 */
  label?: string
  className?: string
}) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 text-xs text-muted-foreground",
        className
      )}
    >
      <span
        aria-hidden
        className={cn(
          "size-1.5 shrink-0 rounded-full",
          tone === "success" && "bg-success",
          tone === "warning" && "bg-warning",
          tone === "destructive" && "bg-destructive",
          tone === "muted" && "bg-muted-foreground/50",
          pulse && "animate-breathe motion-reduce:animate-none"
        )}
      />
      {label}
    </span>
  )
}
