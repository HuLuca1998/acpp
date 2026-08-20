# ADR-011：ACP 能力面补全——用满协议已到手的数据，probe 定夺存疑能力

日期：2026-08-20 ｜ 状态：已实施

## 背景

对 ACP 规范全集与本项目实现做了一次系统对照（codex-acp 1.1.x / claude-agent-acp 0.63+），
发现三类缺口：**数据已到手但没用**（locations、prompt 响应 usage、权限裁决原始帧）、
**能力没声明/没解析**（promptCapabilities、boolean configOptions、authRequired 错误码、cost）、
**整块未接且价值存疑**（terminal 代理、resource_link）。本 ADR 记录取舍与实测依据。

## 决策

### 1. 呈现层补全（不动协议）

- **locations 跟随**：后端一直把 tool_call 的 locations 带到 SSE，前端此前零消费。
  补四个消费点：消息流「正在触碰」小字（可点击定位到行）、文件树呼吸点（目录冒泡）、
  查看器跟随开关（默认关——自动抢焦点必须用户主动选）、子代理面板当前文件。
- **重建补全**：plan 轮末快照（kind=plan，历史折叠成一行进度，实时卡 turn_done 收起由它接力）、
  权限裁决记录（kind=permission_request，请求与答复按 JSON-RPC id 配对）、
  每轮 usage 挂到最后一段正文 payload.turnUsage。数据全部来自既有转录，重建器单方扩展。

### 2. 能力声明与解析

- **promptCapabilities**：解析进 Caps（握手写一次），随 Settings.prompt 出包。
  方言兜底住 adapter：claude/codex 的 image/embeddedContext 实测恒真（probe 确认声明
  也如此），generic 严格按声明。发送前收口：resource 降级 text、图片报错。
- **session.configOptions.boolean**：声明之。前置条件是解析兼容——ConfigOption.CurrentValue
  改 FlexString（接受字符串/原生布尔）。两端 dist 源码核对：布尔模式下 set 侧仍接受
  "on"/"off" 字符串，无需分叉。
- **authRequired（-32000）**：conn.Call 改 %w 保留 rpcError 链，哨兵 ErrAuthRequired
  按错误码匹配（不嗅探 message）；httpapi 映射 424；前端按 flavor 给登录引导卡。
- **cost**：usage_update 的 cost 只有 claude 间歇带，作为可选装饰透传（codex 缺省不显示）。
  不违反交集规范——交集约束的是「承诺的统一能力」，可选展示不在其内。

### 3. probe 实测定夺的两项（2026-08-20，脚本为 scripts/acp-probe.py 的临时变体）

| 能力 | claude-agent-acp | codex-acp | 结论 |
| --- | --- | --- | --- |
| terminal 代理（声明 terminal:true） | 仍自带 shell 跑命令，**零 terminal/* 反向调用** | 同左 | **不做**。实现五个 handler 只会是死代码；adapter 行为变了再评估 |
| resource_link 内容块 | 自带 Read 按路径读取，内容复述一字不差 | shell 读取，同样精准 | **做**。超 32KB 的 @ 文件改发 resource_link 按需读取，省 token |

### 4. 维持不接（有据）

- **audio 内容块**：两端 promptCapabilities 都不声明 audio，发了也被拒；无产品场景。
- **elicitation url 模式**：继续一律 cancel；接入时机是挂第一个需要 OAuth 的远程 MCP 时
  （届时补 capability 声明 + 一张「去浏览器完成授权」卡即可，成本很低）。
- **session/set_model、codex 顶层 models、session_info_update 的 title**：已有正确替代
  （见 adr-001 与标题服务），重新接入只会破坏跨端一致性。

## 后果

- SSE 契约新增：tool_call 的 `locations`、settings 的 `prompt`、usage 的 `cost`；
  移除死事件 `message_saved`。消息种类新增 `plan` / `permission_request`。
- 一次 probe 顺带确认了 claude 的完整 agentCapabilities（sessionCapabilities 的
  delete/resume/fork/list/close、mcpCapabilities http/sse、auth.logout），记录在
  协议实测记忆与 acp/AGENTS.md 差异表。
