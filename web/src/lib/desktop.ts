/**
 * 桌面壳（macOS app）的原生通道。
 *
 * 开机启动改的是**这台机器的登录项**，不是服务端配置——后端 API 碰不到它，
 * 所以走 WKWebView 注入的消息通道而不是 `lib/api.ts`。浏览器里这些能力不
 * 存在，`isDesktop()` 恒为 false，相关设置项整块不渲染。
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

async function call(action: string, value?: boolean): Promise<LaunchPrefs> {
  const handler = (window as DesktopWindow).webkit?.messageHandlers?.acppDesktop
  if (!handler) throw new Error("desktop bridge unavailable")
  const result = await handler.postMessage(
    value === undefined ? { action } : { action, value }
  )
  return result as LaunchPrefs
}

/**
 * 每次调用都回读壳里的真实状态：开机启动可能被系统拒绝（未签名、不在
 * 「应用程序」目录），前端不能想当然地乐观更新。
 */
export const desktopLaunch = {
  get: () => call("get"),
  setOpenAtLogin: (on: boolean) => call("setOpenAtLogin", on),
  setStartMinimized: (on: boolean) => call("setStartMinimized", on),
}
