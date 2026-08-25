import { BrowserWindow, app, clipboard, shell } from "electron"

import { installBridge } from "./bridge.js"
import { installMainMenu } from "./menu.js"
import { launchPrefs } from "./launch-prefs.js"
import { notifier } from "./notifier.js"
import { LOG_PATH, ServerController } from "./server.js"
import { MainWindow } from "./window.js"
import { TrayController } from "./tray.js"

/**
 * 装配层 + 生命周期裁决：谁能真正退出这个 app，是这里唯一的复杂逻辑。
 * 产品约定（沿用 ADR-004）：关闭窗口/Cmd+Q/Dock 退出都只是隐藏窗口，服务常驻
 * 菜单栏；真退出只有两条路——菜单栏右键「退出」，或收到 SIGTERM（自更新）。
 *
 * 与 Swift 版的一处已知差别：那边靠读 Quit AppleEvent 的 `why?` 参数放行系统
 * 注销/关机，Electron 没有等价 API。注销时系统会提示「应用阻止了注销」，需要
 * 用户确认一下——这是权衡后接受的代价（见 ADR-015）。
 */

// 窗口遮挡计算在 resize 期间反复触发且跟着掉帧，必须在 ready 之前关掉（ADR-015）。
app.commandLine.appendSwitch("disable-features", "CalculateNativeWinOcclusion")

class AppShell {
  constructor() {
    this.server = new ServerController()
    this.window = null
    this.tray = null
    /** 置真后才允许真正退出，且退出路径不可重入。 */
    this.quitting = false
  }

  async start() {
    installMainMenu({ onCloseWindow: () => this.window?.hide() })
    installBridge()
    this.window = new MainWindow()
    this.tray = new TrayController({ server: this.server, shell: this })
    this.installSignalHandlers()

    // 通知：这里只接线，**不请求授权**——启动就弹系统授权框是最招人烦的做法，
    // 而且用户还没见过这个 app 会通知什么。授权由设置页里的开关发起。
    notifier.onActivate = () => this.showMainWindow()
    notifier.onAction = (payload) => this.window?.deliverNotificationAction(payload)

    // 开机最小化：只驻留菜单栏，窗口不弹、Dock 不占。服务照常起——用户从
    // 菜单栏打开时窗口里应该已经是可用的界面，而不是现加载。
    if (launchPrefs.startMinimized) {
      app.dock?.hide()
    } else {
      this.window.showLoading()
      this.window.show()
    }

    const ok = await this.server.start()
    this.applyServerResult(ok)
  }

  applyServerResult(ok) {
    if (ok) this.window.loadApp(this.server.localURL)
    else this.window.showFailure(LOG_PATH)
  }

  // MARK: - 菜单栏动作

  toggleMainWindow() {
    this.restoreRegularActivation()
    this.window.toggle()
  }

  showMainWindow() {
    this.restoreRegularActivation()
    this.window.show()
  }

  /**
   * 静默启动后第一次亮出窗口时切回正常 app：Dock 图标回来。
   * 切回后不再切走——开机最小化只管开机那一下。
   */
  restoreRegularActivation() {
    if (app.dock && !app.dock.isVisible()) app.dock.show()
  }

  openInBrowser() {
    shell.openExternal(this.server.localURL)
  }

  /**
   * 切换局域网共享要换监听地址，只能重启服务进程。上下文在 agent 侧持久化，
   * 会话续聊时 session/load 无感恢复，代价可接受。
   */
  async toggleLanShare() {
    this.server.lanShareEnabled = !this.server.lanShareEnabled
    await this.restartServer()
  }

  copyLanLink() {
    const url = this.server.lanURL
    if (!url) {
      shell.beep()
      return
    }
    clipboard.writeText(url)
  }

  get openAtLogin() {
    return launchPrefs.openAtLogin
  }

  toggleOpenAtLogin() {
    const reason = launchPrefs.setOpenAtLogin(!launchPrefs.openAtLogin)
    if (reason) console.error("切换开机自启失败:", reason)
  }

  get startMinimized() {
    return launchPrefs.startMinimized
  }

  toggleStartMinimized() {
    launchPrefs.startMinimized = !launchPrefs.startMinimized
  }

  async restartServer() {
    this.window.showLoading()
    const ok = await this.server.restart()
    this.applyServerResult(ok)
  }

  openServerLog() {
    shell.openPath(LOG_PATH)
  }

  // MARK: - 退出

  /**
   * 真退出。必须回收 acp-server（连带它的 agent 子进程），不留孤儿。
   *
   * 用 app.exit() 而不是 app.quit()：自更新的流程是「先把 .app 换成新版 →
   * 再给壳发 TERM」，此刻 bundle 里的 asar 已经不是我们启动时那份，走完整
   * 退出生命周期可能触发新的磁盘读取。停完子进程立刻硬退，最稳。
   */
  async requestRealQuit() {
    if (this.quitting) return
    this.quitting = true
    try {
      await this.server.stop()
    } catch (err) {
      console.error("停止 acp-server 失败:", err)
    }
    this.tray?.destroy()
    app.exit(0)
  }

  /** SIGTERM/SIGINT（自更新、终端 kill）走真退出，保证服务子进程被回收。 */
  installSignalHandlers() {
    for (const sig of ["SIGTERM", "SIGINT"]) {
      process.on(sig, () => {
        this.requestRealQuit()
      })
    }
  }
}

// MARK: - 启动

const shellApp = new AppShell()

// 开发态 `npm start` 可能多开，打包态由 LaunchServices 保证单实例。
if (!app.requestSingleInstanceLock()) {
  app.exit(0)
} else {
  app.on("second-instance", () => shellApp.showMainWindow())

  app.whenReady().then(() => {
    shellApp.start()

    // 点 Dock 图标且无可见窗口时召回主窗口。
    app.on("activate", () => {
      if (BrowserWindow.getAllWindows().length > 0) shellApp.showMainWindow()
    })
  })

  // 关掉所有窗口不退出：服务常驻菜单栏，这正是产品约定。
  app.on("window-all-closed", () => {})

  // 拦住一切非「真退出」路径的退出请求（Cmd+Q、Dock 退出）。
  app.on("before-quit", (event) => {
    if (shellApp.quitting) return
    event.preventDefault()
    shellApp.window?.hide()
  })
}
