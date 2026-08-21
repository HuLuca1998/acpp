import AppKit
import UserNotifications

/// macOS 系统通知。只有桌面壳有这条路——局域网访客用浏览器，既收不到也发
/// 不出系统通知（Web Notification 要 secure context，局域网明文 http 拿不到
/// 权限），他们那边走页内提示。
///
/// 授权的两个坑，都是 2026-08 在 macOS 26 上实测出来的：
///
///  1. **app 必须待在「应用程序」目录并被 LaunchServices 认过**。放在临时目录
///     里运行时，`requestAuthorization` 直接返回 `Code=1 Notifications are not
///     allowed`，**连系统弹窗都不出现**，状态停在 notDetermined——看上去像什么
///     都没发生，最容易误判成代码写错了。
///  2. **一旦被拒**（`.denied`），再调 `requestAuthorization` 也不会再弹窗，只会
///     立刻返回 false。唯一出路是拉起系统设置让用户自己开。
///
/// ad-hoc 签名是够用的，不需要开发者证书——网上流传的「必须签名否则崩溃」指的
/// 是完全未签名且没有 bundle 的情况。
final class Notifier: NSObject, UNUserNotificationCenterDelegate {
    static let shared = Notifier()

    /// 通知上的操作回传给前端。裁决与回答都走前端已有的 API 客户端，壳不在
    /// Swift 里再实现一遍认证与请求——那会变成第二份要同步维护的契约。
    var onAction: (([String: Any]) -> Void)?
    /// 点了通知本体：把窗口带到前台，用户接下来要看的就是那一页。
    var onActivate: (() -> Void)?

    private let center = UNUserNotificationCenter.current()

    /// 已注册的 category，按注册顺序留着。系统的 `setNotificationCategories`
    /// 是**全量替换**，所以得自己攒——决策通知的按钮是 agent 当场给出的选项
    /// （允许一次/始终允许/拒绝…），没法预先写死成固定 category，只能一条通知
    /// 现注册一个。
    private var categories: [String: UNNotificationCategory] = [:]
    private var categoryOrder: [String] = []

    /// category 保有上限。超了就丢最老的：那些通知早已不在通知中心里，
    /// 留着只会让每次全量下发越来越大。
    private let categoryLimit = 32

    /// 无按钮通知共用的 category：点一下回到 app 就够了。
    private static let plainCategory = "acpp.plain"

    func start() {
        center.delegate = self
        register(UNNotificationCategory(
            identifier: Self.plainCategory, actions: [],
            intentIdentifiers: [], options: []))
    }

    // MARK: - 授权

    /// 当前授权状态，供设置页如实显示。
    /// 注意判断能不能发通知只看 `authorizationStatus`——`alertSetting` 在被拒
    /// 的 app 上照样是 enabled（实测），拿它判断会得到相反的结论。
    func status(_ done: @escaping ([String: Any]) -> Void) {
        center.getNotificationSettings { settings in
            let status: String
            switch settings.authorizationStatus {
            case .notDetermined: status = "notDetermined"
            case .denied: status = "denied"
            case .authorized: status = "authorized"
            case .provisional: status = "provisional"
            @unknown default: status = "unknown"
            }
            DispatchQueue.main.async {
                done([
                    "status": status,
                    // 只有「还没问过」时系统才会弹授权框；其余情况按钮该引导
                    // 用户去系统设置，而不是点了没反应。
                    "canRequest": settings.authorizationStatus == .notDetermined,
                    "bundlePath": Bundle.main.bundlePath,
                    // app 不在「应用程序」目录时授权会静默失败，设置页要能
                    // 当场说明白，而不是让用户对着一个点不动的开关猜。
                    "inApplicationsDir": Self.isInApplicationsDir,
                ])
            }
        }
    }

    func requestAuthorization(_ done: @escaping ([String: Any]) -> Void) {
        center.requestAuthorization(options: [.alert, .sound, .badge]) { _, err in
            // 成功与否一律回读一次真实状态：被拒时 granted=false 与
            // 「系统压根没弹窗」是同一个返回值，只有状态能区分。
            self.status { var result = $0
                if let err { result["error"] = err.localizedDescription }
                done(result)
            }
        }
    }

    /// 拉起系统设置里本 app 的通知面板。被拒之后这是唯一的出路。
    @discardableResult
    func openSettings() -> Bool {
        let id = Bundle.main.bundleIdentifier ?? ""
        // macOS 13+ 是 Notifications-Settings.extension，老系统是
        // com.apple.preference.notifications——都试一遍，谁开得了算谁的。
        let candidates = [
            "x-apple.systempreferences:com.apple.Notifications-Settings.extension?id=\(id)",
            "x-apple.systempreferences:com.apple.preference.notifications?id=\(id)",
        ]
        for raw in candidates {
            guard let url = URL(string: raw) else { continue }
            if NSWorkspace.shared.open(url) { return true }
        }
        return false
    }

    /// app 是否待在「应用程序」目录（系统级或用户级）。不在那儿通知拿不到授权。
    static var isInApplicationsDir: Bool {
        let path = Bundle.main.bundlePath
        if path.hasPrefix("/Applications/") { return true }
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        return path.hasPrefix(home + "/Applications/")
    }

    // MARK: - 发送

    /// 发一条通知。`actions` 非空时按它现注册一个 category——决策通知的按钮
    /// 就是 agent 给的选项，按下去等于当场裁决。
    func post(id: String, title: String, subtitle: String?, body: String,
              actions: [[String: Any]], userInfo: [String: Any], threadID: String?) {
        let content = UNMutableNotificationContent()
        content.title = title
        if let subtitle, !subtitle.isEmpty { content.subtitle = subtitle }
        content.body = body
        content.sound = .default
        content.userInfo = userInfo
        // 同一会话的通知归一组堆叠，长任务不会把通知中心刷满。
        if let threadID { content.threadIdentifier = threadID }
        content.categoryIdentifier = actions.isEmpty
            ? Self.plainCategory
            : registerCategory(for: id, actions: actions)

        center.add(UNNotificationRequest(identifier: id, content: content, trigger: nil))
    }

    /// 撤回一条通知：这件事已经在界面上处理掉了（比如权限在页面上裁决过），
    /// 通知再挂在那儿就是在骗人。
    func dismiss(id: String) {
        center.removeDeliveredNotifications(withIdentifiers: [id])
        center.removePendingNotificationRequests(withIdentifiers: [id])
    }

    /// 按 agent 给的选项现场生成 category。
    ///
    /// 只取前 4 个：macOS 通知展开后放得下的按钮有限，再多也点不到，而选项
    /// 列表在页面上的卡片里是完整的——通知是快捷方式，不是替代品。
    private func registerCategory(for id: String, actions: [[String: Any]]) -> String {
        let identifier = "acpp.actions.\(id)"
        let items: [UNNotificationAction] = actions.prefix(4).compactMap { raw in
            guard let actionID = raw["id"] as? String,
                  let title = raw["title"] as? String else { return nil }
            let destructive = raw["destructive"] as? Bool ?? false
            return UNNotificationAction(
                identifier: actionID, title: title,
                options: destructive ? [.destructive] : [])
        }
        guard !items.isEmpty else { return Self.plainCategory }

        register(UNNotificationCategory(
            identifier: identifier, actions: items,
            intentIdentifiers: [], options: []))
        return identifier
    }

    private func register(_ category: UNNotificationCategory) {
        if categories[category.identifier] == nil {
            categoryOrder.append(category.identifier)
        }
        categories[category.identifier] = category
        while categoryOrder.count > categoryLimit {
            let oldest = categoryOrder.removeFirst()
            // 无按钮的那个是所有普通通知的依靠，永远不能被挤掉。
            if oldest == Self.plainCategory {
                categoryOrder.append(oldest)
                continue
            }
            categories.removeValue(forKey: oldest)
        }
        center.setNotificationCategories(Set(categories.values))
    }

    // MARK: - UNUserNotificationCenterDelegate

    /// app 在前台时也要显示。要不要打扰用户是前端判断过才发出来的，
    /// 到这一步再按「app 在不在前台」二次拦截，只会让它该响的时候不响。
    func userNotificationCenter(_ center: UNUserNotificationCenter,
                                willPresent notification: UNNotification,
                                withCompletionHandler handler: @escaping (UNNotificationPresentationOptions) -> Void) {
        handler([.banner, .sound, .list])
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter,
                                didReceive response: UNNotificationResponse,
                                withCompletionHandler handler: @escaping () -> Void) {
        let request = response.notification.request
        var payload: [String: Any] = [
            "notificationId": request.identifier,
            "actionId": response.actionIdentifier,
            "userInfo": request.content.userInfo,
        ]
        if let text = response as? UNTextInputNotificationResponse {
            payload["userText"] = text.userText
        }

        // 清除（划掉/点“清除”）不是一个决定，别让它触发任何动作。
        if response.actionIdentifier == UNNotificationDismissActionIdentifier {
            handler()
            return
        }
        // 点通知本体 = 想去看看，把窗口带到前台；点具体按钮 = 就地裁决，
        // 不打断用户手头的事。
        if response.actionIdentifier == UNNotificationDefaultActionIdentifier {
            onActivate?()
        }
        onAction?(payload)
        handler()
    }
}
