import { useTranslation } from "react-i18next"
import { Link } from "react-router"
import { XIcon } from "lucide-react"

import { formatRelativeTime } from "@/lib/format"
import { clearNotices, type Notice } from "@/lib/notify/store"
import { cn } from "@/lib/utils"
import { useNotices } from "@/hooks/use-notices"
import { useNoticeLeave } from "@/hooks/use-notice-leave"
import { STYLES, TITLE_KEYS } from "@/components/shell/notice-visuals"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"

/**
 * 通知中心的完整列表（按会话分组）。
 *
 * 独立成文件是因为有两个入口：侧栏底部的通知中心，以及窗口控件组里的铃铛。
 * 后者是折叠态唯一的入口——侧栏一收起来，底部那个就看不见了，而通知恰恰是
 * 「有事等人处理」，不该跟着侧栏一起消失。
 */
export function NoticeList({ onNavigate }: { onNavigate?: () => void }) {
  const { t } = useTranslation()
  const notices = useNotices()

  return (
    <>
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
          disabled={notices.length === 0}
        >
          {t("notify.center.clearAll")}
        </Button>
      </div>
      {notices.length === 0 ? (
        <p className="px-3 py-6 text-center text-xs text-muted-foreground">
          {t("notify.center.empty")}
        </p>
      ) : (
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
                      {t("notify.center.groupCount", {
                        count: group.items.length,
                      })}
                    </span>
                  </div>
                ) : null}
                {group.items.map((notice) => (
                  <NoticeItem
                    key={notice.id}
                    notice={notice}
                    // 组头已经报过会话名，组内不再逐条重复。
                    showSession={group.items.length === 1}
                    onNavigate={onNavigate ?? (() => {})}
                  />
                ))}
              </li>
            ))}
          </ul>
        </ScrollArea>
      )}
    </>
  )
}

export function NoticeAction({
  notice,
  className,
  onAct,
}: {
  notice: Notice
  className: string
  /** 动作执行后的收尾：撤走这条，展开态还要顺手收起浮层。 */
  onAct?: () => void
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
      onClick={onAct}
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
  const { leaving, leave } = useNoticeLeave(notice.id)
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
        onAct={() => {
          onNavigate()
          leave()
        }}
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
export function NoticeRow({
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
