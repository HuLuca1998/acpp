import path from "node:path"
import { fileURLToPath } from "node:url"

import { BrowserWindow, shell } from "electron"

import { prefs } from "./prefs.js"

const here = path.dirname(fileURLToPath(import.meta.url))

/**
 * 窗口底色。**这不是外观选项，是性能开关**——不设 backgroundColor 时窗口按带
 * alpha 的图层合成，拖窗口边框每帧都要做透明混合，卡到不能用（ADR-015 有实测
 * 对比）。取值与 web 端深色主题的 --background 对齐，避免加载瞬间闪一下。
 */
const BACKGROUND = "#0B0F0D"

/**
 * 主窗口：一块 BrowserWindow 加载本机服务。关闭按钮 = 隐藏（app 常驻菜单栏），
 * 站外链接与 target=_blank 转交系统浏览器。
 */
export class MainWindow {
  constructor() {
    const bounds = prefs.windowBounds
    this.win = new BrowserWindow({
      ...bounds,
      minWidth: 900,
      minHeight: 600,
      title: "ACPP",
      backgroundColor: BACKGROUND,
      show: false,
      // 无标题栏：那条系统标题栏整个消失，界面自己的顶栏顶到窗口最上沿，
      // 红绿灯浮在它左端。**没用 frame:false**——hiddenInset 保留了系统的
      // 拖动、边缘缩放、双击顶部最大化、全屏与窗口吸附，前端只要把顶栏空白
      // 标成 `-webkit-app-region: drag`（见 web/src/index.css 的 .drag-region）。
      // 自绘三颗按钮换不来任何视觉收益，却要把这些系统行为逐个补回来。
      titleBarStyle: "hiddenInset",
      // 与前端 --titlebar-height(40px) 对齐：y=14 让 12px 的按钮在这条里居中，
      // x=14 使按钮占到 66px，前端据此左让 --titlebar-inset(76px)。改一处必须改另一处。
      trafficLightPosition: { x: 14, y: 14 },
      webPreferences: {
        preload: path.join(here, "../preload/index.cjs"),
      },
    })
    if (!bounds.x && !bounds.y) this.win.center()

    // resize 期间的窗口遮挡计算是白费功夫，且会跟着掉帧（ADR-015）。
    this.win.webContents.setBackgroundThrottling(false)

    // 关闭 = 隐藏。真退出由 AppShell 置 allowClose 后放行，那时不能再拦。
    this.allowClose = false
    this.win.on("close", (e) => {
      if (this.allowClose) return
      e.preventDefault()
      this.hide()
    })

    this.win.on("resize", () => this.rememberBounds())
    this.win.on("move", () => this.rememberBounds())

    // target=_blank 没有目标 frame，Electron 默认吞掉——转交系统浏览器
    this.win.webContents.setWindowOpenHandler(({ url }) => {
      shell.openExternal(url)
      return { action: "deny" }
    })

    // 站外 http(s) 链接同样交给浏览器，窗口里只住本机服务
    this.win.webContents.on("will-navigate", (e, url) => {
      let host
      try {
        const parsed = new URL(url)
        if (!parsed.protocol.startsWith("http")) return
        host = parsed.hostname
      } catch {
        return
      }
      if (host === "127.0.0.1" || host === "localhost") return
      e.preventDefault()
      shell.openExternal(url)
    })
  }

  get isVisible() {
    return this.win.isVisible()
  }

  show() {
    this.win.show()
    this.win.focus()
  }

  hide() {
    this.win.hide()
  }

  toggle() {
    if (this.isVisible) this.hide()
    else this.show()
  }

  /** 窗口位置与大小记在偏好里，下次开在原地（对应 Swift 的 setFrameAutosaveName）。 */
  rememberBounds() {
    if (this.win.isMinimized() || this.win.isFullScreen()) return
    prefs.windowBounds = this.win.getBounds()
  }

  // MARK: - 内容

  /**
   * 主文档强制条件请求（304 才复用缓存）：更新重启后必须拿到新入口页，
   * 否则缓存会把旧版界面又端出来。
   */
  loadApp(url) {
    this.win.loadURL(url, { extraHeaders: "pragma: no-cache\n" })
  }

  /**
   * 让已加载的界面切到某个路由（菜单栏「查看全部 issue」用）。前端是 SPA，
   * pushState + popstate 就能让路由器跟上，不必整页重载；页面还没加载好
   * 或根本不是我们的界面时才走 loadApp。
   */
  navigate(appURL, route) {
    const wc = this.win.webContents
    if (wc.isLoading() || !wc.getURL().startsWith(appURL)) {
      this.loadApp(appURL + route.replace(/^\//, ""))
      return
    }
    const path = JSON.stringify(route)
    void wc.executeJavaScript(
      `history.pushState(null, "", ${path}); dispatchEvent(new PopStateEvent("popstate"));`
    )
  }

  showLoading() {
    this.loadPage({ mark: "❯_", markPulse: true, title: "正在启动 ACPP 服务…", detail: "" })
  }

  showFailure(logPath) {
    this.loadPage({
      mark: "✕",
      markPulse: false,
      title: "服务启动失败",
      detail: `可从菜单栏图标 → 重启服务 重试；日志：${logPath}`,
    })
  }

  loadPage({ mark, markPulse, title, detail }) {
    // 整页可拖：无标题栏窗口在前端加载完之前没有任何 drag 区域，不标这一下
    // 启动那几秒窗口是钉死的。页里没有可点元素，全页 drag 不影响任何操作。
    const html = `<!doctype html><meta charset="utf-8"><style>
      html,body{height:100%;margin:0;background:${BACKGROUND};color:#D1FAE5;
        -webkit-app-region:drag;
        font:15px -apple-system,'PingFang SC',sans-serif;
        display:flex;align-items:center;justify-content:center}
      .wrap{text-align:center;max-width:520px;padding:0 24px}
      .mark{font-size:44px;color:#4ADE80;${markPulse ? "animation:pulse 1.2s ease-in-out infinite" : ""}}
      @keyframes pulse{50%{opacity:.35}}
      p{color:#8BA396;margin-top:14px;line-height:1.7;word-break:break-all}
      </style><div class="wrap"><div class="mark">${mark}</div>
      <p><b style="color:#D1FAE5">${title}</b><br>${detail}</p></div>`
    this.win.loadURL("data:text/html;charset=utf-8," + encodeURIComponent(html))
  }

  /**
   * 把通知上的操作交回前端。裁决与回答都走前端已有的 API 客户端，壳不在
   * 主进程里再实现一遍认证与请求——那会变成第二份要同步维护的契约。
   *
   * 页面还没加载好就丢掉：那种情况下用户点的是通知本体，窗口已经被带到
   * 前台，他在界面上照样能处理，比让壳去猜要可靠。
   */
  deliverNotificationAction(payload) {
    if (this.win.webContents.isLoading()) return
    this.win.webContents.send("acpp:notification-action", payload)
  }
}
