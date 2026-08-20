import type { ReactElement, ReactNode } from "react"

import { cn } from "@/lib/utils"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"

/**
 * 说明气泡：给纯图标控件补一句「这按钮是干嘛的」。
 *
 * 图标按钮的 `aria-label` 只有读屏软件听得见，原生 `title` 要等一两秒才
 * 出来、样式也不归我们管（桌面壳的 WKWebView 里尤其不可靠）。所以图标
 * 控件统一走 tooltip：包住已有的按钮即可，不改按钮自身的结构与样式。
 *
 *   <Hint label={t("workspace.git.refresh")}>
 *     <button type="button" aria-label={…}>…</button>
 *   </Hint>
 *
 * `label` 是名字，`desc` 是「按下去会发生什么」——名字已经不言自明（关闭、
 * 删除）就别写第二行，一屏图标每个都挂两行说明只会变成噪音。
 *
 * 触发元素仍然要保留 `aria-label`：tooltip 是给眼睛看的，读屏走的是标签。
 */
export function Hint({
  label,
  desc,
  shortcut,
  side = "top",
  align = "center",
  children,
}: {
  label: ReactNode
  /** 可选的第二行小字：解释后果或适用场景。 */
  desc?: ReactNode
  /** 快捷键徽标（`<Kbd>`），跟在标题右侧。 */
  shortcut?: ReactNode
  side?: "top" | "bottom" | "left" | "right"
  align?: "start" | "center" | "end"
  /** 被包住的控件，必须是单个能接 ref 的元素。 */
  children: ReactElement
}) {
  return (
    // 气泡本身不可交互：`disableHoverablePopup` 让鼠标一离开控件就收，
    // `pointer-events-none` 让它挡住的东西照样点得到——说明性气泡飘在
    // 密集工具条上方，挡住隔壁按钮就成了新的麻烦。
    <Tooltip disableHoverablePopup>
      <TooltipTrigger render={children} />
      <TooltipContent
        side={side}
        align={align}
        className={cn(
          "pointer-events-none",
          desc && "flex-col items-start gap-0.5 py-2"
        )}
      >
        <span className="flex items-center gap-1.5">
          {label}
          {shortcut}
        </span>
        {desc ? (
          <span className="leading-snug text-background/65">{desc}</span>
        ) : null}
      </TooltipContent>
    </Tooltip>
  )
}
