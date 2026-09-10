import path from "node:path"

import { BrowserWindow, app, clipboard, powerMonitor, shell } from "electron"

import { installBridge } from "./bridge.js"
import { installMainMenu } from "./menu.js"
import { IssueFeed } from "./issues.js"
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
 * 系统注销/关机也必须放行：Electron 重写了 `terminate:`，系统的 Quit 事件只会
 * 走到 `before-quit`，被拦下后既不回复取消也不退出，macOS 连「应用阻止了关机」
 * 都不弹，关机就一直卡着。区分来源靠 `powerMonitor` 的 `shutdown` 事件——它对应
 * NSWorkspaceWillPowerOffNotification，只在注销/重启/关机时发、且早于 Quit 事件，
 * Cmd+Q 与 Dock 退出都不会触发（见 ADR-015 补记）。
 */

// 窗口遮挡计算在 resize 期间反复触发且跟着掉帧，必须在 ready 之前关掉（ADR-015）。
app.commandLine.appendSwitch("disable-features", "CalculateNativeWinOcclusion")

/**
 * 预览模式：只开窗口加载这个地址，**不启动 acp-server、不占 48090、不放菜单栏
 * 图标**。用途是在不动用户已安装的 ACPP.app 的前提下看壳与界面的改动效果
 * （`make dev-app`，指向 vite 的 45173 + 开发后端 48080）。
 *
 * 隔离靠两件事：这里根本不碰 48090，以及下面那段显式改名换目录。
 */
const DEV_URL = process.env.ACPP_DEV_URL || null

/**
 * 预览壳必须**显式**换掉身份，不能指望「未打包所以名字不同」——打包产物
 * asar 里的 package.json name 仍然是 `acpp-shell`，`app.getName()` 因此与
 * 开发态一模一样，userData 目录与 requestSingleInstanceLock 全都撞上：
 * 正在运行的 ACPP.app 会把预览壳当成自己的第二个实例，后者启动即退出，
 * 日志里连一行错都没有。改名必须赶在任何 getPath 之前。
 */
if (DEV_URL) {
  app.setName("acpp-shell-dev")
  app.setPath("userData", path.join(app.getPath("appData"), "acpp-shell-dev"))
}

class AppShell {
  constructor() {
    this.devPreview = Boolean(DEV_URL)
    // 预览壳不管服务：那份后端是用户正在用的 app 的，碰它就是事故。
    this.server = this.devPreview ? null : new ServerController()
    this.window = null
    this.tray = null
    /** 置真后才允许真正退出，且退出路径不可重入。 */
    this.quitting = false
  }

  async start() {
    installMainMenu({
      devPreview: this.devPreview,
      onCloseWindow: () =>
        this.devPreview ? this.requestRealQuit() : this.window?.hide(),
    })
    installBridge()
    this.window = new MainWindow()
    this.installSignalHandlers()

    if (this.devPreview) {
      // 关窗 = 退出：没有菜单栏图标，窗口藏起来就再也召不回来了。
      this.window.allowClose = true
      this.window.loadApp(DEV_URL)
      this.window.show()
      return
    }

    this.issues = new IssueFeed({ server: this.server })
    this.tray = new TrayController({ server: this.server, shell: this, issues: this.issues })

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
    // 服务起来才有 issue 可拉；重启服务也走这里，顺手重拉一次。
    if (ok) this.issues?.start()
  }

  // MARK: - 菜单栏动作

  toggleMainWindow() {
    this.restoreRegularActivation()
    this.window.toggle()
  }

  /** route 给了就顺带切到那个页面（如菜单栏的「查看全部 issue」→ /github）。 */
  showMainWindow(route) {
    this.restoreRegularActivation()
    if (route && this.server.state === "running") {
      this.window.navigate(this.server.localURL, route)
    }
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
      await this.server?.stop()
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

    // 系统注销/重启/关机：真退出，回收服务子进程。powerMonitor 必须在 ready 之后
    // 才能访问；preventDefault 是告诉 Electron「别走它的 quit 流程，我们自己退」，
    // 随后系统补发的 terminate: 会被 Electron 挡掉，不会与这里的退出路径打架。
    powerMonitor.on("shutdown", (event) => {
      event.preventDefault()
      shellApp.requestRealQuit()
    })
  })

  // 关掉所有窗口不退出：服务常驻菜单栏，这正是产品约定。
  // 预览壳没有常驻这回事，窗口关了就该收摊。
  app.on("window-all-closed", () => {
    if (shellApp.devPreview) shellApp.requestRealQuit()
  })

  // 拦住一切非「真退出」路径的退出请求（Cmd+Q、Dock 退出）。
  app.on("before-quit", (event) => {
    if (shellApp.quitting || shellApp.devPreview) return
    event.preventDefault()
    shellApp.window?.hide()
  })
}
