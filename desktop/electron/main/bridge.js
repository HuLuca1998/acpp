import { ipcMain } from "electron"

import { launchPrefs } from "./launch-prefs.js"
import { notifier } from "./notifier.js"

/**
 * 页面 ↔ 壳的窄通道：设置页要的原生开关，加上系统通知的收发。
 *
 * 用 `ipcMain.handle`（请求-响应）而不是单向消息：开机启动可能被系统拒绝
 * （未签名、不在「应用程序」目录），前端必须拿到真实结果才能把开关弹回去，
 * 而不是假装成功。每次变更后回读一份快照，前端据此对齐真实状态。
 *
 * action 名与返回形状**必须**与 web/src/lib/desktop.ts 保持一致，那是跨端契约。
 */

export const CHANNEL = "acpp:desktop"

export function installBridge() {
  ipcMain.handle(CHANNEL, async (_event, body) => {
    const action = body?.action
    if (!action) throw new Error("bridge: 需要 {action}")

    switch (action) {
      case "get":
        return snapshot()

      case "setOpenAtLogin": {
        if (typeof body.value !== "boolean") throw new Error("bridge: setOpenAtLogin 缺少 value")
        // 失败不抛错——把原因连同真实状态一起回去，让设置页就地说明。
        const reason = launchPrefs.setOpenAtLogin(body.value)
        const result = snapshot()
        if (reason) result.error = reason
        return result
      }

      case "setStartMinimized": {
        if (typeof body.value !== "boolean") throw new Error("bridge: setStartMinimized 缺少 value")
        launchPrefs.startMinimized = body.value
        return snapshot()
      }

      // 通知的三件事：查状态、请求授权、拉起系统设置。设置页要如实显示现状——
      // 被拒之后请求授权不会再弹窗，那时唯一能做的就是把人送去系统设置。
      case "notificationStatus":
        return notifier.status()

      case "requestNotification":
        return notifier.requestAuthorization()

      case "openNotificationSettings":
        return notifier.openSettings()

      case "notify": {
        if (!body.id || !body.title) throw new Error("bridge: notify 需要 {id, title}")
        return notifier.post(body)
      }

      // 这件事已经在页面上处理掉了，通知别再挂着骗人。
      case "dismissNotify": {
        if (!body.id) throw new Error("bridge: dismissNotify 缺少 id")
        return notifier.dismiss(body.id)
      }

      default:
        throw new Error(`bridge: 未知动作 ${action}`)
    }
  })
}

function snapshot() {
  return {
    openAtLogin: launchPrefs.openAtLogin,
    startMinimized: launchPrefs.startMinimized,
  }
}
