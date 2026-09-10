import path from "node:path"
import { fileURLToPath } from "node:url"

import { Menu, Tray, app, clipboard, nativeImage } from "electron"

import { chromeProfiles, openURL } from "./browser.js"
import { MENU_LIMIT } from "./issues.js"
import { prefs } from "./prefs.js"

const here = path.dirname(fileURLToPath(import.meta.url))

/** 菜单项标题的长度上限：菜单栏下拉再宽也就这么些字，长了系统会截成省略号。 */
const TITLE_MAX = 48

/**
 * 菜单栏图标：左键、右键都弹同一个菜单（照 codex-ui 的 Codex Viewer：菜单栏
 * 图标就是一张「我的 issue」清单，主窗口从菜单里的「打开 ACPP」进）。
 * 菜单每次现 build，直接读服务状态，不维护同步逻辑。
 *
 * 曾经是「左键切窗口 / 右键菜单」的分工，实际用下来点图标想看的都是清单，
 * 窗口有 Dock 图标可点，于是改成一律弹菜单。
 */
export class TrayController {
  constructor({ server, shell, issues }) {
    this.server = server
    this.shell = shell
    this.issues = issues

    const icon = nativeImage.createFromPath(iconPath())
    // 模板图：系统按菜单栏明暗自动着色
    icon.setTemplateImage(true)
    this.tray = new Tray(icon)
    this.tray.setToolTip("ACPP")

    this.tray.on("click", () => this.popMenu())
    this.tray.on("right-click", () => this.popMenu())
  }

  popMenu() {
    // 菜单画的是上一次拉到的清单；弹出时顺手再拉一次给下次用。
    void this.issues?.refresh()
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
      ...this.issueItems(running),
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

  /**
   * 「分配给我的 issue」一段：默认条件与 GitHub 页一致（open、排除做完 / 取消
   * 的看板列、按优先级排）。点一条**复制纯数字编号**——日常最高频的动作是把
   * 编号丢给 AI 的 /issue 命令，与 codex-ui 的菜单一致；⌥ 点击才用选定的
   * Chrome 账号打开。末尾是账号子菜单与「查看全部」。
   */
  issueItems(running) {
    const snap = this.issues?.snapshot
    const items = []
    if (!running || !snap) {
      items.push({ label: "我的 issue（服务未运行）", enabled: false })
      return items
    }
    if (snap.items.length === 0) {
      const why = snap.error
        ? `拉取失败：${snap.error}`
        : snap.watched && snap.watched.length === 0
          ? "还没有关注仓库，去 GitHub 页挑几个"
          : "没有分配给你的 issue"
      items.push({ label: "我的 issue", enabled: false })
      items.push({ label: `    ${truncate(why, 60)}`, enabled: false })
    } else {
      const more = snap.total > snap.items.length ? `，显示前 ${snap.items.length}` : ""
      items.push({
        label: `我的 issue（${snap.total}${more}）· 点击复制编号，⌥ 点击打开`,
        enabled: false,
      })
      for (const it of snap.items.slice(0, MENU_LIMIT)) {
        // 优先级与看板列作前缀：菜单项没有颜色可用，文字得把两件事说清。
        const tags = [it.priority, it.status].filter(Boolean).join(" · ")
        const repo = it.repo.split("/").pop()
        items.push({
          label: `${tags ? `[${tags}] ` : ""}#${it.number} ${truncate(it.title, TITLE_MAX)}`,
          toolTip: `${it.repo}#${it.number}\n${it.title}\n点击复制 ${it.number}，⌥ 点击打开 GitHub`,
          sublabel: repo,
          // Electron 的原生菜单做不了 codex-ui 那种「行内分热区」，用修饰键分工。
          click: (_item, _win, event) => {
            if (event?.altKey) openURL(it.url, prefs.chromeProfile)
            else clipboard.writeText(String(it.number))
          },
        })
      }
      if (snap.error) items.push({ label: `    部分仓库拉取失败`, enabled: false, toolTip: snap.error })
    }
    items.push({
      label: "查看全部 issue",
      click: () => this.shell.showMainWindow("/github"),
    })
    items.push({
      label: "刷新 issue",
      enabled: running,
      click: () => void this.issues?.refresh({ force: true }),
    })
    const profiles = chromeProfiles()
    if (profiles.length > 0) {
      const current = prefs.chromeProfile
      items.push({
        label: "用哪个 Chrome 账号打开",
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
      })
    }
    return items
  }

  destroy() {
    this.tray?.destroy()
  }
}

function truncate(s, n) {
  const str = String(s ?? "")
  return str.length > n ? `${str.slice(0, n - 1)}…` : str
}

/** 模板图位置：打包态在 Resources 下，开发态回退到仓库 build 产物。 */
function iconPath() {
  if (app.isPackaged) {
    return path.join(path.dirname(process.execPath), "../Resources/MenuBarIcon.png")
  }
  return path.resolve(here, "../../../build/app/stage/icons/MenuBarIcon.png")
}
