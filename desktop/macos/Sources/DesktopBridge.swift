import AppKit
import WebKit

/// JS ↔ 壳的窄通道，只暴露设置页真正需要的原生开关。
///
/// 用带 reply 的处理器而不是单向 postMessage：开机启动可能被系统拒绝
/// （未签名、不在「应用程序」目录），前端必须拿到真实结果才能把开关弹回去，
/// 而不是假装成功。每次变更后回读一份快照，前端据此对齐真实状态。
final class DesktopBridge: NSObject, WKScriptMessageHandlerWithReply {
    /// JS 侧的调用名：window.webkit.messageHandlers.acppDesktop
    static let name = "acppDesktop"

    /// 注入到主框架：web 端据此判断「我跑在桌面壳里」。浏览器里这两个开关
    /// 根本不该出现——它们改的是这台机器的登录项，不是服务端配置。
    static let bootstrap = WKUserScript(
        source: "window.__ACPP_DESKTOP__ = true;",
        injectionTime: .atDocumentStart,
        forMainFrameOnly: true)

    func userContentController(_ controller: WKUserContentController,
                               didReceive message: WKScriptMessage,
                               replyHandler: @escaping (Any?, String?) -> Void) {
        guard let body = message.body as? [String: Any],
              let action = body["action"] as? String else {
            replyHandler(nil, "bridge: 需要 {action}")
            return
        }

        switch action {
        case "get":
            replyHandler(snapshot(), nil)

        case "setOpenAtLogin":
            guard let on = body["value"] as? Bool else {
                replyHandler(nil, "bridge: setOpenAtLogin 缺少 value")
                return
            }
            // 失败不抛错——把原因连同真实状态一起回去，让设置页就地说明。
            let reason = LaunchPreferences.setOpenAtLogin(on)
            var result = snapshot()
            if let reason { result["error"] = reason }
            replyHandler(result, nil)

        case "setStartMinimized":
            guard let on = body["value"] as? Bool else {
                replyHandler(nil, "bridge: setStartMinimized 缺少 value")
                return
            }
            LaunchPreferences.startMinimized = on
            replyHandler(snapshot(), nil)

        default:
            replyHandler(nil, "bridge: 未知动作 \(action)")
        }
    }

    private func snapshot() -> [String: Any] {
        [
            "openAtLogin": LaunchPreferences.openAtLogin,
            "startMinimized": LaunchPreferences.startMinimized,
        ]
    }
}
