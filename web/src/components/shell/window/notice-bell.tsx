import { useState } from "react"
import { useTranslation } from "react-i18next"
import { BellIcon } from "lucide-react"

import { Hint } from "@/components/hint"
import { NoticeList } from "@/components/shell/notice-list"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { useNotices } from "@/hooks/use-notices"
import { cn } from "@/lib/utils"

/**
 * 窗口控件组里的通知入口。
 *
 * 侧栏底部本来就有通知中心，但它跟着侧栏一起收——折叠之后就再也看不见了，
 * 而通知恰恰是「有事等人处理」，不该随布局消失。这颗铃铛住在窗口控件组里，
 * 哪一页、折不折叠都在。
 *
 * 有待处理的事时右上角亮一颗红点：数量写在展开后的角标上，收起时只报「有」，
 * 一个点就够——具体几条要人点进去才有意义。
 */
export function NoticeBell() {
  const { t } = useTranslation()
  const notices = useNotices()
  // 受控：点了某条要跳走时得把浮层收起来，否则它会盖在刚打开的会话上。
  const [open, setOpen] = useState(false)

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <Hint label={t("notify.center.title")} align="start">
        {/* 用原生 button 而不是 Button 组件：Base UI 的 render 模式下，
            trigger 自己的 props 会覆盖掉 render 元素的 className，套 Button
            的话尺寸类整个丢掉、按钮塌成 0 宽。项目里其它 PopoverTrigger
            也都是这个写法。 */}
        <PopoverTrigger
          render={
            <button
              type="button"
              aria-label={t("notify.center.title")}
              className={cn(
                "relative flex size-6 shrink-0 items-center justify-center rounded-md",
                "text-muted-foreground transition-colors duration-150 ease-snappy",
                "hover:bg-muted hover:text-foreground"
              )}
            />
          }
        >
          <BellIcon className="size-4" />
          {notices.length > 0 ? (
            <span
              aria-hidden
              className="absolute end-0.5 top-0.5 size-1.5 rounded-full bg-destructive"
            />
          ) : null}
        </PopoverTrigger>
      </Hint>
      <PopoverContent align="start" className="w-88 p-0">
        <NoticeList onNavigate={() => setOpen(false)} />
      </PopoverContent>
    </Popover>
  )
}
