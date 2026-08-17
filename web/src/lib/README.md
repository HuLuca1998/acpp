# 前端工具索引（lib/ + hooks/）

本文件是前端可复用逻辑的**唯一索引**。写任何新逻辑之前先查这里——同类工具已存在就复用；
新增/删除 lib/ 或 hooks/ 下的文件**必须同步更新本表**（`make check-structure` 会对账，缺条目直接 fail）。

收录规则见 [web/AGENTS.md](../../AGENTS.md) §1：纯函数进 `lib/`，React 逻辑进 `hooks/`，
≥2 处需要的逻辑必须沉淀到这里，不许在组件里复制。

## lib/ — 纯函数与客户端（无 JSX、不依赖组件）

| 文件              | 职责                                                                  | 关键导出                                                             |
| ----------------- | --------------------------------------------------------------------- | -------------------------------------------------------------------- |
| api.ts            | 后端 API 客户端，全部 HTTP/SSE/ws 地址的唯一出口；组件禁止裸 fetch    | `api`、`ApiError`、`Paged`                                           |
| chat-events.ts    | 聊天 SSE 事件 reducer（纯函数）与聊天状态类型；seq 去重在 use-chat    | `reduceChatEvent`、`ChatState`、`INITIAL_CHAT_STATE`、`mergeInputs`  |
| clipboard.ts      | 复制到剪贴板；非安全上下文（局域网 http）静默退让                     | `copyText`                                                           |
| db-result.ts      | 数据库 MCP 工具输出文本 → 结构化结果（对话里渲染真表格用）；格式与后端 datasource/render.go 共同约定 | `parseDbToolOutput`、`isDbQueryCall`、`ParsedDbResult`              |
| elicitation.ts    | elicitation JSON Schema 解析成结构化问题与作答收集                    | `parseElicitationSchema`、`answerFor`                                |
| files.ts          | 浏览器文件 → base64 图片附件、剪贴板取图                              | `fileToImageAttachment`、`imagesFromClipboard`                       |
| local-commands.ts | 本地斜杠命令（前端自己执行、不发给 agent）的解析与补全清单合并        | `parseLocalCommand`、`withLocalCommands`、`LOCAL_COMMANDS`          |
| format.ts         | 时间/数字/字符串格式化纯函数                                          | `formatRelativeTime`、`formatDateTime`、`formatTokens`、`capitalize`、`displayPath` |
| git-status.ts     | git 变更 → 文件树着色：绝对路径映射与目录汇总（新增/修改/删除）       | `buildChangeMap`、`dirChangeKind`、`CHANGE_TONE`                     |
| line-diff.ts      | 行级 diff（LCS 对齐，大文件退化保护）                                 | `lineDiff`                                                           |
| message-blocks.ts | 消息列表按类型聚合成渲染块（过程性消息折叠）                          | `groupMessages`                                                      |
| palette.ts        | 主题方案的注册、读写与应用（token 定义在 index.css）                  | `PALETTES`、`loadPalette`、`applyPalette`                            |
| path-tree.ts      | 一组带路径的条目 → 目录树（单子目录链压缩），变更面板等树形视图共用   | `buildPathTree`、`countFiles`、`PathTreeNode`                        |
| saved-layouts.ts  | 用户自存的工作区布局（localStorage）：存/读/删，上限 8 套             | `loadSavedLayouts`、`saveLayout`、`deleteLayout`                     |
| session-groups.ts | 会话按工作目录分组（adr-007）：cwd 分桶、组内取最新、最多 5 组 × 5 条 | `groupSessionsByCwd`、`SessionGroup`、`MAX_GROUPS`                   |
| status-tone.ts    | 会话/agent 状态 → StatusDot 色调的统一映射                            | `SESSION_STATE_TONE`、`AGENT_STATUS_TONE`、`StatusTone`              |
| utils.ts          | 类名合并（shadcn 标配）                                               | `cn`                                                                 |

## hooks/ — 可复用 React 逻辑

| 文件                 | 职责                                                                                                            | 关键导出                                       |
| -------------------- | --------------------------------------------------------------------------------------------------------------- | ---------------------------------------------- |
| use-async-data.ts    | 「进页面拉一次」的加载样板：cancelled 守卫 + data/error；轮询和分页不适用                                       | `useAsyncData`                                 |
| use-chat.ts          | 会话流状态机：bootstrap、SSE 订阅、发送/排队/中止、设置与交互裁决                                               | `useChat`                                      |
| identity-context.ts  | 身份上下文与读取 hook（adr-007）：owner / 租户 / 被停用三态，provider 在 components/shell/identity-provider.tsx | `IdentityContext`、`useIdentity`、`useIsOwner` |
| use-draft-session.ts | 草稿态会话：agent/模型选择与首条消息落地建会话                                                                  | `useDraftSession`                              |
| use-mobile.ts        | 移动端断点判断（shadcn 生成）                                                                                   | `useIsMobile`                                  |
| use-orch-chat.ts     | 编排主会话流状态机（adr-006）：复用 chat-events reducer + task_update 任务列表；返回形状与 useChat 结构兼容     | `useOrchChat`                                  |
| use-task-chat.ts     | 编排任务子会话的只读观察流：SSE + 转录重建，权限/提问可裁决，不能发消息                                         | `useTaskChat`                                  |
