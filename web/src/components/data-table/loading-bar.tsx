import { useTranslation } from "react-i18next"

import { cn } from "@/lib/utils"
import { LOADING_CYCLE_MS } from "@/hooks/use-min-loading"

/**
 * 数据加载指示器：贴在表格区顶端的 3px 进度线，不确定进度来回扫，不遮挡内容、不拦截点击。
 * 父容器要有 `relative`。`show` 来自 useMinLoading(fetching)：以「扫一趟」为周期，亮起后至少完整扫完一趟才灭。
 * 动画时长与 LOADING_CYCLE_MS 同源，JS 的灭灯时机和 CSS 的趟尾对齐。
 */
export function LoadingBar({
  show,
  className,
}: {
  show: boolean
  className?: string
}) {
  const { t } = useTranslation()
  return (
    <div
      role="status"
      aria-live="polite"
      aria-label={show ? t("table.loading") : undefined}
      aria-hidden={!show}
      className={cn(
        "pointer-events-none absolute inset-x-0 top-0 z-20 h-[3px] overflow-hidden rounded-t-[inherit] bg-primary/20 transition-opacity",
        show ? "opacity-100" : "opacity-0",
        className
      )}
    >
      {show ? (
        <div
          className="absolute top-0 h-full w-1/2 animate-loading-sweep rounded-full bg-primary motion-reduce:w-full motion-reduce:animate-none"
          style={{ animationDuration: `${LOADING_CYCLE_MS}ms` }}
        />
      ) : null}
    </div>
  )
}
