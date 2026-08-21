import { useEffect } from "react"
import { useTranslation } from "react-i18next"
import { useLocation, useNavigate } from "react-router"
import { toast } from "sonner"

import { useServerEvents } from "@/hooks/use-server-events"
import { api } from "@/lib/api"
import {
  DEFAULT_NOTIFICATION_ACTION,
  NOTIFICATION_ACTION_EVENT,
  desktopNotify,
  isDesktop,
  type NotificationAction,
} from "@/lib/desktop"
import { flashTitle, isUserWatching, playChime, stopFlashTitle } from "@/lib/notify/in-page"
import { loadNotifyPrefs } from "@/lib/notify/prefs"
import { dismissNotice, pushNotice } from "@/lib/notify/store"
import type { NoticeEvent, ServerEvent } from "@/types/acp"

/** 通知的 id：决策与问答按各自的请求 id，这样处理完能精确撤回。 */
function noticeID(ev: ServerEvent): string {
  switch (ev.event) {
    case "permission":
    case "permission_done":
      return `perm-${ev.permissionId}`
    case "elicitation":
    case "elicitation_done":
      return `elicit-${ev.elicitationId}`
    case "error":
      return `err-${ev.sessionId}`
    default:
      // 同一会话的「答完了」直接覆盖上一条：用户要的是最新结论，
      // 不是一串历史。
      return `turn-${ev.sessionId}`
  }
}

/**
 * 通知标题的文案 key。写成显式映射而不是拼字符串——i18n 的类型增强只认
 * 字面量，拼出来的 key 漏翻了要到运行时才发现。
 */
const TITLE_KEYS = {
  permission: "notify.permission",
  elicitation: "notify.elicitation",
  turn_end: "notify.turnEnd",
  error: "notify.error",
} as const

/** 会真正弹出去的那几类（撤回信号不在其中）。 */
type NotifiableEvent = keyof typeof TITLE_KEYS

function notifiable(event: NoticeEvent | undefined): event is NotifiableEvent {
  return event !== undefined && event in TITLE_KEYS
}

/** 这类通知用户开着吗。 */
function wanted(event: NoticeEvent | undefined): boolean {
  const prefs = loadNotifyPrefs()
  switch (event) {
    case "permission":
    case "elicitation":
      return prefs.decisions
    case "turn_end":
      return prefs.results
    case "error":
      return prefs.errors
    default:
      return false
  }
}

/**
 * 通知：把「会话上发生了事」变成一次恰当的打扰。
 *
 * 后端只广播发生了什么，判断留在这里——因为「该不该打扰」要知道用户此刻
 * 在看哪一页、页面在不在前台，只有前端有这个上下文（见 stream.Notice）。
 * 判断完再分两条路执行：桌面壳交给 macOS 系统通知，浏览器走页内提示。
 * 两端形式不同是刻意的：局域网访客根本拿不到系统通知（macOS 通知只发到
 * owner 那台机器，Web Notification 又要 secure context，局域网明文 http
 * 拿不到权限），页面自己够得着的只有 toast、标题和声音。
 */
export function useNotifications() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { pathname } = useLocation()

  useServerEvents((ev) => {
    if (ev.kind !== "notify" || !ev.sessionId) return

    // 撤回信号：这件事已经在页面上处理掉了，挂着的那条通知不该还在替一个
    // 结束了的请求要决定。
    if (ev.event === "permission_done" || ev.event === "elicitation_done") {
      const id = noticeID(ev)
      dismissNotice(id)
      if (isDesktop()) void desktopNotify.dismiss(id).catch(() => {})
      return
    }

    const event = ev.event
    if (!notifiable(event) || !wanted(event)) return
    // 用户正盯着这个会话——界面上已经有卡片、有正文了，再弹一次是重复打扰。
    if (isUserWatching() && pathname === `/sessions/${ev.sessionId}`) return

    const title = t(TITLE_KEYS[event])
    const id = noticeID(ev)
    const body = ev.text ?? ""
    const subtitle = ev.sessionTitle ?? ""

    // 先落进通知中心。它与「弹不弹」无关——弹出去的横幅是给此刻在看屏幕的
    // 人，留下的这条是给等会儿才回来的人，后者尤其是局域网访客的常态。
    pushNotice({
      id,
      kind: event,
      sessionId: ev.sessionId,
      sessionTitle: subtitle,
      text: body,
      at: Date.now(),
    })

    if (isDesktop()) {
      void desktopNotify
        .post({
          id,
          title,
          subtitle,
          body,
          // 同一会话归一组堆叠，长任务不会把通知中心刷满。
          threadId: `session-${ev.sessionId}`,
          // 决策通知上的按钮就是 agent 当场给的选项，按一下即裁决——
          // 用户不必先把窗口翻出来。
          actions:
            event === "permission"
              ? ev.options?.map((o) => ({
                  id: o.optionId,
                  title: o.name,
                  destructive: o.kind.startsWith("reject"),
                }))
              : undefined,
          userInfo: {
            sessionId: ev.sessionId,
            permissionId: ev.permissionId ?? "",
          },
        })
        .catch(() => {
          // 没授权就发不出去，这不是错误——设置页会告诉用户去开。
        })
      return
    }

    // 浏览器：界面提示只有侧栏的通知中心一处（toast 试过，弹一下就走、
    // 还盖内容，被砍掉了）。标题闪烁管标签页被切走的情况，声音管人在看
    // 别的窗口的情况。
    flashTitle(`● ${title}`)
    if (loadNotifyPrefs().sound) playChime()
  })

  // 人回到页面就停止闪烁——他已经看见了，再闪下去就是噪音。
  useEffect(() => {
    const settle = () => {
      if (isUserWatching()) stopFlashTitle()
    }
    window.addEventListener("focus", settle)
    document.addEventListener("visibilitychange", settle)
    return () => {
      window.removeEventListener("focus", settle)
      document.removeEventListener("visibilitychange", settle)
    }
  }, [])

  // 系统通知上的操作回到这里执行：裁决走前端已有的 API 客户端，壳不在
  // Swift 里重复实现一遍认证与请求。
  useEffect(() => {
    if (!isDesktop()) return

    const handle = (e: Event) => {
      const { actionId, userInfo } = (e as CustomEvent<NotificationAction>).detail
      const sessionId = Number(userInfo.sessionId ?? 0)
      if (!sessionId) return

      // 点通知本体 = 想去看看。窗口已由壳带到前台，这里负责落到那一页。
      if (actionId === DEFAULT_NOTIFICATION_ACTION) {
        void navigate(`/sessions/${sessionId}`)
        return
      }

      // 点的是决策按钮：actionId 就是 agent 给的 optionId。
      const permissionId = String(userInfo.permissionId ?? "")
      if (!permissionId) return
      api.sessions
        .resolvePermission(sessionId, permissionId, actionId)
        .catch((err: Error) => toast.error(err.message))
    }

    window.addEventListener(NOTIFICATION_ACTION_EVENT, handle)
    return () => window.removeEventListener(NOTIFICATION_ACTION_EVENT, handle)
  }, [navigate])
}
