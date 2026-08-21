import AppKit
import WebKit

/// JS ↔ 壳的窄通道：设置页要的原生开关，加上系统通知的收发。
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

        // 通知的三件事：查状态、请求授权、拉起系统设置。设置页要如实显示
        // 现状——被拒之后请求授权不会再弹窗（见 Notifier），那时唯一能做的
        // 就是把人送去系统设置。
        case "notificationStatus":
            Notifier.shared.status { replyHandler($0, nil) }

        case "requestNotification":
            Notifier.shared.requestAuthorization { replyHandler($0, nil) }

        case "openNotificationSettings":
            replyHandler(["opened": Notifier.shared.openSettings()], nil)

        case "notify":
            guard let id = body["id"] as? String,
                  let title = body["title"] as? String else {
                replyHandler(nil, "bridge: notify 需要 {id, title}")
                return
            }
            Notifier.shared.post(
                id: id,
                title: title,
                subtitle: body["subtitle"] as? String,
                body: body["body"] as? String ?? "",
                actions: body["actions"] as? [[String: Any]] ?? [],
                userInfo: body["userInfo"] as? [String: Any] ?? [:],
                threadID: body["threadId"] as? String)
            replyHandler(["posted": true], nil)

        // 这件事已经在页面上处理掉了，通知别再挂着骗人。
        case "dismissNotify":
            guard let id = body["id"] as? String else {
                replyHandler(nil, "bridge: dismissNotify 缺少 id")
                return
            }
            Notifier.shared.dismiss(id: id)
            replyHandler(["dismissed": true], nil)

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
