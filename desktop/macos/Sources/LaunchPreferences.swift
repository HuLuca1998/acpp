import AppKit
import ServiceManagement

/// 启动偏好：开机自启、开机静默驻留。
///
/// 自启不自己写 LaunchAgent plist——`SMAppService` 是 macOS 13 起的正道，
/// 用户能在「系统设置 › 通用 › 登录项」里看到并关掉它，我们只是同一个开关的
/// 另一个入口（最低系统版本本来就是 13.0，见 Info.plist）。
enum LaunchPreferences {
    private static let startMinimizedKey = "acpp.startMinimized"

    /// 开机自启。读的是系统里的真实注册状态，不自己记一份——用户随时可能
    /// 在系统设置里改，本地缓存只会和事实对不上。
    static var openAtLogin: Bool {
        get { SMAppService.mainApp.status == .enabled }
        set {
            do {
                if newValue {
                    try SMAppService.mainApp.register()
                } else {
                    try SMAppService.mainApp.unregister()
                }
            } catch {
                // 未签名或不在 /Applications 下时注册会被系统拒绝。这不是能
                // 静默吞掉的失败——开关会弹回原位，用户得知道为什么。
                NSLog("acpp: 切换开机自启失败: \(error.localizedDescription)")
                presentFailure(enabling: newValue, error: error)
            }
        }
    }

    /// 开机最小化：启动后只驻留菜单栏，不显示主窗口也不占 Dock。
    /// 只管**启动那一下**——用户之后手动打开窗口，就是一个正常 app。
    static var startMinimized: Bool {
        get { UserDefaults.standard.bool(forKey: startMinimizedKey) }
        set { UserDefaults.standard.set(newValue, forKey: startMinimizedKey) }
    }

    private static func presentFailure(enabling: Bool, error: Error) {
        let alert = NSAlert()
        alert.alertStyle = .warning
        alert.messageText = enabling ? "无法开启开机启动" : "无法关闭开机启动"
        alert.informativeText = """
            系统拒绝了这次登录项变更：\(error.localizedDescription)

            未签名或不在「应用程序」文件夹里的 app 常会被拒。可以把 ACPP 移到
            「应用程序」后重试，或直接在「系统设置 › 通用 › 登录项」里改。
            """
        alert.addButton(withTitle: "好")
        NSApp.activate(ignoringOtherApps: true)
        alert.runModal()
    }
}
