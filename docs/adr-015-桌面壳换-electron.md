# ADR-015：桌面壳从 Swift/AppKit 换成 Electron

日期：2026-08-25　状态：已采纳（取代 [adr-004](adr-004-macos-桌面壳.md) 的**壳选型**决策，其余决策——48090 端口、局域网默认关、数据目录共用 `~/.acpp`、图标即代码——继续有效）

## 背景

桌面版拖窗口边框、拖工作区面板分隔条时明显卡顿，同一份前端在 Chrome 里却完全跟手。
adr-004 选 Swift/AppKit + WKWebView 时看重的是「零阻抗、产物 16MB」，没有预料到
WKWebView 在 macOS live resize 下的表现。

## 决策

### 换 Electron，因为卡顿是 WKWebView 固有的

**先排除了前端**：纯 CSS layout 实测 0.3ms；拖分隔条期间 dockview 零 setState，
各面板都没订阅尺寸变化；消息条早已挂 `content-visibility:auto`。前端这一侧是干净的。

**再排除了壳的用法**。四种 live resize 策略逐一实测，全部无效：

| 策略 | 结果 |
| --- | --- |
| 现状：webView 直接当 contentView | 卡 |
| live resize 期间 100ms 节流同步 frame | 卡 |
| live resize 期间完全冻结不重排 | 卡 |
| `takeSnapshot` 顶替 + webView 退出 view 层级 | 卡 |

**决定性的一条**：把页面换成 `data:text/html` 里只有一句 `blank` 的空白页，
**照样卡**。页面里没有 React、没有 dockview、没有 markdown，成本为零。

结论：只要窗口里住着 WKWebView，macOS live resize 就到不了 Chromium 的流畅度。
这是引擎的取舍——Chromium 在 resize 时先把已有画面拉伸上屏、重排在合成器线程后台
跟进（边框永远跟手）；WebKit 选的是「宁可卡也不露白」，内容与 resize 同步。
壳侧改不动它。

### 不选 Tauri

Tauri 在 macOS 上用的**就是 WKWebView**，换过去等于没换。

### 窗口配置：`backgroundColor` 是性能开关，不是外观选项

裸的 `new BrowserWindow()` 一样卡——这一步差点让人误判「Electron 也不行」。
逐项 A/B 后定下这套参数：

| 参数 | 作用 |
| --- | --- |
| `backgroundColor: '#0B0F0D'` | **决定性的一条**。不设时窗口按带 alpha 的图层合成，每帧都要做透明混合；设成不透明色后才走 opaque 快速路径 |
| `--disable-features=CalculateNativeWinOcclusion` | 窗口遮挡计算在 resize 期间反复触发 |
| `webContents.setBackgroundThrottling(false)` | 体感更稳 |
| ~~`titleBarStyle: 'hiddenInset'`~~ | **实测有害**，会干扰窗口拖动，不要用 |

这套配下来与 Chrome 窗口并排拖已经分不出差别。**改窗口参数前请重跑这个对比**，
`backgroundColor` 尤其不能因为「反正 CSS 里有背景色」就删掉。

### Cmd+Q 保持拦截，接受注销时的系统提示

Swift 版靠读 Quit AppleEvent 的 `why?` 参数区分「用户按了 Cmd+Q」和「系统在注销」，
只放行后者；Electron 的 `before-quit` 拿不到来源，没有等价 API。三个选项——
Cmd+Q 改真退出 / 保持拦截 / 加超时启发式——选了**保持拦截**：产品约定
（关闭窗口与 Cmd+Q 都只是隐藏，真退出只走菜单栏）不变，代价是系统注销/关机时
会弹「应用阻止了注销」要用户确认一下。启发式方案被否掉是因为它不保证对所有
系统流程都准，那种「大部分时候对」的行为比一个稳定的提示更难排查。

**2026-09-10 补记**：「没有等价 API」与「系统会提示」两个前提都不成立。实测关机时
ACPP 只是静默不退、关机一直卡着，没有任何提示——因为 Electron 重写了 `terminate:`，
系统的 Quit AppleEvent 只走到 `before-quit`，被拦下后既不回复 `NSTerminateCancel`
也不退出。等价 API 其实有：Electron 在 `applicationWillFinishLaunching` 里监听了
`NSWorkspaceWillPowerOffNotification`，暴露为 `powerMonitor` 的 `shutdown` 事件，
只在注销/重启/关机时发且早于 Quit 事件，Cmd+Q 与 Dock 退出不会触发。壳现在在该
事件里走真退出（停 acp-server 后 `app.exit`），决策本身（Cmd+Q 只隐藏）不变，
「注销要手动确认」这个代价没了。

### 主进程用 ESM JS，不引 TypeScript

壳是薄装配层，为它单独拉一条 tsc 构建链不划算；`make typecheck` 继续只管 web。

### 保持不变

`ACPP_SHELL_PID` 自更新握手、48090 端口与清端口策略、登录 shell PATH 注入、
局域网默认关、关闭窗口=隐藏、真退出只在菜单栏、图标由 `icongen.swift` 程序化绘制。

## 代价

- **体积 21MB → 约 250MB**，常驻内存大约翻倍。用户在知情下拍板接受：拖窗口是日常
  高频动作，卡顿的持续代价高于磁盘占用。（参照：ChatGPT.app 同为 Electron，1.2G。）
- 打包链路重做：`swiftc` 换成 `@electron/packager`（不选 electron-builder——现有脚本
  本来就是手工组 bundle + ad-hoc 签名，packager 更贴合）。
- 附带收益：壳不再绑死 macOS，将来要 Windows/Linux 桌面版时不必重写（当前不做）。

## 已知边界

- **通知必须待在「应用程序」目录**才拿得到授权，否则 `UNErrorDomain Code=1` 且
  连系统弹窗都不出现。这条在 Electron 下**完全一样**——迁移时实测复现：未打包的
  Electron 发通知直接 failed，打包成 .app 放进 `~/Applications` 后立刻正常。
  这是 macOS 的限制，与引擎无关，adr-004 记的那两个通知坑继续有效。
- 帧率没法自动测：无焦点窗口 rAF 被节流，内嵌浏览器 pane 是 hidden 的同样测不了。
  验证只能靠多开几个不同配置的窗口让人手动拖对比。

## 关联

- [adr-004 macOS 桌面壳](adr-004-macos-桌面壳.md)（被本文取代的壳选型；其余决策仍然有效）
- [adr-013 通知体系](adr-013-通知体系.md)（决策通知带按钮的设计，迁移后行为不变）
- README §macOS 桌面版
