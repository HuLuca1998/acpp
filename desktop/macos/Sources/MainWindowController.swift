import AppKit
import WebKit

/// 主窗口：一块 WKWebView 加载本机服务。关闭按钮 = 隐藏（app 常驻菜单栏），
/// 站外链接与 target=_blank 转交系统浏览器。
final class MainWindowController: NSObject, NSWindowDelegate, WKNavigationDelegate {
    private let window: NSWindow
    private let webView: WKWebView
    private let appURL: URL

    var isVisible: Bool { window.isVisible }

    init(appURL: URL) {
        self.appURL = appURL

        let config = WKWebViewConfiguration()
        // 右键「检查元素」，排查前端问题时救命
        config.preferences.setValue(true, forKey: "developerExtrasEnabled")
        webView = WKWebView(frame: .zero, configuration: config)
        webView.allowsMagnification = true

        window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 1280, height: 820),
            styleMask: [.titled, .closable, .miniaturizable, .resizable],
            backing: .buffered, defer: false)
        window.title = "ACP Console"
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

    // MARK: - 内容

    func loadApp() {
        webView.load(URLRequest(url: appURL))
    }

    func showLoading() {
        webView.loadHTMLString(Self.page(mark: "❯_", markPulse: true, title: "正在启动 ACP Console 服务…", detail: ""), baseURL: nil)
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
