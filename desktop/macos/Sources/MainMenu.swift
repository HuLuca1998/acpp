import AppKit

/// 程序化构建主菜单，不用 nib。Edit 菜单不是装饰——没有它，WKWebView 里的
/// Cmd+C/V/X/A 全部失效；App/Window 菜单保持系统习惯。
enum MainMenu {
    static func install() {
        let main = NSMenu()

        let appItem = NSMenuItem()
        main.addItem(appItem)
        let appMenu = NSMenu()
        appItem.submenu = appMenu
        appMenu.addItem(
            withTitle: "关于 ACPP",
            action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: "")
        appMenu.addItem(.separator())
        appMenu.addItem(
            withTitle: "隐藏 ACPP",
            action: #selector(NSApplication.hide(_:)), keyEquivalent: "h")
        // Cmd+Q 会被 AppDelegate 拦截成「隐藏窗口」，真退出只在菜单栏右键——标题如实描述行为。
        appMenu.addItem(
            withTitle: "关闭窗口（服务保留在菜单栏）",
            action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")

        let editItem = NSMenuItem()
        main.addItem(editItem)
        let edit = NSMenu(title: "编辑")
        editItem.submenu = edit
        edit.addItem(withTitle: "撤销", action: Selector(("undo:")), keyEquivalent: "z")
        edit.addItem(withTitle: "重做", action: Selector(("redo:")), keyEquivalent: "Z")
        edit.addItem(.separator())
        edit.addItem(withTitle: "剪切", action: #selector(NSText.cut(_:)), keyEquivalent: "x")
        edit.addItem(withTitle: "拷贝", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        edit.addItem(withTitle: "粘贴", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        edit.addItem(withTitle: "全选", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")

        let windowItem = NSMenuItem()
        main.addItem(windowItem)
        let windowMenu = NSMenu(title: "窗口")
        windowItem.submenu = windowMenu
        windowMenu.addItem(
            withTitle: "最小化", action: #selector(NSWindow.miniaturize(_:)), keyEquivalent: "m")
        windowMenu.addItem(
            withTitle: "关闭窗口", action: #selector(NSWindow.performClose(_:)), keyEquivalent: "w")
        NSApp.windowsMenu = windowMenu

        NSApp.mainMenu = main
    }
}
