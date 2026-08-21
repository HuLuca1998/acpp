import { useState, useSyncExternalStore } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"
import {
  BellIcon,
  CircleCheckIcon,
  MessageCircleQuestionMarkIcon,
  OctagonXIcon,
  RefreshCwIcon,
  ShieldQuestionMarkIcon,
  XIcon,
} from "lucide-react"

import { formatRelativeTime } from "@/lib/format"
import { clearNotices, dismissNotice, type Notice } from "@/lib/notify/store"
import { cn } from "@/lib/utils"
import { useNotices } from "@/hooks/use-notices"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { ScrollArea } from "@/components/ui/scroll-area"

/**
 * 每类通知的图标与色调。照 iOS 横幅的图形语言：图标装进一块色底小方块
 * （相当于 app 图标的位置），颜色只落在这一块上，卡片本身永远是中性纸面色。
 */
const STYLES = {
  permission: { Icon: ShieldQuestionMarkIcon, tone: "text-warning", tile: "bg-warning/15" },
  elicitation: { Icon: MessageCircleQuestionMarkIcon, tone: "text-warning", tile: "bg-warning/15" },
  turn_end: { Icon: CircleCheckIcon, tone: "text-success", tile: "bg-success/15" },
  error: { Icon: OctagonXIcon, tone: "text-destructive", tile: "bg-destructive/15" },
  update: { Icon: RefreshCwIcon, tone: "text-warning", tile: "bg-warning/15" },
  // 撤回信号不会进列表，列在这里只是让类型收口。
  permission_done: { Icon: CircleCheckIcon, tone: "text-muted-foreground", tile: "bg-muted" },
  elicitation_done: { Icon: CircleCheckIcon, tone: "text-muted-foreground", tile: "bg-muted" },
} as const

const TITLE_KEYS = {
  permission: "notify.permission",
  elicitation: "notify.elicitation",
  turn_end: "notify.turnEnd",
  error: "notify.error",
  update: "backend.updateAvailable",
  permission_done: "notify.turnEnd",
  elicitation_done: "notify.turnEnd",
} as const

/** 离场动画时长。再长就变成「删不掉」的错觉（规范上限 300ms）。 */
const LEAVE_MS = 240

/**
 * 关闭一条通知的两步走：先播离场动画，播完才真正从存量里删。
 * React 卸载是瞬时的，不给这一拍，卡片就是「啪」地消失——出现有动画、
 * 消失没有，像话说一半。
 */
function useLeave(id: string) {
  const [leaving, setLeaving] = useState(false)
  const leave = () => {
    if (leaving) return
    setLeaving(true)
    setTimeout(() => dismissNotice(id), LEAVE_MS)
  }
  return { leaving, leave }
}

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

        <div className={cn("flex flex-col gap-1.5", folded > 0 && "pb-3")}>
          {visible.map((notice, i) => (
            <NoticeCard key={notice.id} notice={notice} last={i === visible.length - 1} folded={folded}>
            </NoticeCard>
          ))}
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
        <div className="flex items-center justify-between border-b px-3 py-2">
          <span className="flex items-center gap-1.5 text-sm font-medium">
            {t("notify.center.title")}
            <Badge
              variant="secondary"
              className="h-4 min-w-4 px-1 text-[10px] tabular-nums"
            >
              {notices.length}
            </Badge>
          </span>
          <Button
            variant="ghost"
            size="sm"
            className="h-6 px-2 text-xs"
            onClick={clearNotices}
          >
            {t("notify.center.clearAll")}
          </Button>
        </div>
        <ScrollArea className="max-h-96">
          <ul className="flex flex-col gap-1.5 p-2">
            {groupNotices(notices).map((group) => (
              <li key={group.key}>
                {/* 组头只在多条时出现：单条自己会说明来自哪个会话。 */}
                {group.items.length > 1 ? (
                  <div className="flex items-baseline justify-between gap-2 px-2 pt-1 pb-0.5">
                    <span className="truncate text-[11px] font-medium text-muted-foreground">
                      {group.title}
                    </span>
                    <span className="shrink-0 text-[10px] text-muted-foreground tabular-nums">
                      {t("notify.center.groupCount", { count: group.items.length })}
                    </span>
                  </div>
                ) : null}
                {group.items.map((notice) => (
                  <NoticeItem
                    key={notice.id}
                    notice={notice}
                    // 组头已经报过会话名，组内不再逐条重复。
                    showSession={group.items.length === 1}
                    onNavigate={() => setOpen(false)}
                  />
                ))}
              </li>
            ))}
          </ul>
        </ScrollArea>
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
function NoticeCard({
  notice,
  last,
  folded,
  children,
}: {
  notice: Notice
  last: boolean
  folded: number
  children?: React.ReactNode
}) {
  const { t } = useTranslation()
  const { leaving, leave } = useLeave(notice.id)

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
      {/* 显示不下的折在最后一张卡后面：两层错位的边缘，照 iOS
          通知中心的堆叠示意，只表厚度不载信息。 */}
      {last && folded > 0 ? (
        <>
          <div
            aria-hidden
            className="absolute inset-x-2 -bottom-1.5 -z-10 h-6 rounded-xl border bg-card"
          />
          {folded > 1 ? (
            <div
              aria-hidden
              className="absolute inset-x-4 -bottom-3 -z-20 h-6 rounded-xl border bg-card/60"
            />
          ) : null}
        </>
      ) : null}

      {/* 整卡即动作：update 刷新页面，会话通知跳那条会话。
          内容盖在点击面上但不吃指针，角上的 × 再浮回来。 */}
      <NoticeAction notice={notice} className="absolute inset-0 rounded-xl" />
      <div className="pointer-events-none relative">
        <NoticeRow notice={notice} />
      </div>
      {children}

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
 */
function NoticeAction({
  notice,
  className,
  onNavigate,
}: {
  notice: Notice
  className: string
  onNavigate?: () => void
}) {
  const { t } = useTranslation()
  if (notice.kind === "update") {
    return (
      <button
        type="button"
        className={className}
        aria-label={t("backend.reload")}
        onClick={() => window.location.reload()}
      />
    )
  }
  if (!notice.sessionId) return null
  return (
    <Link
      to={`/sessions/${notice.sessionId}`}
      aria-label={t(TITLE_KEYS[notice.kind])}
      className={className}
      onClick={onNavigate}
    />
  )
}

/** 展开列表里的一条：整行执行动作，右侧常驻清除。 */
function NoticeItem({
  notice,
  showSession,
  onNavigate,
}: {
  notice: Notice
  showSession?: boolean
  onNavigate: () => void
}) {
  const { t } = useTranslation()
  const { leaving, leave } = useLeave(notice.id)
  return (
    <div
      className={cn(
        "relative flex items-start gap-1 rounded-lg p-2 hover:bg-accent",
        "transition-[transform,opacity,background-color] ease-snappy",
        leaving
          ? "translate-x-4 opacity-0 duration-240 motion-reduce:translate-x-0"
          : "duration-280"
      )}
    >
      <NoticeAction
        notice={notice}
        className="absolute inset-0 rounded-lg"
        onNavigate={onNavigate}
      />
      <div className="pointer-events-none relative min-w-0 flex-1">
        <NoticeRow notice={notice} showSession={showSession} />
      </div>
      <Button
        variant="ghost"
        size="icon"
        className="relative size-5 shrink-0 text-muted-foreground hover:text-foreground"
        aria-label={t("notify.center.dismiss")}
        onClick={leave}
      >
        <XIcon className="size-3" />
      </Button>
    </div>
  )
}

interface NoticeGroup {
  key: string
  title: string
  items: Notice[]
}

/**
 * 按来源分组——iOS 通知中心的同款组织：同一会话攒下的几条聚成一组
 * （组头报会话名与条数），而不是散在列表里逐条抢位置。
 *
 * 输入已按优先级排好序，按 key 首现的顺序建组即可：组的顺序天然等于
 * 「组内最高优先级」的顺序，组内也保持优先级序。update 不属于任何会话，
 * 自成一组，恰好因优先级最高而永远在最上面。
 */
function groupNotices(notices: Notice[]): NoticeGroup[] {
  const groups = new Map<string, NoticeGroup>()
  for (const notice of notices) {
    const key = notice.sessionId ? `session-${notice.sessionId}` : notice.kind
    let group = groups.get(key)
    if (!group) {
      group = { key, title: notice.sessionTitle ?? "", items: [] }
      groups.set(key, group)
    }
    group.items.push(notice)
  }
  return [...groups.values()]
}

/** 图标 + 标题 + 时间 + 一行摘要。 */
function NoticeRow({
  notice,
  showSession = true,
}: {
  notice: Notice
  /** 组头已报过会话名时传 false，免得每行再念一遍。 */
  showSession?: boolean
}) {
  const { t, i18n } = useTranslation()
  const { Icon, tone, tile } = STYLES[notice.kind]
  const when = formatRelativeTime(new Date(notice.at).toISOString(), i18n.language)
  const desc = [showSession ? notice.sessionTitle : "", notice.text]
    .filter(Boolean)
    .join(" · ")

  return (
    <div className="flex min-w-0 items-center gap-2.5">
      <span
        className={cn(
          "flex size-8 shrink-0 items-center justify-center rounded-lg",
          tile
        )}
      >
        <Icon className={cn("size-4", tone)} />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline justify-between gap-2">
          <span className="truncate text-xs font-medium">
            {t(TITLE_KEYS[notice.kind])}
          </span>
          <span className="shrink-0 text-[10px] text-muted-foreground tabular-nums">
            {when}
          </span>
        </div>
        {desc ? (
          <p className="truncate text-xs text-muted-foreground">{desc}</p>
        ) : null}
      </div>
    </div>
  )
}
