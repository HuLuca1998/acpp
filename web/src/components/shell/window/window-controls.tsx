import { useTranslation } from "react-i18next"

import { Hint } from "@/components/hint"
import { NotifyMenu } from "@/components/shell/notify-menu"
import { Kbd } from "@/components/ui/kbd"
import { SidebarTrigger, useSidebar } from "@/components/ui/sidebar"
import type { useSidebarFrame } from "@/hooks/use-sidebar-frame"
import { cn } from "@/lib/utils"

/**
 * 窗口左上角那一组控件：折叠、通知，以及页面自己挂上来的按钮。
 *
 * **整个界面只有这一份**，位置用 fixed 直接算：展开时停在侧栏那条的右端，
 * 折叠后侧栏不在了，落回窗口左上角（桌面壳里紧挨系统红绿灯）。不做成两份再靠
 * CSS 对齐——两个容器的内外边距基准不同，同一个位置会差出几像素，一眼看得见
 * （规范 §5.6）。位移跟着侧栏动画同步过渡，不是瞬移。
 */
export function WindowControls({
  frame,
  children,
}: {
  frame: ReturnType<typeof useSidebarFrame>
  children?: React.ReactNode
}) {
  const { t } = useTranslation()
  const { state } = useSidebar()
  const collapsed = state === "collapsed"

  return (
    <div
      className={cn(
        "fixed top-2 z-50 flex items-center gap-0.5",
        "transition-[left] duration-200 ease-linear",
        collapsed
          ? "left-(--titlebar-lights)"
          : // 侧栏右沿减去控件组自身宽度：inset 变体的容器有 8px 内边距，一并扣掉。
            "left-[calc(var(--sidebar-width)-var(--titlebar-controls)-8px)]"
      )}
    >
      <Hint
        label={t("nav.toggleSidebar")}
        shortcut={<Kbd>⌘B</Kbd>}
        align="start"
      >
        <SidebarTrigger
          className="size-6"
          aria-label={t("nav.toggleSidebar")}
          // 折叠态悬停这颗钮，侧栏浮出来给人瞄一眼；点击后上锁，
          // 指针离开一次才重新允许浮出，免得刚折叠就弹回来。
          onMouseEnter={collapsed ? frame.openPeek : undefined}
          onMouseLeave={frame.releasePeekLock}
          onClick={frame.lockPeek}
        />
      </Hint>
      <NotifyMenu />
      {children}
    </div>
  )
}
