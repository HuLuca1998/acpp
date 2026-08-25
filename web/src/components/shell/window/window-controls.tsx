import { useEffect } from "react"
import { useTranslation } from "react-i18next"

import { Hint } from "@/components/hint"
import { NotifyMenu } from "@/components/shell/notify-menu"
import { Kbd } from "@/components/ui/kbd"
import { SidebarTrigger, useSidebar } from "@/components/ui/sidebar"
import type { useSidebarFrame } from "@/hooks/use-sidebar-frame"

/**
 * 窗口左上角那一条：身份（桌面壳是系统红绿灯，浏览器是应用图标与名称）
 * 加上折叠与通知。**这里住的是「哪一页都在」的东西**：跟着内容走的（面板操作
 * 之类）归内容区顶栏右端。通知曾放在那边，结果每条顶栏右端都挂一颗铃，
 * 而它其实和折叠一样从不随页面变化。
 *
 * **整条只有一份，fixed 钉在窗口坐标上，与侧栏折不折叠无关**。它是窗口的
 * chrome，不是侧栏的内容——跟着侧栏走的话，折叠那一下按钮就会横穿半个窗口，
 * 而折叠恰恰是最高频的操作，位置必须是肌肉记忆。也别拆成两份再靠 CSS 对齐：
 * 两个容器的内外边距基准不同，同一个位置差出几像素，一眼看得见（规范 §5.6）。
 *
 * `no-drag-region` 不能省：整条浮在拖动区上方，而拖动区是窗口级的矩形，
 * 会吞掉落在范围内的点击——漏标的表现是按钮看得见、点不动。
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

  // 折叠状态挂到根元素上：工作区页里，最左上那组标签栏要据此给这一条让位，
  // 而它是 dockview 渲染的、够不着 React 状态（见 index.css）。
  useEffect(() => {
    document.documentElement.dataset.sidebarCollapsed = String(collapsed)
  }, [collapsed])

  return (
    <div className="no-drag-region fixed top-0 left-0 z-50 flex h-(--titlebar-height) w-(--titlebar-inset) items-center gap-0.5 ps-(--titlebar-lights) pe-2">
      {/* 桌面壳里这块被系统红绿灯占着（靠 --titlebar-lights 让位），应用名
          不再重复出现；浏览器里那儿空着，正好放图标与名称。 */}
      <div className="me-1 flex items-center gap-1.5 in-data-[shell=desktop]:hidden">
        {/* 与右边的按钮同为 24px：一条 40px 的横栏上，各个视觉块等高才立得住。
            图标是带底色的实心方块，比同尺寸的线性图标重，到此为止。 */}
        <img src="/app-icon.svg" alt="" className="size-6 shrink-0" />
        <span className="text-sm font-semibold tracking-tight">
          {t("common.appName")}
        </span>
      </div>

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
