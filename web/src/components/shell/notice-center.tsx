import { useState, useSyncExternalStore } from "react"
import { useTranslation } from "react-i18next"
import { BellIcon, XIcon } from "lucide-react"

import { type Notice } from "@/lib/notify/store"
import { cn } from "@/lib/utils"
import { useNotices } from "@/hooks/use-notices"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import {
  NoticeAction,
  NoticeList,
  NoticeRow,
} from "@/components/shell/notice-list"
import { useNoticeLeave } from "@/hooks/use-notice-leave"

/**
 * 折叠态平行显示的条数：跟着视口高度走——侧栏底部的空间是从导航嘴里抢的，
 * 矮窗口多摆一条就少一截会话列表。显示不下的折叠成卡后的垫层，总数记在
 * 标题行的角标上。上限 3，与 iOS 通知横幅同款克制。
 */
function subscribeResize(fn: () => void) {
  window.addEventListener("resize", fn)
  return () => window.removeEventListener("resize", fn)
}

function maxParallelCount() {
  if (window.innerHeight < 720) return 1
  if (window.innerHeight < 900) return 2
  return 3
}

/**
 * 通知中心：侧栏底部一叠留得住的通知。
 *
 * 与 iOS 通知中心同构的三层交互：
 *  - **标题行**（通知中心 + 总数角标）负责展开完整列表；
 *  - **单张卡**点击直接执行动作——update 刷新页面，会话通知跳那条会话，
 *    不需要先展开列表再找一遍；
 *  - **卡角的 ×** hover 哪张出哪张，关的就是那一条（macOS 横幅同款）。
 *
 * 为什么留得住：toast 弹一下就走，管的是「此刻正好在看屏幕的人」；这里管
 * **回来的人**——局域网访客可能几十分钟才看一眼这个标签页，回来时最该看见
 * 的恰恰是「我不在的时候发生了什么」，尤其有 agent 停在那儿等他决策。
 */
export function NoticeCenter() {
  const { t } = useTranslation()
  const notices = useNotices()
  // 受控：点了某条要跳走时得把浮层收起来，否则它会盖在刚打开的会话上。
  const [open, setOpen] = useState(false)
  const maxParallel = useSyncExternalStore(
    subscribeResize,
    maxParallelCount,
    () => 1
  )

  if (notices.length === 0) return null

  const visible = notices.slice(0, maxParallel)
  const folded = notices.length - visible.length

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <div className="flex flex-col gap-1.5 px-1 group-data-[collapsible=icon]:hidden">
        {/* 标题行是展开列表的唯一入口：卡片的点击已经让给「执行动作」了。 */}
        <PopoverTrigger
          render={
            <button
              type="button"
              className={cn(
                "flex items-center gap-1.5 rounded-md px-1.5 py-1 text-xs font-medium",
                "text-muted-foreground transition-colors duration-150 ease-snappy",
                "hover:bg-accent hover:text-foreground"
              )}
            />
          }
        >
          {t("notify.center.title")}
          <Badge
            variant="secondary"
            className="h-4 min-w-4 px-1 text-[10px] tabular-nums"
          >
            {notices.length}
          </Badge>
        </PopoverTrigger>

        <div
          className={cn("relative flex flex-col gap-1.5", folded > 0 && "pb-3")}
        >
          {visible.map((notice) => (
            <NoticeCard key={notice.id} notice={notice} />
          ))}
          {/* 显示不下的折在最后一张卡后面：两层错位的边缘，照 iOS 通知
              中心的堆叠示意，只表厚度不载信息。挂在容器而不是卡内——卡有
              view-transition-name（自成层叠上下文），负 z 的垫层放卡里会
              浮到卡背景之上、把边框印进卡面。 */}
          {folded > 0 ? (
            <div
              aria-hidden
              className="absolute inset-x-3 bottom-1.5 -z-10 h-6 rounded-xl border bg-card"
            />
          ) : null}
          {folded > 1 ? (
            <div
              aria-hidden
              className="absolute inset-x-5 bottom-0 -z-20 h-6 rounded-xl border bg-card/60"
            />
          ) : null}
        </div>
      </div>

      {/* 折叠成图标条时只留一个铃铛。 */}
      <div className="hidden justify-center group-data-[collapsible=icon]:flex">
        <PopoverTrigger
          render={
            <Button
              variant="ghost"
              size="icon"
              className="size-8"
              aria-label={t("notify.center.title")}
            />
          }
        >
          <span className="relative">
            <BellIcon className="size-4" />
            <span className="absolute -end-1 -top-1 size-1.5 rounded-full bg-warning" />
          </span>
        </PopoverTrigger>
      </div>

      <PopoverContent side="right" align="end" className="w-88 p-0">
        <NoticeList onNavigate={() => setOpen(false)} />
      </PopoverContent>
    </Popover>
  )
}

/**
 * 折叠态的一张独立卡：动作、hover、关闭、动画都是自己的。
 *
 * 动效（§5.4）：入场从下方 8px 冒出 + 淡入（低频事件，值得一个完整入场）；
 * 离场向右滑出 + 缩小 + 淡出——通知从内容区来、往屏幕边缘去的方向感，
 * 播完 LEAVE_MS 才真正从存量删除。reduced-motion 下只留透明度。
 */
function NoticeCard({ notice }: { notice: Notice }) {
  const { t } = useTranslation()
  const { leaving, leave } = useNoticeLeave(notice.id)

  return (
    <div
      className={cn(
        "group/card relative rounded-xl border bg-card p-2.5",
        "transition-[transform,opacity,background-color] ease-snappy",
        "hover:bg-accent",
        leaving
          ? "translate-x-4 scale-95 opacity-0 duration-240 motion-reduce:translate-x-0 motion-reduce:scale-100"
          : cn(
              "opacity-100 duration-280 starting:translate-y-2 starting:scale-[0.97] starting:opacity-0",
              "motion-reduce:starting:translate-y-0 motion-reduce:starting:scale-100"
            )
      )}
      // 每卡独立的 view-transition 名：列表重排（新通知按优先级插进中间、
      // 关一条后下一条补位）时，位置变化由浏览器补成平滑位移。
      style={{ viewTransitionName: `notice-${notice.id}` }}
    >
      {/* 整卡即动作：update 刷新页面，会话通知跳那条会话。
          内容盖在点击面上但不吃指针，角上的 × 再浮回来。 */}
      <NoticeAction
        notice={notice}
        className="absolute inset-0 rounded-xl"
        onAct={leave}
      />
      <div className="pointer-events-none relative">
        <NoticeRow notice={notice} />
      </div>

      {/* 单条关闭：hover 这张卡才现的浮角圆钮，关的就是这一条——
          macOS 通知横幅的同款。可以藏，因为它不是唯一入口：展开
          后每条有常驻清除（web/AGENTS.md §5.5 反对的是「藏了就
          点不到」，不是有常驻替代的快捷方式）。 */}
      <button
        type="button"
        className={cn(
          "absolute -end-1 -top-1 z-10 hidden size-4 items-center justify-center",
          "rounded-full border bg-popover text-muted-foreground shadow-xs",
          "hover:text-foreground group-hover/card:flex"
        )}
        aria-label={t("notify.center.dismiss")}
        onClick={leave}
      >
        <XIcon className="size-2.5" />
      </button>
    </div>
  )
}

/**
 * 一张卡（或一行）的点击动作，做成拉伸元素盖满容器。
 * update 是刷新（不需要任何上下文的动作）；会话通知是去那条会话——
 * 决策真要拿主意本来就得看上下文，跳过去反而比就地摆按钮快。
 *
 * 点过即办过：动作执行完这条就从列表撤走（onAct）。通知中心留的是
 * 「我不在时发生了什么」，人已经点进去看了的事再挂着就是残影——真还悬着
 * 的决策，会话页里的卡片才是事实源。update 例外，刷新会把一切重来。
 */
