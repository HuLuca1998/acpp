# ADR-001：codex 与 claude 的 runtime 差异收敛——统一词汇表 + Adapter

- 状态：已采纳（2026-08-11）
- 背景：对话基建与 codex-acp 接入已完成，现要接入 claude（`@agentclientprotocol/claude-agent-acp`）。目标是**同一份上层代码同时驱动两个 runtime，差异停留在实现内部**。
- 实测依据：codex-acp 1.1.7 与 claude-agent-acp 0.63.0 的 probe 往返（2026-08-11），另参考 `~/Documents/obsidian/30.tech/ACP协议/` 的既有探针结论（2026-07-31 同版本）。

## 一、差异清单（实测）

协议（JSON-RPC 形状、核心方法、loadSession、多轮上下文、stopReason 语义）两端一致，**不需要抽象**。语义与形状差异如下：

| # | 差异 | codex 1.1.7 | claude 0.63.0 |
| --- | --- | --- | --- |
| 1 | 权限档 | 3 档（`read-only`/`agent`/`agent-full-access`），默认 `agent`——**写文件跑命令不问** | 6 档（`auto`/`default`/`acceptEdits`/`plan`/`dontAsk`/`bypassPermissions`），默认 `default`——危险操作会问 |
| 2 | 模型清单位置 | `session/new` 顶层 `models`（25 个=模型×推理档笛卡尔积）+ configOptions `model` 项 | **无顶层 `models`**，只有 configOptions `model` 项 |
| 3 | 模型切换方法 | `session/set_model` 与 `set_config_option` 写同一状态、互相覆盖 | **无 `session/set_model`**（Method not found） |
| 4 | configOption id | `reasoning_effort` / `fast-mode` / `collaboration_mode` | `effort` / `fast` / `agent`；**category 两端一致**（`mode`/`model`/`thought_level`） |
| 5 | 思考深度选项 | low/medium/high/xhigh/max/**ultra** | **default**/low/medium/high/xhigh/max |
| 6 | plan 模式 | 独立配置项 `collaboration_mode=plan` | 权限档 `mode=plan`；退出时以 **ExitPlanMode 权限请求**出现（选项即模式切换） |
| 7 | `rawOutput` 形状 | 对象 `{formatted_output, exit_code}` | **字符串** |
| 8 | usage 字段 | 有 `thoughtTokens`，无 cost | 有 `cachedWriteTokens` + `usage_update` 通知里间歇带 `cost:{amount,currency}` |
| 9 | `usage_update.size` 语义 | 会话水位 | 模型上下文窗口 |
| 10 | permission 请求的 `toolCall` | 仅 `toolCallId`/`kind`/`status` | 富字段：`title`/`rawInput`/`content`(diff)/`locations`/`_meta.claudeCode.toolName` |
| 11 | elicitation 自由输入标记 | `_meta.codex.isOtherAnswer` + `__other` 字段 | `question_<n>_custom` 命名约定（title="Other"） |
| 12 | fs 代理 | 完全不走（自带 shell） | 0.63 实测 Write 也**不再走**（权限批准后 SDK 自己落盘；旧版 0.16 会走） |
| 13 | `session_info_update` | `_meta.codex.threadStatus` | `{title, updatedAt}`（自动会话标题） |
| 14 | 嵌套会话标记 | `CODEX_SANDBOX*` | `CLAUDECODE` / `CLAUDE_CODE_*` |

diff 的形状两端一致：`content: [{type:"diff", path, oldText, newText}]`（claude 的 `oldText` 新建文件时为 `null`）。elicitation 通道一致（`elicitation/create`，mode=form，`{字段id: 选项label}` 回传）。

## 二、决定：交集规范 + 统一词汇表 + per-runtime Adapter

**总原则（交集规范）：统一接口只收录两条 ACP 都能做到的功能；有一端做不到的，功能废弃。**
上层（service、httpapi、前端）只认统一词汇表；每个 runtime 一个 Adapter 实现，把统一概念翻译成自己的协议调用。`if flavor == ...` 只允许出现在 adapter 实现文件里。

### 统一词汇表（全部两端可达）

| 概念 | 取值 | claude 映射 | codex 映射 |
| --- | --- | --- | --- |
| `Effort` 思考深度 | `low`/`medium`/`high`/`xhigh`/`max`（两端交集恰好五档） | configOption `effort` | configOption `reasoning_effort` |
| `AccessLevel` 权限档 | `safe`/`auto-edit`/`full` 三档，两端全覆盖 | safe←→`default`、auto-edit←→`acceptEdits`、full←→`bypassPermissions` | safe←→`read-only`、auto-edit←→`agent`、full←→`agent-full-access` |
| Plan 模式 | 独立 bool 开关（不并入 AccessLevel——claude 退出 plan 要回落到某个档，独立开关语义干净） | `set_mode(plan)` / 退出回 `set_mode(default)` | `set_config_option(collaboration_mode, plan/default)` |
| Fast 模式 | bool | configOption `fast` | configOption `fast-mode` |
| `Model` | `{id, name, description}`，**id 透传不映射** | configOption `model` | configOption `model`（**不用**顶层 models 笛卡尔积，**不用** set_model） |
| 上下文用量 | `{used, size}`（usage_update，两端都发；size 语义有出入但按占比展示两端都成立） | 同 | 同 |

**按交集规范废弃的**（是决定，不是遗漏）：

- `cost` 成本显示——只有 claude 报，废弃；
- `thoughtTokens`（codex 独有）、`cachedWriteTokens`（claude 独有）——Usage 只保留两端交集四字段；
- claude 的 `agent` persona 配置项——词汇表外配置不再透传（原 Extra 逃生舱概念整个删除）；
- claude 的 `auto`/`dontAsk` 权限档、codex 的 `ultra` 推理档、claude effort 的 `default` 值（currentValue=default 时统一视图 CurrentEffort 留空）。

### 模型选择的产品形态

- **新会话（草稿态）**：跨 ACP 的分组模型清单（按 agent 分组），选模型即选定 agent+model；首条消息落地时按所选 agent 建会话并应用模型。
- **会话进行中**：只能切换当前 ACP 的模型（会话绑定 agent，`Settings.Models` 天然只含本端清单）。
- 实现支撑：模型清单要在建会话**之前**可得，Agent 记录上缓存模型清单（注册/更新 agent 后异步探测：spawn 临时会话 → 读 caps → 关闭；探测失败清单为空，不阻塞注册）。

### 接口

```go
type Adapter interface {
    Flavor() Flavor                 // codex | claude | generic
    Settings(caps Caps) Settings    // 读：从能力快照提取统一视图
    SetModel(ctx, *Session, id string) error
    SetEffort(ctx, *Session, Effort) error
    SetAccessLevel(ctx, *Session, AccessLevel) error
    SetPlan(ctx, *Session, on bool) error
    SetFast(ctx, *Session, on bool) error
    SetExtra(ctx, *Session, configID, value string) error // 词汇表外配置的通道
}
```

`Settings` 统一视图：models/efforts/levels 及各自 current、plan/fast 的 supported+on，`Extra []ConfigOption` 透传词汇表外的配置项（如 claude 的 `agent` persona）。空切片=该 runtime 不支持此维度，前端隐藏控件。

Flavor 识别：`initialize` 响应的 `agentInfo.name` + 启动命令名双信号，认不出 → `generic`。

**genericAdapter**（未知 runtime 的降级）：按 `category` 试探（`model`/`thought_level` 的 category 两端一致，是 ACP 生态惯例，新 runtime 守约即自动工作）；效果值在词汇表内才收，其余全进 `Extra`；modes 合成伪配置项 `__mode` 进 Extra，写入时转调 `set_mode`；不支持的 Set* 返回 `ErrUnsupported`。

### 上层契约

- `Manager.Settings(key)` / `Manager.Apply(key, SettingsPatch)` 取代 `SetMode`/`SetModel`/`SetConfigOption` 三个导出方法；原语下沉为 Session 私有方法供 adapter 调用。
- HTTP：`set-mode`/`set-model`/`set-config` 三端点收敛为 `PUT /api/sessions/{id}/settings`，body 为逐项可选的 patch，响应带最新 `Settings`。
- SSE：`mode`/`config` 两个 kind 合并为 `settings`（agent 自行切档/改配置时推全量统一视图）；新增 kind `usage`（`{used,size,cost?}`）；`turn_end` 补透传 usage；`tool_call` 补 `content`（diff）/`locations`；`permission` 补 `choice` 与富 `title`/`rawInput`。
- REST 其余端点、数据模型、消息重建不变（usage 不落库，转录里有原始数据）。

### 交互差异的收敛点（2026-08-11 补充实测）

- **turn 进行中插话（Interject）**：两端通道不同，收敛在 adapter——
  claude 用 **promptQueueing**（turn 中再发 session/prompt 会排队成独立一轮，
  有自己的 stopReason/usage；不用 `_session/steering`，实测 0.63 注入后被
  抢占轮提前 end_turn、插话内容没有轮次边界与结束信号）；codex 用
  **`_session/steering`**（注入当前轮、内容并入当前轮统一收尾；不用并发
  prompt，实测第二个请求的响应会永远悬着）。generic 不支持。
- **ExitPlanMode（计划完成审批）**：claude 独有交互但走两端一致的权限通道，
  识别信号是 ACP 标准的 `toolCall.kind == "switch_mode"`；选项映射
  （claude 档位名 → 统一三档，`auto` 丢弃）在 claudeAdapter.PlanReview。
- **session/delete**：两端都支持，删除会话时尽力清 agent 侧线程历史
  （进程活着直调；死了拉临时进程；失败只记警告不阻塞本地删除）。
- **斜杠命令**：`available_commands_update` 两端都推全量清单，发送就是普通
  文本两端都认；通知只在会话建立后推一次，必须存快照否则页面刷新即丢。

### 形状差异的收敛点（不涉及概念映射的部分）

- `Usage` 结构：`cachedWriteTokens`/`thoughtTokens`/`cost` 用指针，nil = 「该 runtime 结构性不报」≠ 0，前端遇 nil 隐藏而不是显示 $0。
- `rawOutput` 双形状（对象/字符串）：前端 `tool-call.tsx` 兼容。
- elicitation 自由输入双标记：前端 `lib/elicitation.ts` 兼容（`_meta.codex.isOtherAnswer` 与 `<qid>_custom` 命名）。
- `session_info_update` 显式丢弃（claude 自动标题与本项目「首条消息简写」策略重复）。
- fs 代理与 path guard 保留但不再宣称是防线（两端实测都会绕过），README 安全姿态同步修正。

## 三、验证

- adapter 映射表用真实 probe 数据做表驱动测试（两端 Settings 提取 + patch 翻译）。
- probe 脚本沉淀为 `scripts/acp-probe.py`，版本升级后重跑对照差异清单——**这些语义差异会随版本漂移**（codex 档位名 0.16→1.1.7 已改过一次）。
- 端到端：两个 runtime 各注册一个 agent，各跑一轮含工具调用的对话，核对统一设置控件、流式 diff、usage 显示、elicitation 作答、权限卡片。
