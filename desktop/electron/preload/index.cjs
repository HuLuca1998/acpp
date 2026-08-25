// 注入到每个页面：给 web 端一个「我跑在桌面壳里」的标记 + 那条窄通道。
//
// CommonJS 而不是 ESM：sandbox 开着时 preload 只能是 CJS，而 sandbox 值得留着
// ——页面里跑的是我们自己的前端，但它渲染的是 agent 输出的任意内容。
//
// 契约（action 名、返回形状、事件名）对齐 web/src/lib/desktop.ts，两边要一起改。
const { contextBridge, ipcRenderer } = require("electron")

const CHANNEL = "acpp:desktop"
const NOTIFICATION_ACTION_EVENT = "acpp:notification-action"

// web 端据此判断「我跑在桌面壳里」。浏览器里这些开关根本不该出现——它们改的
// 是这台机器的登录项，不是服务端配置。
contextBridge.exposeInMainWorld("__ACPP_DESKTOP__", true)

contextBridge.exposeInMainWorld("acppDesktop", {
  postMessage: (body) => ipcRenderer.invoke(CHANNEL, body),
})

// 通知上的操作交回前端：裁决与回答都走前端已有的 API 客户端，壳不在主进程里
// 再实现一遍认证与请求——那会变成第二份要同步维护的契约。
ipcRenderer.on(NOTIFICATION_ACTION_EVENT, (_event, detail) => {
  window.dispatchEvent(new CustomEvent(NOTIFICATION_ACTION_EVENT, { detail }))
})
