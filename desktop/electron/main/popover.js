import path from "node:path"
import { fileURLToPath } from "node:url"

import { BrowserWindow, clipboard, ipcMain, screen } from "electron"

import { openURL } from "./browser.js"
import { prefs } from "./prefs.js"

const here = path.dirname(fileURLToPath(import.meta.url))

export const CHANNEL = "acpp:tray"
export const SNAPSHOT_EVENT = "acpp:tray:snapshot"

/** 弹层宽度：与 codex-ui 的菜单同一量级，一行放得下「点 · 编号 · 仓库 · 状态 · 标题 · 按钮」。 */
const WIDTH = 480
/** 高度上限：再多就该去 /github 页翻了，弹层内部滚动兜底。 */
const MAX_HEIGHT = 640
/** 与菜单栏图标的间距。 */
const GAP = 6

/**
 * 托盘左键弹出的「我的 issue」面板。Electron 的原生菜单只能放文字，画不出
 * codex-ui 那种带色点、状态徽标、行内按钮、点编号闪「已复制」的行，所以自绘：
 * 一块无边框、透明、`vibrancy: "menu"` 的 NSPanel 贴在图标下方，页面是
 * popover/ 下的静态 HTML，数据经 preload 的窄通道从壳里拿。
 *
 * 用 `type: "panel"`（非激活式 NSPanel）而不是普通窗口：弹出时不激活整个
 * app，主窗口不会被一起拽到前台；失焦即收起，与系统菜单的手感一致。
 */
export class PopoverWindow {
  constructor({ issues, shell }) {
    this.issues = issues
    this.shell = shell
    this.win = null
    /** 最近一次因失焦收起的时刻：点图标时先来 blur 再来 click，靠它区分「点图标关掉」与「点图标打开」。 */
    this.hiddenAt = 0
    this.installIPC()
    this.issues.onChange = () => this.pushSnapshot()
  }

  ensureWindow() {
    if (this.win && !this.win.isDestroyed()) return this.win
    const win = new BrowserWindow({
      width: WIDTH,
      height: 200,
      show: false,
      frame: false,
      transparent: true,
      resizable: false,
      movable: false,
      minimizable: false,
      maximizable: false,
      fullscreenable: false,
      skipTaskbar: true,
      hiddenInMissionControl: true,
      alwaysOnTop: true,
      type: "panel",
      vibrancy: "menu",
      visualEffectState: "active",
      webPreferences: {
        preload: path.join(here, "../preload/popover.cjs"),
      },
    })
    win.setAlwaysOnTop(true, "pop-up-menu")
    win.setVisibleOnAllWorkspaces(true, { visibleOnFullScreen: true })
    win.on("blur", () => {
      this.hiddenAt = Date.now()
      win.hide()
    })
    win.webContents.on("did-finish-load", () => this.pushSnapshot())
    win.loadFile(path.join(here, "../popover/index.html"))
    this.win = win
    return win
  }

  get isVisible() {
    return Boolean(this.win && !this.win.isDestroyed() && this.win.isVisible())
  }

  /** @param {Electron.Rectangle} trayBounds 托盘图标的位置，click 事件带来的。 */
  toggle(trayBounds) {
    if (this.isVisible) {
      this.hide()
      return
    }
    // 点图标关面板：面板先失焦收起，紧跟着才轮到这个 click——那不是要再打开。
    if (Date.now() - this.hiddenAt < 300) return
    this.show(trayBounds)
  }

  show(trayBounds) {
    const win = this.ensureWindow()
    this.trayBounds = trayBounds
    this.place(win.getSize()[1])
    void this.issues.refresh()
    this.pushSnapshot()
    win.show()
    win.focus()
  }

  hide() {
    if (this.isVisible) this.win.hide()
  }

  /** 贴在图标正下方居中，不出屏幕。 */
  place(height) {
    const win = this.win
    const b = this.trayBounds ?? { x: 0, y: 0, width: 0, height: 22 }
    const area = screen.getDisplayNearestPoint({ x: b.x, y: b.y }).workArea
    const h = Math.min(height, MAX_HEIGHT)
    let x = Math.round(b.x + b.width / 2 - WIDTH / 2)
    x = Math.max(area.x + 4, Math.min(x, area.x + area.width - WIDTH - 4))
    const y = Math.round(b.y + b.height + GAP)
    win.setBounds({ x, y, width: WIDTH, height: h })
  }

  snapshot() {
    return { ...this.issues.snapshot, running: this.issues.server.state === "running" }
  }

  pushSnapshot() {
    if (!this.win || this.win.isDestroyed() || this.win.webContents.isLoading()) return
    this.win.webContents.send(SNAPSHOT_EVENT, this.snapshot())
  }

  /** 页面 → 壳：动作都很小，一个 channel 按 action 分发，与 bridge.js 同一风格。 */
  installIPC() {
    ipcMain.handle(CHANNEL, async (_event, body) => {
      switch (body?.action) {
        case "get":
          return this.snapshot()
        case "copy":
          clipboard.writeText(String(body.text ?? ""))
          return null
        case "open":
          if (body.url) openURL(String(body.url), prefs.chromeProfile)
          this.hide()
          return null
        case "refresh":
          await this.issues.refresh({ force: true })
          return this.snapshot()
        case "openApp":
          this.hide()
          this.shell.showMainWindow(body.route ? String(body.route) : undefined)
          return null
        case "size":
          if (this.win && Number.isFinite(body.height)) this.place(Math.ceil(body.height))
          return null
        case "hide":
          this.hide()
          return null
        default:
          throw new Error(`tray: 未知 action ${body?.action}`)
      }
    })
  }

  destroy() {
    if (this.win && !this.win.isDestroyed()) this.win.destroy()
    this.win = null
  }
}
