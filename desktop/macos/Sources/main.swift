import AppKit

// 壳入口：菜单栏 + 主窗口 + 后台 acp-server 子进程，全部装配在 AppDelegate。
// .regular 让 app 拥有 Dock 图标与主菜单；「菜单栏常驻」由退出拦截实现，
// 不走 LSUIElement（那是编译期就定死的，而「开机最小化」要能随时开关——
// AppDelegate 在启动时按偏好切 .accessory，用户打开窗口再切回来）。
let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.setActivationPolicy(.regular)
app.run()
