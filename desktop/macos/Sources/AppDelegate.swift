import AppKit

/// 装配层 + 生命周期裁决：谁能真正退出这个 app，是这里唯一的复杂逻辑。
/// 产品约定：关闭窗口/Cmd+Q/Dock 退出都只是隐藏窗口，服务常驻菜单栏；
/// 真退出只有两条路——菜单栏右键「退出」，或系统注销/关机。
final class AppDelegate: NSObject, NSApplicationDelegate {
    private var server: ServerController!
    private var windowController: MainWindowController!
    private var statusItem: StatusItemController!
    /// 菜单栏「退出」置真后，applicationShouldTerminate 才放行。
    private var allowTermination = false
    private var signalSources: [DispatchSourceSignal] = []

    func applicationDidFinishLaunching(_ notification: Notification) {
        MainMenu.install()
        server = ServerController()
        windowController = MainWindowController(appURL: server.localURL)
        statusItem = StatusItemController(server: server, app: self)
        installSignalHandlers()

        // 通知：这里只接线，**不请求授权**——启动就弹系统授权框是最招人烦的
        // 做法，而且用户还没见过这个 app 会通知什么。授权由设置页里的开关
        // 发起，那时他正好在决定这件事。
        Notifier.shared.start()
        Notifier.shared.onActivate = { [weak self] in self?.showMainWindow() }
        Notifier.shared.onAction = { [weak self] payload in
            self?.windowController.deliverNotificationAction(payload)
        }

        // 开机最小化：只驻留菜单栏，窗口不弹、Dock 不占。服务照常起——
        // 用户从菜单栏打开时窗口里应该已经是可用的界面，而不是现加载。
        if LaunchPreferences.startMinimized {
            NSApp.setActivationPolicy(.accessory)
        } else {
            windowController.showLoading()
            windowController.show()
        }
        server.start { [weak self] ok in
            guard let self else { return }
            if ok {
                self.windowController.loadApp()
            } else {
                self.windowController.showFailure(logPath: self.server.logPath)
            }
        }
    }

    func applicationSupportsSecureRestorableState(_ app: NSApplication) -> Bool { true }

    /// 点 Dock 图标且无可见窗口时召回主窗口。
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        if !flag { windowController.show() }
        return true
    }

    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        if allowTermination || isSystemInitiatedQuit() {
            server.stop()  // 必须回收 acp-server（连带它的 agent 子进程），不留孤儿
            return .terminateNow
        }
        windowController.hide()
        return .terminateCancel
    }

    // MARK: - 菜单栏动作

    func toggleMainWindow() {
        restoreRegularActivation()
        windowController.toggle()
    }

    func showMainWindow() {
        restoreRegularActivation()
        windowController.show()
    }

    /// 静默启动后第一次亮出窗口时切回正常 app：Dock 图标与主菜单一起回来。
    /// 主菜单不是装饰——没有 Edit 菜单，WKWebView 里连复制粘贴都没有
    /// （见 MainMenu）。切回后不再切走：开机最小化只管开机那一下。
    private func restoreRegularActivation() {
        guard NSApp.activationPolicy() != .regular else { return }
        NSApp.setActivationPolicy(.regular)
        NSApp.activate(ignoringOtherApps: true)
    }

    func openInBrowser() { NSWorkspace.shared.open(server.localURL) }

    /// 切换局域网共享要换监听地址，只能重启服务进程。上下文在 agent 侧持久化，
    /// 会话续聊时 session/load 无感恢复，代价可接受。
    func toggleLanShare() {
        server.lanShareEnabled.toggle()
        restartServer()
    }

    func copyLanLink() {
        guard let url = server.lanURL else {
            NSSound.beep()
            return
        }
        let pb = NSPasteboard.general
        pb.clearContents()
        pb.setString(url, forType: .string)
    }

    func restartServer() {
        windowController.showLoading()
        server.restart { [weak self] ok in
            guard let self else { return }
            if ok {
                self.windowController.loadApp()
            } else {
                self.windowController.showFailure(logPath: self.server.logPath)
            }
        }
    }

    func openServerLog() {
        NSWorkspace.shared.open(URL(fileURLWithPath: server.logPath))
    }

    func requestRealQuit() {
        allowTermination = true
        NSApp.terminate(nil)
    }

    // MARK: - 退出裁决辅助

    /// 注销/关机/重启发来的 Quit 事件必须放行，否则会卡住整个系统流程。
    private func isSystemInitiatedQuit() -> Bool {
        guard let event = NSAppleEventManager.shared().currentAppleEvent,
              let why = event.attributeDescriptor(forKeyword: fourCC("why?"))?.typeCodeValue
        else { return false }
        return [fourCC("logo"), fourCC("rlgo"), fourCC("shut"), fourCC("rest")].contains(why)
    }

    /// SIGTERM/SIGINT（如终端 kill）也走真退出，保证服务子进程被回收。
    private func installSignalHandlers() {
        for sig in [SIGTERM, SIGINT] {
            signal(sig, SIG_IGN)
            let src = DispatchSource.makeSignalSource(signal: sig, queue: .main)
            src.setEventHandler { [weak self] in self?.requestRealQuit() }
            src.resume()
            signalSources.append(src)
        }
    }
}

private func fourCC(_ code: String) -> FourCharCode {
    code.utf16.reduce(0) { ($0 << 8) + FourCharCode($1) }
}
