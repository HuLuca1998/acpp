import AppKit
import WebKit

/// 主窗口：一块 WKWebView 加载本机服务。关闭按钮 = 隐藏（app 常驻菜单栏），
/// 站外链接与 target=_blank 转交系统浏览器。
final class MainWindowController: NSObject, NSWindowDelegate, WKNavigationDelegate {
    private let window: NSWindow
    private let webView: WKWebView
    private let bridge = DesktopBridge()
    private let appURL: URL

    var isVisible: Bool { window.isVisible }

    init(appURL: URL) {
        self.appURL = appURL

        let config = WKWebViewConfiguration()
        // 右键「检查元素」，排查前端问题时救命
        config.preferences.setValue(true, forKey: "developerExtrasEnabled")
        // 设置页要开的是这台机器的登录项，只有壳碰得到——开一条窄通道给它。
        config.userContentController.addUserScript(DesktopBridge.bootstrap)
        config.userContentController.addScriptMessageHandler(
            bridge, contentWorld: .page, name: DesktopBridge.name)
        webView = WKWebView(frame: .zero, configuration: config)
        webView.allowsMagnification = true

        window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 1280, height: 820),
            styleMask: [.titled, .closable, .miniaturizable, .resizable],
            backing: .buffered, defer: false)
        window.title = "ACPP"
        window.minSize = NSSize(width: 900, height: 600)
        window.center()
        // 记住上次的窗口位置与大小；orderOut 隐藏而非 close，所以关掉释放也要禁
        window.setFrameAutosaveName("ACPConsoleMain")
        window.isReleasedWhenClosed = false
        window.contentView = webView

        super.init()
        window.delegate = self
        webView.navigationDelegate = self
    }

    // MARK: - 显隐

    func show() {
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    func hide() {
        window.orderOut(nil)
    }

    func toggle() {
        if window.isVisible { hide() } else { show() }
    }

    /// 关闭按钮/Cmd+W = 隐藏窗口，服务与菜单栏图标保留；真退出只在菜单栏右键。
    func windowShouldClose(_ sender: NSWindow) -> Bool {
        hide()
        return false
    }

    /// 把通知上的操作交回前端。裁决与回答都走前端已有的 API 客户端，壳不在
    /// Swift 里再实现一遍认证与请求——那会变成第二份要同步维护的契约。
    ///
    /// 页面还没加载好就丢掉：那种情况下用户点的是通知本体，窗口已经被带到
    /// 前台，他在界面上照样能处理，比让壳去猜要可靠。
    func deliverNotificationAction(_ payload: [String: Any]) {
        guard JSONSerialization.isValidJSONObject(payload),
              let data = try? JSONSerialization.data(withJSONObject: payload),
              let json = String(data: data, encoding: .utf8) else { return }
        let js = "window.dispatchEvent(new CustomEvent('acpp:notification-action',"
            + " { detail: \(json) }))"
        DispatchQueue.main.async { [weak self] in
            self?.webView.evaluateJavaScript(js)
        }
    }

    // MARK: - 内容

    func loadApp() {
        // 主文档强制条件请求（304 才复用缓存）：更新重启后必须拿到新入口页，
        // 否则 WKWebView 的启发式缓存会把旧版界面又端出来。
        var request = URLRequest(url: appURL)
        request.cachePolicy = .reloadRevalidatingCacheData
        webView.load(request)
    }

    func showLoading() {
        webView.loadHTMLString(Self.page(mark: "❯_", markPulse: true, title: "正在启动 ACPP 服务…", detail: ""), baseURL: nil)
    }

    func showFailure(logPath: String) {
        webView.loadHTMLString(Self.page(
            mark: "✕", markPulse: false,
            title: "服务启动失败",
            detail: "可从菜单栏图标 → 重启服务 重试；日志：\(logPath)"), baseURL: nil)
    }

    // MARK: - 导航策略

    func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction,
                 decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        guard let url = navigationAction.request.url else {
            decisionHandler(.allow)
            return
        }
        // target=_blank 没有目标 frame，WKWebView 默认吞掉——转交系统浏览器
        if navigationAction.targetFrame == nil {
            NSWorkspace.shared.open(url)
            decisionHandler(.cancel)
            return
        }
        // 站外 http(s) 链接同样交给浏览器，窗口里只住本机服务
        if let scheme = url.scheme, scheme.hasPrefix("http"),
           let host = url.host, host != "127.0.0.1", host != "localhost" {
            NSWorkspace.shared.open(url)
            decisionHandler(.cancel)
            return
        }
        decisionHandler(.allow)
    }

    // MARK: - 内置页面

    private static func page(mark: String, markPulse: Bool, title: String, detail: String) -> String {
        """
        <!doctype html><meta charset="utf-8"><style>
        html,body{height:100%;margin:0;background:#0B0F0D;color:#D1FAE5;
          font:15px -apple-system,'PingFang SC',sans-serif;
          display:flex;align-items:center;justify-content:center}
        .wrap{text-align:center;max-width:520px;padding:0 24px}
        .mark{font-size:44px;color:#4ADE80;\(markPulse ? "animation:pulse 1.2s ease-in-out infinite" : "")}
        @keyframes pulse{50%{opacity:.35}}
        p{color:#8BA396;margin-top:14px;line-height:1.7;word-break:break-all}
        </style><div class="wrap"><div class="mark">\(mark)</div>
        <p><b style="color:#D1FAE5">\(title)</b><br>\(detail)</p></div>
        """
    }
}
