import path from "node:path"
import { fileURLToPath } from "node:url"

import { Menu, Tray, app, nativeImage } from "electron"

import { chromeProfiles } from "./browser.js"
import { prefs } from "./prefs.js"

const here = path.dirname(fileURLToPath(import.meta.url))

/**
 * 菜单栏图标：**左键弹「我的 issue」面板**（popover.js 自绘，照 codex-ui 的
 * Codex Viewer——图标就是一张 issue 清单），**右键（或 ⌃+左键）弹操作菜单**
 * （打开窗口、局域网、启动项、重启服务、退出）。操作菜单每次现 build，
 * 直接读服务状态，不维护同步逻辑。
 *
 * 曾经左键是切主窗口显隐，实际用下来点图标想看的都是清单，窗口有 Dock 图标
 * 可点，于是左键让给了清单。
 */
export class TrayController {
  constructor({ server, shell, popover }) {
    this.server = server
    this.shell = shell
    this.popover = popover

    const icon = nativeImage.createFromPath(iconPath())
    // 模板图：系统按菜单栏明暗自动着色
    icon.setTemplateImage(true)
    this.tray = new Tray(icon)
    this.tray.setToolTip("ACPP")

    this.tray.on("click", (event, bounds) => {
      if (event.ctrlKey) this.popActionMenu()
      else this.popover.toggle(bounds)
    })
    this.tray.on("right-click", () => this.popActionMenu())
  }

  popActionMenu() {
    this.popover.hide()
    this.tray.popUpContextMenu(this.buildActionMenu())
  }

  /** 右键菜单：服务状态与壳的操作项，issue 在左键面板里。 */
  buildActionMenu() {
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
      ...chromeProfileItems(),
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

/**
 * 「用哪个 Chrome 账号打开 issue」：读 Chrome 的账号清单做单选子菜单，偏好存
 * 壳的 prefs。私有仓库只有某个账号有权限时用。没装 Chrome 就不出这项。
 */
function chromeProfileItems() {
  const profiles = chromeProfiles()
  if (profiles.length === 0) return []
  const current = prefs.chromeProfile
  return [
    {
      label: "用哪个 Chrome 账号打开 issue",
      submenu: [
        {
          label: "跟随 Chrome 上次使用的账号",
          type: "radio",
          checked: current === "",
          click: () => (prefs.chromeProfile = ""),
        },
        { type: "separator" },
        ...profiles.map((p) => ({
          label: p.email ? `${p.name} — ${p.email}` : p.name,
          sublabel: p.dir,
          type: "radio",
          checked: current === p.dir,
          click: () => (prefs.chromeProfile = p.dir),
        })),
      ],
    },
  ]
}

/** 模板图位置：打包态在 Resources 下，开发态回退到仓库 build 产物。 */
function iconPath() {
  if (app.isPackaged) {
    return path.join(path.dirname(process.execPath), "../Resources/MenuBarIcon.png")
  }
  return path.resolve(here, "../../../build/app/stage/icons/MenuBarIcon.png")
}
