# ADR-004：macOS 桌面壳——Swift/AppKit 原生实现

日期：2026-08-13　状态：已采纳

## 背景

web 端基本成型后，需要把 ACP Console 打包成 macOS 桌面应用。产品要求：有 Dock 图标与菜单栏图标；关闭窗口不退出（常驻菜单栏，右键才是真退出）；能一键跳到浏览器用 web 端；支持局域网共享链接给其他设备。根 AGENTS.md 原本为 `desktop/` 预留的方向是 tauri/electron。

## 决策

### 壳选型：Swift/AppKit 原生，不用 tauri/electron

- 需求全部是 macOS 专属行为（NSStatusItem 左右键分工、关闭即隐藏、Quit 事件拦截、系统注销放行）。原生 AppKit 对这些是零阻抗，跨平台框架反而要绕它们的抽象层。
- 后端已支持 `ACP_WEB_DIR` 单进程托管前端，壳的职责收敛为「WKWebView 窗口 + 菜单栏 + 子进程管理」，约 500 行 Swift 就写完，引入 Rust（tauri）或 Node 运行时（electron，产物 +150MB）不成比例。
- 机器自带 swiftc（Xcode CLT），无新增工具链；产物 16MB。
- 代价：将来若要 Windows/Linux 桌面版，壳要另写。届时 web + server 部分原样可用，壳本身很薄，重写成本可控；真到那一步再按 tauri 评估，本决策不挡路。

### 端口：桌面版固定 48090

开发态约定「48080 被占就杀掉旧进程」（AGENTS.md §4.0），桌面版若共用 48080 会与 dev.sh 互相误杀。48090 让两者可并存共栖，壳沿用同一端口策略：启动前清掉 48090 上的遗留进程（上次被 SIGKILL 留下的孤儿）再拉起自己的。

### 局域网共享：默认关，菜单栏开关

工作区终端与 agent 都是任意命令执行面（README §安全姿态），监听 0.0.0.0 等于把它暴露给所在网络，笔记本换到陌生 Wi-Fi 时风险不可控。默认 `127.0.0.1`，用户在菜单栏显式开启后才监听 `0.0.0.0`；开关状态存 UserDefaults，切换通过重启服务进程实现（agent 上下文在 runtime 侧持久化，`session/load` 无感恢复，代价可接受）。

### 数据目录：与开发态共用 `~/.acpp`

桌面版和 dev 看到同样的 agent 注册与会话，这是特性不是妥协。两个服务同时运行时共写一个 SQLite 存在理论上的锁竞争，实际使用（一次只操作一边）可接受，不为此引入隔离。

### 图标即代码

App 图标与菜单栏模板图由 `desktop/macos/IconGen/icongen.swift` 用 CoreGraphics 逐尺寸矢量绘制，打包时现生成 iconset → icns，仓库不存二进制图片。设计语言：终端提示符 `❯` + 光标下划线，绿色取自 web 主题 primary。

### 已知边界

- GUI app 没有登录 shell 的 PATH，壳启动时执行 `$SHELL -lc` 取真实 PATH 注入子进程，否则 server 拉不起 `codex-acp` / `claude-agent-acp`。
- ad-hoc 签名仅限本机/自用分发；要过 Gatekeeper 公证需开发者证书，暂无此需求。
- 壳被 `kill -9` 时来不及回收 server 子进程，靠下次启动的清端口逻辑兜底；SIGTERM/SIGINT 已挂 DispatchSource 走正常退出路径。

## 关联

- [adr-003 messages 表退役](adr-003-messages-表退役.md)（转录即事实源，是「重启服务代价可接受」的前提）
- README §macOS 桌面版（用户可见行为）、§安全姿态（暴露面说明）
