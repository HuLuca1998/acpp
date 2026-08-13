# acp 包规范（ACP 协议客户端）

全项目知识最密的包：JSON-RPC 连接、会话池、两个 runtime 的方言适配、技能隔离。
动这里之前先读完本文件；通用后端规范见 [../../AGENTS.md](../../AGENTS.md)。

## 1. 铁律

- **本包不 import 本项目任何其他包**（标准库除外）。它是叶子，反向依赖直接破坏分层。
- **runtime 差异全部住在 adapter**（`adapter_claude.go` / `adapter_codex.go` / `adapter_generic.go`）。
  manager / session / turn 只说统一词汇（Settings / Effort / AccessLevel / Interject），
  出现 `if flavor == ...` 的分支就该下沉到 adapter。
- **上层只认统一视图**：`Caps` 原始快照不出包，出包的是 adapter 提取的 `Settings`；
  错误上抛哨兵（`ErrBusy` / `ErrNoSession`）或 `StopReason`，不要让上层解析错误字符串
  ——cancel 的字符串嗅探补偿集中在 `Interject` 一处（codex 协议缺陷），不许扩散。

## 2. 文件地图

| 文件 | 职责 |
| --- | --- |
| protocol.go | JSON-RPC 与 ACP 线级类型（请求/响应/通知的形状） |
| conn.go | stdio 连接：ndjson 读写、请求关联、反向调用路由、进程关闭宽限 |
| runtime.go | Runtime 注册表 + 嵌套环境变量清理 |
| event.go | 归一化事件模型（EventKind + Event），推给上层的唯一事件形状 |
| session.go | Session 状态体：能力快照、协议原语（setMode/setConfigOption）、prompt/steering 裸调用 |
| manager.go | 会话池：Open（并发去重）、握手（load 恢复优先）、Close/Idle、远端删除 |
| turn.go | 轮次执行：Prompt/Interject/Cancel、turn 排他、设置门面（Commands/Settings/Apply） |
| updates.go | agent 反向调用：session/update 归一化、权限与 elicitation 的挂起/裁决 |
| fsproxy.go | fs 代理（读写限制在会话 cwd 内，纵深防御的一层——codex 不走这里） |
| adapter.go | 统一词汇表 + Adapter 接口（模型/思考深度/权限档/plan/fast/插话/PlanReview） |
| adapter_*.go | claude / codex / generic 三个实现 |
| isolation.go | 技能隔离注入（机器级屏蔽 / 技能包 / 项目级保留，按方言给 Env/Meta/AdditionalDirs） |

## 3. 两个 runtime 的关键差异（改 adapter 必读）

| 维度 | claude | codex |
| --- | --- | --- |
| 能力快照来源 | session/new 返回 `modes` | session/new 返回 `configOptions`（全量覆盖式更新） |
| 用户中止 | 按规范返回 stopReason=cancelled | 让在途调用报 "context canceled" 错误字符串，需归一（turn.go） |
| turn 中插话 | promptQueueing：排队成独立一轮（followUp=true） | `_session/steering`：注入当前轮（followUp=false） |
| 权限请求 | 带 Title/RawInput/Content；ExitPlanMode 走同通道 | 只有 options，其余字段空 |
| 技能隔离 | 项目级 skillpack 目录注入 | CODEX_HOME 整体隔离 |
| fs 代理 | 走（声明的 fs capability 会被真的调用） | 不走（自带 shell） |

## 4. 新增一个 agent 支持的步骤

1. `flavorOf`（adapter.go）加身份识别（agent 自报名 + 启动命令双信号）。
2. 新建 `adapter_<name>.go` 实现 Adapter 接口；能力缺失的维度返回空清单（前端自动隐藏控件）。
3. 有技能隔离诉求则在 `isolation.go` 给该方言实现 Isolation。
4. 补 `adapter_test.go` 表驱动用例（Settings 提取与 Set* 的协议调用形状）。
5. 跑通探测：注册 agent 后 ProbeAgent 应能拿到模型清单与设置骨架。

## 5. 并发模型备忘

- 一条会话一个 turn：`turnMu.TryLock` 排他，busy 时上层走 Interject。
- `activeCalls`/`lastDone` 支撑空闲回收判定；权限/提问挂起发生在 prompt 调用之内，天然被覆盖。
- 挂起交互（permissions/elicitations map）由 `sess.mu` 保护，应答 chan 容量 1，谁摘除谁投递。
- conn 的 pending 关闭责任在 readLoop 单点；`exitErr` 先于 `close(done)` 写入。
