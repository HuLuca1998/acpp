import path from "node:path"

import { app } from "electron"

import { prefs } from "./prefs.js"

/**
 * 启动偏好：开机自启、开机静默驻留。
 *
 * 自启读的是系统里的真实注册状态，不自己记一份——用户随时可能在
 * 「系统设置 › 通用 › 登录项」里改，本地缓存只会和事实对不上。
 */

export const launchPrefs = {
  get openAtLogin() {
    return app.getLoginItemSettings().openAtLogin === true
  },

  /**
   * 设置开机自启，成功返回 null，失败返回给人看的原因。
   *
   * Electron 的 setLoginItemSettings 不报错（Swift 那边 SMAppService.register()
   * 会抛），只能设完回读比对。未签名或不在「应用程序」目录的 app 常被系统拒，
   * 那正是回读对不上的典型情形，所以提示照搬 Swift 版那条。
   */
  setOpenAtLogin(on) {
    try {
      app.setLoginItemSettings({ openAtLogin: on })
    } catch (err) {
      return err?.message ?? String(err)
    }
    if (this.openAtLogin === on) return null
    return inApplicationsDir()
      ? "系统拒绝了这次登录项变更"
      : "系统拒绝了这次登录项变更——app 不在「应用程序」文件夹里常会被拒，" +
          "可以把 ACPP 移过去后重试，或直接在「系统设置 › 通用 › 登录项」里改"
  },

  /**
   * 开机最小化：启动后只驻留菜单栏，不显示主窗口也不占 Dock。
   * 只管**启动那一下**——用户之后手动打开窗口，就是一个正常 app。
   */
  get startMinimized() {
    return prefs.startMinimized
  },
  set startMinimized(on) {
    prefs.startMinimized = on
  },
}

/** app 是否待在「应用程序」目录（系统级或用户级）。不在那儿登录项与通知都容易被拒。 */
export function inApplicationsDir() {
  const bundle = appBundlePath()
  if (!bundle) return false
  return (
    bundle.startsWith("/Applications/") ||
    bundle.startsWith(path.join(app.getPath("home"), "Applications") + "/")
  )
}

/** 当前 .app 的路径；开发态（没打包）返回 null。 */
export function appBundlePath() {
  if (!app.isPackaged) return null
  // <bundle>/Contents/MacOS/ACPP → <bundle>
  return path.resolve(path.dirname(process.execPath), "../..")
}
