// 托盘弹层页面的桥：只暴露它需要的几个动作与一条快照推送。
// CJS 的原因同 preload/index.cjs：sandbox 开着时 preload 只能是 CJS。
// 契约（action 名、快照形状、事件名）对齐 main/popover.js 与 popover/popover.js。
const { contextBridge, ipcRenderer } = require("electron")

const CHANNEL = "acpp:tray"
const SNAPSHOT_EVENT = "acpp:tray:snapshot"

const call = (action, extra = {}) => ipcRenderer.invoke(CHANNEL, { action, ...extra })

contextBridge.exposeInMainWorld("acppTray", {
  get: () => call("get"),
  copy: (text) => call("copy", { text }),
  open: (url) => call("open", { url }),
  refresh: () => call("refresh"),
  openApp: (route) => call("openApp", { route }),
  size: (height) => call("size", { height }),
  hide: () => call("hide"),
  onSnapshot: (cb) => {
    ipcRenderer.on(SNAPSHOT_EVENT, (_event, snap) => cb(snap))
  },
})
