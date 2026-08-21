import { flushSync } from "react-dom"

import type { NoticeEvent } from "@/types/acp"

/**
 * 通知的种类。`update` 是前端自己产生的（版本哨兵发现后端换了版本），
 * 其余几种来自后端广播——但对用户来说都是「有件事等你处理」，摆在一起才
 * 有意义，所以并进同一个列表。
 */
export type NoticeKind = NoticeEvent | "update"

/** 通知中心里的一条。 */
export interface Notice {
  /** 与系统通知同一个 id，处理完能精确撤回（见 use-notifications）。 */
  id: string
  kind: NoticeKind
  /** 会话相关的通知才有；版本更新不属于任何会话。 */
  sessionId?: number
  sessionTitle?: string
  text: string
  /** 收到的时刻，列表里显示成相对时间。 */
  at: number
}

/** 保留上限。再多也没人会翻，只会让侧栏那块越来越沉。 */
const LIMIT = 20

/**
 * 优先级：数字小的排前面，同级按时间倒序。
 *
 * 排序即展示策略——折叠态露出的永远是列表第一条，所以这里定的是「多件事
 * 同时等着时，先让用户看见哪件」：更新压倒一切（不刷新，看到的一切都是
 * 旧的，处理别的事反而危险）；其次是 agent 停着等人的决策与问答（它们在
 * 阻塞任务）；出错要人知道但不阻塞谁；答完了只是个好消息。
 */
const PRIORITY: Record<NoticeKind, number> = {
  update: 0,
  permission: 1,
  elicitation: 1,
  error: 2,
  turn_end: 3,
  // 撤回信号不入列表，给个垫底值让类型收口。
  permission_done: 9,
  elicitation_done: 9,
}

const byPriority = (a: Notice, b: Notice) =>
  PRIORITY[a.kind] - PRIORITY[b.kind] || b.at - a.at

/**
 * 通知中心的存量。
 *
 * 为什么要留着：toast 是一闪而过的横幅，人不在的时候等于没发生过。而局域网
 * 访客可能几十分钟才回来看一眼，回来时最该看见的恰恰是「我不在的时候都发生
 * 了什么」——尤其是有 agent 正停在那儿等他决策。这正是 iOS 的分工：横幅转瞬
 * 即逝，通知中心留着等你处理。
 *
 * 为什么不落 storage：刷新之后 pending 的决策请求多半已经失效，端出一列点了
 * 没反应的旧通知比没有更糟。真正还悬着的事，会话页里的卡片才是事实源。
 *
 * 做成模块级 store 而不是 context：写入方是 shell 上的通知 hook，读取方是
 * 侧栏组件，两者隔着整棵树；而项目不引入全局状态库（web/AGENTS.md §4），
 * 于是照 session-activity.ts 的先例走「模块级广播 + useSyncExternalStore」。
 */
let notices: Notice[] = []
const listeners = new Set<() => void>()

function commit(next: Notice[]) {
  // 必须换新数组引用，useSyncExternalStore 才认得出变化。
  notices = next
  const notifyAll = () => {
    for (const fn of [...listeners]) fn()
  }
  // 列表重排走 View Transitions：新通知按优先级插进中间、关一条后下一条
  // 补位，其余卡的让位由浏览器补成平滑位移（每张卡有自己的
  // view-transition-name）。flushSync 是这个 API 的契约——DOM 必须在快照
  // 回调内同步更新完。不支持的浏览器直接瞬时完成，不是错误。
  if (typeof document !== "undefined" && "startViewTransition" in document) {
    ;(
      document as Document & {
        startViewTransition: (fn: () => void) => void
      }
    ).startViewTransition(() => flushSync(notifyAll))
  } else {
    notifyAll()
  }
}

export function pushNotice(notice: Notice) {
  // 同 id 覆盖：同一个请求重来一次不该在列表里排两行。
  // 排序后再裁尾：挤出去的自然是最低优先级里最旧的那条。
  const next = [notice, ...notices.filter((n) => n.id !== notice.id)]
  next.sort(byPriority)
  commit(next.slice(0, LIMIT))
}

/** 这件事处理完了（在页面上裁决过，或用户划掉了）。 */
export function dismissNotice(id: string) {
  if (!notices.some((n) => n.id === id)) return
  commit(notices.filter((n) => n.id !== id))
}

export function clearNotices() {
  if (notices.length === 0) return
  commit([])
}

export function getNotices(): Notice[] {
  return notices
}

export function subscribeNotices(fn: () => void): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}
