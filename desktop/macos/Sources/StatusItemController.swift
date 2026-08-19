import AppKit

/// 菜单栏图标：左键切换主窗口显隐，右键（或 ⌃+左键）弹操作菜单。
/// 菜单每次现build，直接读服务状态，不维护同步逻辑。
final class StatusItemController: NSObject {
    private let statusItem: NSStatusItem
    private let server: ServerController
    private unowned let app: AppDelegate

    init(server: ServerController, app: AppDelegate) {
        self.server = server
        self.app = app
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        super.init()

        guard let button = statusItem.button else { return }
        let image = NSImage(named: "MenuBarIcon")
        image?.isTemplate = true  // 模板图：系统按菜单栏明暗自动着色
        button.image = image
        button.target = self
        button.action = #selector(clicked)
        button.sendAction(on: [.leftMouseUp, .rightMouseUp])
    }

    @objc private func clicked() {
        let event = NSApp.currentEvent
        let isRight = event?.type == .rightMouseUp
            || event?.modifierFlags.contains(.control) == true
        if isRight {
            popMenu()
        } else {
            app.toggleMainWindow()
        }
    }

    /// 只在右键时临时挂上菜单再模拟点击：常驻 menu 会让左键也弹菜单，
    /// 「左键切窗口/右键菜单」的分工就没了。
    private func popMenu() {
        statusItem.menu = buildMenu()
        statusItem.button?.performClick(nil)
        statusItem.menu = nil
    }

    private func buildMenu() -> NSMenu {
        let menu = NSMenu()
        menu.autoenablesItems = false

        let running = server.state == .running
        let status: String
        switch server.state {
        case .running: status = server.lanShareEnabled ? "服务运行中 · 局域网共享开启" : "服务运行中 · 仅本机访问"
        case .starting: status = "服务启动中…"
        case .failed: status = "服务已停止（可重启）"
        case .stopped: status = "服务未运行"
        }
        menu.addItem(disabled(status))
        menu.addItem(.separator())

        menu.addItem(item("打开 ACPP", #selector(showWindow)))
        menu.addItem(item("在浏览器中打开", #selector(openBrowser), enabled: running))
        menu.addItem(.separator())

        let lan = item("允许局域网访问", #selector(toggleLan))
        lan.state = server.lanShareEnabled ? .on : .off
        menu.addItem(lan)
        if server.lanShareEnabled, let url = server.lanURL {
            menu.addItem(item("复制局域网链接", #selector(copyLan), enabled: running))
            menu.addItem(disabled("    \(url)"))
        } else {
            menu.addItem(item("复制局域网链接", #selector(copyLan), enabled: false))
        }
        menu.addItem(.separator())

        let login = item("开机启动", #selector(toggleOpenAtLogin))
        login.state = LaunchPreferences.openAtLogin ? .on : .off
        menu.addItem(login)

        let minimized = item("开机最小化（仅菜单栏）", #selector(toggleStartMinimized))
        minimized.state = LaunchPreferences.startMinimized ? .on : .off
        menu.addItem(minimized)
        menu.addItem(.separator())

        menu.addItem(item("重启服务", #selector(restart)))
        menu.addItem(item("打开服务日志", #selector(openLog)))
        menu.addItem(.separator())

        menu.addItem(item("退出 ACPP", #selector(quit)))
        return menu
    }

    private func item(_ title: String, _ action: Selector, enabled: Bool = true) -> NSMenuItem {
        let it = NSMenuItem(title: title, action: action, keyEquivalent: "")
        it.target = self
        it.isEnabled = enabled
        return it
    }

    private func disabled(_ title: String) -> NSMenuItem {
        let it = NSMenuItem(title: title, action: nil, keyEquivalent: "")
        it.isEnabled = false
        return it
    }

    // MARK: - 动作转发

    @objc private func showWindow() { app.showMainWindow() }
    @objc private func openBrowser() { app.openInBrowser() }
    @objc private func toggleLan() { app.toggleLanShare() }
    @objc private func copyLan() { app.copyLanLink() }
    @objc private func toggleOpenAtLogin() {
        LaunchPreferences.openAtLogin.toggle()
    }

    @objc private func toggleStartMinimized() {
        LaunchPreferences.startMinimized.toggle()
    }

    @objc private func restart() { app.restartServer() }
    @objc private func openLog() { app.openServerLog() }
    @objc private func quit() { app.requestRealQuit() }
}
