# 前端工具索引（lib/ + hooks/）

本文件是前端可复用逻辑的**唯一索引**。写任何新逻辑之前先查这里——同类工具已存在就复用；
新增/删除 lib/ 或 hooks/ 下的文件**必须同步更新本表**（`make check-structure` 会对账，缺条目直接 fail）。

收录规则见 [web/AGENTS.md](../../AGENTS.md) §1：纯函数进 `lib/`，React 逻辑进 `hooks/`，
≥2 处需要的逻辑必须沉淀到这里，不许在组件里复制。

## lib/ — 纯函数与客户端（无 JSX、不依赖组件）

| 文件              | 职责                                                                  | 关键导出                                                             |
| ----------------- | --------------------------------------------------------------------- | -------------------------------------------------------------------- |
| api.ts            | 后端 API 客户端，全部 HTTP/SSE/ws 地址的唯一出口；组件禁止裸 fetch    | `api`、`ApiError`、`Paged`                                           |
| chat/chat-events.ts | 聊天 SSE 事件 reducer（纯函数）与聊天状态类型；seq 去重在 use-chat  | `reduceChatEvent`、`ChatState`、`INITIAL_CHAT_STATE`、`mergeInputs`  |
| clipboard.ts      | 复制到剪贴板；非安全上下文（局域网 http）静默退让                     | `copyText`                                                           |
| db-uri.ts         | 连接 URI 解析（Navicat 的 `navicat://` 与通用 `mysql://`）→ 表单字段；导出在后端 | `parseDbUri`、`ParsedUri`                                            |
| db-result.ts      | 数据库 MCP 工具输出文本 → 结构化结果（对话里渲染真表格用）；格式与后端 datasource/render.go 共同约定 | `parseDbToolOutput`、`isDbQueryCall`、`ParsedDbResult`              |
| elicitation.ts    | elicitation JSON Schema 解析成结构化问题与作答收集                    | `parseElicitationSchema`、`answerFor`                                |
| files.ts          | 浏览器文件 → base64 图片附件、剪贴板取图                              | `fileToImageAttachment`、`imagesFromClipboard`                       |
| chat/first-send.ts | 草稿页 → 会话页的首发交棒：乐观用户消息、派发状态与失败通知          | `stashFirstSend`、`claimFirstSend`、`optimisticUserMessage`、`isOptimisticMessage` |
| local-commands.ts | 本地斜杠命令（前端自己执行、不发给 agent）的解析与补全清单合并        | `parseLocalCommand`、`withLocalCommands`、`LOCAL_COMMANDS`          |
| format.ts         | 时间/数字/字符串格式化纯函数                                          | `formatRelativeTime`、`formatDateTime`、`formatTokens`、`formatBytes`、`capitalize`、`displayPath`、`relativePath` |
| git-status.ts     | git 变更 → 文件树着色：绝对路径映射与目录汇总（新增/修改/删除）       | `buildChangeMap`、`dirChangeKind`、`CHANGE_TONE`                     |
| mcp-tool.ts       | MCP 工具的读法：破坏性判定、JSON-RPC 请求拼装、响应拆解（协议错误与工具错误分开）；工具台与调用记录共用 | `isDestructive`、`toolFullName`、`buildToolCall`、`buildRequest`、`initialArgs`、`coerceArgs`、`isRequired`、`readResponse`、`prettyJSON`、`parseJSON` |
| line-diff.ts      | 行级 diff（LCS 对齐，大文件退化保护）                                 | `lineDiff`                                                           |
| chat/usage.ts     | 会话累计用量：把历史各轮的 turnUsage 相加（用量面板用）              | `sumSessionUsage`、`SessionUsageTotals`                              |
| chat/message-blocks.ts | 消息列表按类型聚合成渲染块（过程性消息折叠）                     | `groupMessages`                                                      |
| palette.ts        | 主题方案的注册、读写与应用（token 定义在 index.css）                  | `PALETTES`、`loadPalette`、`applyPalette`                            |
| path-tree.ts      | 一组带路径的条目 → 目录树（单子目录链压缩），变更面板等树形视图共用   | `buildPathTree`、`countFiles`、`PathTreeNode`                        |
| saved-layouts.ts  | 用户自存的工作区布局（localStorage）：存/读/删，上限 8 套             | `loadSavedLayouts`、`saveLayout`、`deleteLayout`                     |
| session-activity.ts | 会话活跃态的进程内广播：会话页把「正在跑一轮」告诉侧边栏，免去为一个状态点轮询 | `markSessionActive`、`subscribeSessionActivity`、`getActiveSessions` |
| session-groups.ts | 会话按工作目录分组（adr-007）：cwd 分桶、组内取最新、最多 5 组 × 5 条 | `groupSessionsByCwd`、`SessionGroup`、`MAX_GROUPS`                   |
| desktop.ts        | 桌面壳（macOS app）的原生通道：环境判定、启动偏好、系统通知（授权/发送/撤回），走 WKWebView 注入的消息口而非 HTTP | `isDesktop`、`desktopLaunch`、`desktopNotify`、`NotifyStatus`、`NOTIFICATION_ACTION_EVENT` |
| notify/prefs.ts   | 通知偏好读写（localStorage）：这台设备上的这个人想不想被打扰，不跨设备同步 | `loadNotifyPrefs`、`saveNotifyPrefs`、`NotifyPrefs`、`DEFAULT_NOTIFY_PREFS` |
| notify/in-page.ts | 页内通知形式（浏览器唯一可用的手段）：标题闪烁、Web Audio 合成提示音、「用户在不在看」判定 | `flashTitle`、`stopFlashTitle`、`playChime`、`isUserWatching` |
| notify/store.ts   | 通知中心的存量（模块级广播，内存态不落盘）：同 id 覆盖、按优先级排序（update 最高）、上限裁剪 | `pushNotice`、`dismissNotice`、`clearNotices`、`Notice`、`NoticeKind` |
| subagents.ts      | 子代理清单提取：两端形状（claude 的 Agent 调用 / codex 的独立 thread）归一成条目 | `collectSubagents`、`isSubagentWork`、`subagentLocations`、`SubagentEntry` |
| status-tone.ts    | 会话/agent 状态 → StatusDot 色调的统一映射                            | `SESSION_STATE_TONE`、`AGENT_STATUS_TONE`、`StatusTone`              |
| utils.ts          | 类名合并（shadcn 标配）                                               | `cn`                                                                 |

## hooks/ — 可复用 React 逻辑

| 文件                 | 职责                                                                                                            | 关键导出                                       |
| -------------------- | --------------------------------------------------------------------------------------------------------------- | ---------------------------------------------- |
| use-async-data.ts    | 「进页面拉一次」的加载样板：cancelled 守卫 + data/error；轮询和分页不适用                                       | `useAsyncData`                                 |
| use-paged-data.ts | 分页列表的标准接线：page/pageSize 状态 + 拉取 + 就地增删改                | `usePagedData`                                                       |
| use-active-sessions.ts | 订阅会话活跃态广播（侧边栏状态点呼吸用）                                    | `useActiveSessions`                            |
| use-chat.ts          | 会话流状态机：bootstrap、SSE 订阅、发送/排队/中止、设置与交互裁决                                               | `useChat`                                      |
| identity-context.ts  | 身份上下文与读取 hook（adr-007）：owner / 租户 / 被停用三态，provider 在 components/shell/identity-provider.tsx | `IdentityContext`、`useIdentity`、`useIsOwner` |
| use-draft-session.ts | 草稿态会话：agent/模型选择与首条消息落地建会话                                                                  | `useDraftSession`                              |
| use-mobile.ts        | 移动端断点判断（shadcn 生成）                                                                                   | `useIsMobile`                                  |
| use-version-watch.ts | 版本哨兵：后端换版本就报出新版本号，侧栏状态条就地给刷新入口（局域网访客不会自己刷新）                         | `useVersionWatch`                              |
| use-server-events.ts | 全局事件流 /api/events 的单一连接（模块级）+ 订阅 hook；含断线退避重连                                          | `useServerEvents`                              |
| use-notifications.ts | 通知：判断该不该打扰（偏好 + 用户在不在看这一页），落进通知中心并分派提示（桌面壳系统通知 / 浏览器标题闪烁 + 声音）；含系统通知回调的裁决处理 | `useNotifications`                             |
| use-notices.ts       | 订阅通知中心存量（useSyncExternalStore 接 lib/notify/store.ts）                                                | `useNotices`                                   |
