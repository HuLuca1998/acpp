import path from "node:path"
import { fileURLToPath } from "node:url"

import { Menu, Tray, app, nativeImage } from "electron"

const here = path.dirname(fileURLToPath(import.meta.url))

/**
 * 菜单栏图标：左键切换主窗口显隐，右键（或 ⌃+左键）弹操作菜单。
 * 菜单每次现 build，直接读服务状态，不维护同步逻辑。
 *
 * 比 Swift 版干净的一点：那边为了保住「左键切窗口/右键菜单」的分工，得靠
 * 「右键时临时挂 menu 再 performClick，然后立刻摘掉」的手法绕开 NSStatusItem
 * 的默认行为；Electron 直接分了两个事件，popUpContextMenu 用完即散。
 */
export class TrayController {
  constructor({ server, shell }) {
    this.server = server
    this.shell = shell

    const icon = nativeImage.createFromPath(iconPath())
    // 模板图：系统按菜单栏明暗自动着色
    icon.setTemplateImage(true)
    this.tray = new Tray(icon)
    this.tray.setToolTip("ACPP")

    this.tray.on("click", (event) => {
      if (event.ctrlKey) this.popMenu()
      else this.shell.toggleMainWindow()
    })
    this.tray.on("right-click", () => this.popMenu())
  }

  popMenu() {
    this.tray.popUpContextMenu(this.buildMenu())
  }

  buildMenu() {
    const server = this.server
    const running = server.state === "running"
    const status =
      {
        running: server.lanShareEnabled
          ? "服务运行中 · 局域网共享开启"
          : "服务运行中 · 仅本机访问",
        starting: "服务启动中…",
        failed: "服务已停止（可重启）",
        stopped: "服务未运行",
      }[server.state] ?? "服务未运行"

    const lanURL = server.lanURL
    const items = [
      { label: status, enabled: false },
      { type: "separator" },
      { label: "打开 ACPP", click: () => this.shell.showMainWindow() },
      { label: "在浏览器中打开", enabled: running, click: () => this.shell.openInBrowser() },
      { type: "separator" },
      {
        label: "允许局域网访问",
        type: "checkbox",
        checked: server.lanShareEnabled,
        click: () => this.shell.toggleLanShare(),
      },
    ]

    if (server.lanShareEnabled && lanURL) {
      items.push({ label: "复制局域网链接", enabled: running, click: () => this.shell.copyLanLink() })
      items.push({ label: `    ${lanURL}`, enabled: false })
    } else {
      items.push({ label: "复制局域网链接", enabled: false })
    }

    items.push(
      { type: "separator" },
      {
        label: "开机启动",
        type: "checkbox",
        checked: this.shell.openAtLogin,
        click: () => this.shell.toggleOpenAtLogin(),
      },
      {
        label: "启动时只驻留菜单栏",
        type: "checkbox",
        checked: this.shell.startMinimized,
        click: () => this.shell.toggleStartMinimized(),
      },
      { type: "separator" },
      { label: "重启服务", click: () => this.shell.restartServer() },
      { label: "打开服务日志", click: () => this.shell.openServerLog() },
      { type: "separator" },
      { label: "退出 ACPP", click: () => this.shell.requestRealQuit() }
    )

    return Menu.buildFromTemplate(items)
  }

  destroy() {
    this.tray?.destroy()
  }
}

/** 模板图位置：打包态在 Resources 下，开发态回退到仓库 build 产物。 */
function iconPath() {
  if (app.isPackaged) {
    return path.join(path.dirname(process.execPath), "../Resources/MenuBarIcon.png")
  }
  return path.resolve(here, "../../../build/app/stage/icons/MenuBarIcon.png")
}
