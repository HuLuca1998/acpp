import { Menu, app } from "electron"

/**
 * 主菜单。Edit 菜单不是装饰——没有它，窗口里的 Cmd+C/V/X/A 全部失效；
 * App/Window 菜单保持系统习惯。
 *
 * 与 Swift 版的唯一差别是这里用 role 而不是手绑 selector：Electron 的 role
 * 自带正确的 accelerator 与本地化标题，手写反而容易漏掉「重做」这类。
 */
export function installMainMenu({ onCloseWindow, devPreview = false }) {
  const template = [
    {
      label: app.name,
      submenu: [
        { role: "about", label: "关于 ACPP" },
        { type: "separator" },
        { role: "hide", label: "隐藏 ACPP" },
        { role: "hideOthers", label: "隐藏其他" },
        { type: "separator" },
        // Cmd+Q 被拦成「隐藏窗口」，真退出只在菜单栏右键——标题如实描述行为。
        {
          label: devPreview ? "退出预览" : "关闭窗口（服务保留在菜单栏）",
          accelerator: "Command+Q",
          click: onCloseWindow,
        },
      ],
    },
    {
      label: "编辑",
      submenu: [
        { role: "undo", label: "撤销" },
        { role: "redo", label: "重做" },
        { type: "separator" },
        { role: "cut", label: "剪切" },
        { role: "copy", label: "拷贝" },
        { role: "paste", label: "粘贴" },
        { role: "selectAll", label: "全选" },
      ],
    },
    // 预览壳专属：改前端代码后要能刷新、要能开控制台。正式壳里不给——
    // 那是用户的 app，不是调试台。
    ...(devPreview
      ? [
          {
            label: "开发",
            submenu: [
              { role: "reload", label: "刷新" },
              { role: "forceReload", label: "强制刷新" },
              { role: "toggleDevTools", label: "开发者工具" },
              { type: "separator" },
              { role: "resetZoom", label: "实际大小" },
              { role: "zoomIn", label: "放大" },
              { role: "zoomOut", label: "缩小" },
            ],
          },
        ]
      : []),
    {
      label: "窗口",
      role: "windowMenu",
      submenu: [
        { role: "minimize", label: "最小化" },
        { role: "close", label: "关闭窗口" },
        { type: "separator" },
        { role: "zoom", label: "缩放" },
      ],
    },
  ]
  Menu.setApplicationMenu(Menu.buildFromTemplate(template))
}
