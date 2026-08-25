import { Notification, shell } from "electron"

import { appBundlePath, inApplicationsDir } from "./launch-prefs.js"

/**
 * macOS 系统通知。只有桌面壳有这条路——局域网访客用浏览器，既收不到也发不出
 * 系统通知，他们那边走页内提示。
 *
 * 授权的坑（adr-004 实测，换 Electron 后复现确认，与引擎无关）：
 *
 *  1. **app 必须待在「应用程序」目录**。放在别处运行时通知直接失败
 *     （`UNErrorDomain Code=1`），**连系统弹窗都不出现**——看上去像什么都没
 *     发生，最容易误判成代码写错了。
 *  2. **一旦被拒**，再发也不会再弹窗。唯一出路是拉起系统设置让用户自己开。
 *
 * 与 Swift 版的能力差异（见 ADR-015）：
 *
 *  - **查不到授权状态**。Electron 没有 `getNotificationSettings` 的等价物，
 *    这里不假装知道——按「发过没有、发成功没有」推断，从没发过就是
 *    notDetermined。请求授权也没有独立 API，只能靠发一条通知让系统弹框，
 *    所以 `request()` 就是发一条示例通知。
 *  - **没有 threadIdentifier**，同一会话的通知不再堆叠成组。
 */

/** 点通知本体（而不是某个按钮）时回传的 action id，与 web 端常量对齐。 */
const DEFAULT_ACTION = "com.apple.UNNotificationDefaultActionIdentifier"

/** 通知留存上限：留着只为支持 dismiss，早已消失的没必要占内存。 */
const KEEP_LIMIT = 64

class Notifier {
  constructor() {
    /** 通知上的操作回传给前端。 */
    this.onAction = null
    /** 点了通知本体：把窗口带到前台，用户接下来要看的就是那一页。 */
    this.onActivate = null
    /** id → Notification，供 dismiss 用。 */
    this.live = new Map()
    /** 推断授权状态用：null=没发过，true=发成功过，false=发失败过。 */
    this.lastDelivered = null
  }

  /** 当前授权状态，供设置页如实显示。拿不到真值时诚实地报 unknown/notDetermined。 */
  status() {
    let state = "notDetermined"
    if (this.lastDelivered === true) state = "authorized"
    else if (this.lastDelivered === false) state = "denied"
    return {
      status: Notification.isSupported() ? state : "denied",
      // 没发过就还没被问过，发一条就会弹框；已经拒过的再发也不弹，只能去系统设置。
      canRequest: this.lastDelivered === null,
      bundlePath: appBundlePath() ?? "",
      // app 不在「应用程序」目录时授权会静默失败，设置页要能当场说明白。
      inApplicationsDir: inApplicationsDir(),
    }
  }

  /**
   * 「请求授权」= 发一条示例通知。Electron 没有独立的授权 API，而系统正是在
   * 首次发送时弹框的——用一条看得懂的通知去触发它，比发一条空通知诚实。
   */
  async requestAuthorization() {
    const ok = await new Promise((resolve) => {
      const n = new Notification({
        title: "ACPP 通知已开启",
        body: "有事等你处理时会在这里提醒你。",
      })
      let settled = false
      const done = (value) => {
        if (settled) return
        settled = true
        resolve(value)
      }
      n.on("show", () => done(true))
      n.on("failed", () => done(false))
      n.show()
      // 两种事件都没来就当没成——不吊着设置页的开关等下去。
      setTimeout(() => done(false), 2000)
    })
    this.lastDelivered = ok
    const result = this.status()
    if (!ok) result.error = "系统没有接受这条通知，可能未获授权"
    return result
  }

  /** 拉起系统设置里本 app 的通知面板。被拒之后这是唯一的出路。 */
  async openSettings() {
    // macOS 13+ 是 Notifications-Settings.extension，老系统是
    // com.apple.preference.notifications——都试一遍，谁开得了算谁的。
    for (const url of [
      "x-apple.systempreferences:com.apple.Notifications-Settings.extension",
      "x-apple.systempreferences:com.apple.preference.notifications",
    ]) {
      try {
        await shell.openExternal(url)
        return { opened: true }
      } catch {
        // 试下一个
      }
    }
    return { opened: false }
  }

  /**
   * 发一条通知。`actions` 非空时通知上带按钮——决策通知的按钮就是 agent 给的
   * 选项，按下去等于当场裁决。
   *
   * 只取前 4 个：macOS 通知展开后放得下的按钮有限，再多也点不到，而选项列表
   * 在页面上的卡片里是完整的——通知是快捷方式，不是替代品。
   */
  post({ id, title, subtitle, body, actions = [], userInfo = {} }) {
    if (!Notification.isSupported()) return { posted: false }

    const buttons = actions.slice(0, 4).filter((a) => a?.id && a?.title)
    const n = new Notification({
      title,
      subtitle: subtitle || undefined,
      body: body || "",
      actions: buttons.map((a) => ({ type: "button", text: a.title })),
    })

    n.on("show", () => {
      this.lastDelivered = true
    })
    n.on("failed", () => {
      this.lastDelivered = false
    })

    // 点通知本体 = 想去看看，把窗口带到前台；点具体按钮 = 就地裁决，
    // 不打断用户手头的事。
    n.on("click", () => {
      this.onActivate?.()
      this.onAction?.({ notificationId: id, actionId: DEFAULT_ACTION, userInfo })
    })
    n.on("action", (_event, index) => {
      const picked = buttons[index]
      if (!picked) return
      this.onAction?.({ notificationId: id, actionId: picked.id, userInfo })
    })
    // 划掉/超时消失不是一个决定，别让它触发任何动作。
    n.on("close", () => this.live.delete(id))

    this.remember(id, n)
    n.show()
    return { posted: true }
  }

  /**
   * 撤回一条通知：这件事已经在界面上处理掉了（比如权限在页面上裁决过），
   * 通知再挂在那儿就是在骗人。
   */
  dismiss(id) {
    const n = this.live.get(id)
    if (n) {
      n.close()
      this.live.delete(id)
    }
    return { dismissed: true }
  }

  remember(id, n) {
    this.live.set(id, n)
    while (this.live.size > KEEP_LIMIT) {
      const oldest = this.live.keys().next().value
      this.live.delete(oldest)
    }
  }
}

export const notifier = new Notifier()
