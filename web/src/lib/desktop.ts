/**
 * 桌面壳（macOS app）的原生通道。
 *
 * 这里的能力都碰的是**这台机器**而不是服务端：开机启动改的是登录项，系统
 * 通知发的是 macOS 通知中心，后端 API 都够不着，所以走 WKWebView 注入的
 * 消息通道而不是 `lib/api.ts`。浏览器里这些能力不存在，`isDesktop()` 恒为
 * false，相关设置项整块不渲染，通知改走页内提示（见 lib/notify.ts）。
 */

/** 壳在每个页面注入的标记。 */
interface DesktopWindow extends Window {
  __ACPP_DESKTOP__?: boolean
  webkit?: {
    messageHandlers?: {
      acppDesktop?: { postMessage: (body: unknown) => Promise<unknown> }
    }
  }
}

/** 当前是否跑在桌面壳里。浏览器（含局域网访客）恒为 false。 */
export function isDesktop(): boolean {
  if (typeof window === "undefined") return false
  return (window as DesktopWindow).__ACPP_DESKTOP__ === true
}

/** 启动偏好快照。`error` 非空表示上一次变更被系统拒绝，值已回读为真实状态。 */
export interface LaunchPrefs {
  openAtLogin: boolean
  startMinimized: boolean
  error?: string
}

async function bridge<T>(body: Record<string, unknown>): Promise<T> {
  const handler = (window as DesktopWindow).webkit?.messageHandlers?.acppDesktop
  if (!handler) throw new Error("desktop bridge unavailable")
  return (await handler.postMessage(body)) as T
}

const call = (action: string, value?: boolean) =>
  bridge<LaunchPrefs>(value === undefined ? { action } : { action, value })

/**
 * 每次调用都回读壳里的真实状态：开机启动可能被系统拒绝（未签名、不在
 * 「应用程序」目录），前端不能想当然地乐观更新。
 */
export const desktopLaunch = {
  get: () => call("get"),
  setOpenAtLogin: (on: boolean) => call("setOpenAtLogin", on),
  setStartMinimized: (on: boolean) => call("setStartMinimized", on),
}

/**
 * 通知授权状态，由壳如实回报（见 desktop/macos/Sources/Notifier.swift）。
 *
 * 判断能不能发通知只看 `status`——被拒的 app 上系统的 alertSetting 照样是
 * enabled（实测），拿它判断会得到相反的结论。
 */
export interface NotifyStatus {
  status: "notDetermined" | "denied" | "authorized" | "provisional" | "unknown"
  /** 只有「还没问过」时系统才会弹授权框；被拒之后只能引导去系统设置。 */
  canRequest: boolean
  bundlePath: string
  /**
   * app 是否待在「应用程序」目录。**不在那儿授权会静默失败**：请求直接
   * 返回错误、连系统弹窗都不出现，状态停在 notDetermined，看着像什么都
   * 没发生——设置页必须当场把这件事说清楚。
   */
  inApplicationsDir: boolean
  error?: string
}

/** 要发给系统通知中心的一条。actions 非空时通知上会带按钮。 */
export interface DesktopNotice {
  id: string
  title: string
  subtitle?: string
  body?: string
  /** 同一会话的通知归一组堆叠，长任务不会把通知中心刷满。 */
  threadId?: string
  actions?: { id: string; title: string; destructive?: boolean }[]
  userInfo?: Record<string, unknown>
}

/** 壳把通知上的操作交回来时，在 window 上派发的事件名。 */
export const NOTIFICATION_ACTION_EVENT = "acpp:notification-action"

/** 点通知本体（而不是某个按钮）时 macOS 给的 action id。 */
export const DEFAULT_NOTIFICATION_ACTION =
  "com.apple.UNNotificationDefaultActionIdentifier"

/** 一次通知交互的回传。userText 只有带输入框的通知才有。 */
export interface NotificationAction {
  notificationId: string
  actionId: string
  userInfo: Record<string, unknown>
  userText?: string
}

/**
 * 系统通知。授权刻意**不在启动时索要**——那时用户还没见过这个 app 会通知
 * 什么，弹出来只会被随手拒掉，而一旦被拒就再也弹不出来了（见 Notifier）。
 */
export const desktopNotify = {
  status: () => bridge<NotifyStatus>({ action: "notificationStatus" }),
  request: () => bridge<NotifyStatus>({ action: "requestNotification" }),
  openSettings: () =>
    bridge<{ opened: boolean }>({ action: "openNotificationSettings" }),
  post: (notice: DesktopNotice) =>
    bridge<{ posted: boolean }>({ action: "notify", ...notice }),
  dismiss: (id: string) =>
    bridge<{ dismissed: boolean }>({ action: "dismissNotify", id }),
}
