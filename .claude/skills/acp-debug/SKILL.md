---
name: acp-debug
description: acpp 的 ACP 协议排障手册。排查 agent 握手失败、会话 busy 卡死、中止无反应、技能未注入、设置不同步等协议层问题时使用；也覆盖 acp-probe.py 探针用法与转录 JSONL 的读法。关键词：协议调试、握手失败、busy 卡死、cancel 没反应、转录怎么看、probe 探针。
---

# ACP 协议排障手册

排查协议层问题的操作手册。协议知识本体（分层铁律、runtime 差异表、并发模型）在
[server/internal/acp/AGENTS.md](../../../server/internal/acp/AGENTS.md)——本 skill 只讲**怎么查**。

## 1. 三件排查工具

### 转录 JSONL（第一现场，永远先看这里）

每条会话的全部线级流量：`<dataDir>/transcripts/<数据库sessionId>.jsonl`。
dataDir 默认 `~/.acpp`，被设置页迁移过就以 `GET /api/system` 的 `dataDir` 为准
（本机曾迁到 `~/.acpp`，仓库内 `server/data/` 是旧目录，别看错）。

每行一帧：`{"ts":"…","dir":"send|recv","msg":{JSON-RPC}}`。常用检索：

```bash
# 一条会话的方法时间线（send→ / recv←）
jq -r '[.ts[11:19], .dir, (.msg.method // (.msg | if has("result") then "result#\(.id)" else "error#\(.id)" end))] | join("  ")' ~/.acpp/transcripts/38.jsonl

# 找错误帧
grep '"error"' ~/.acpp/transcripts/38.jsonl | jq .msg.error
```

界面上「工作区 → 日志面板」看的就是这个文件的实时尾巴。

### acp-probe.py（脱离本项目直连 agent）

`scripts/acp-probe.py`——绕过整个后端，直接 stdio 拉起 agent 走一遍
initialize → session/new → prompt，用于分辨「agent 本身坏了」还是「我们的代码坏了」。

### 服务日志

`make dev` 后：`${TMPDIR:-/tmp}/acpp-dev/server.log`（ACP_DEBUG=1 已开，
含 `acp:` 前缀的协议层告警：agent exited / session-load 回退 / 未处理 update kind）。

## 2. 场景 → 检查步骤

| 症状 | 依次检查 |
| --- | --- |
| agent 拉不起来 / 握手失败 | ① server.log 搜 `agent exited`（带 stderr 截断）② 转录看 initialize 有无应答、协议版本是否匹配 ③ acp-probe.py 直连同一命令排除环境问题（PATH/登录态） |
| 会话 busy 永久卡死 | ① 转录看最后一帧：`session/prompt` 发出后没有 result → agent 侧挂起 ② 有 result 但界面仍 busy → 前端丢了 turn_done，查 SSE（浏览器 Network 的 events 流）seq 是否断档 ③ server.log 搜该 session 的 `save stop reason` |
| 点中止没反应 | ① 转录应出现 `session/cancel` 通知帧 ② codex 的取消表现为 prompt 报 "context canceled" **错误字符串**（协议缺陷，turn.go/Interject 已归一）——若界面报错横幅而非静默收尾，说明归一没兜住新形态，把错误文本补进 `isCancelledErr` 的判断依据 |
| 技能没被注入 | ① 转录看 session/new 的 params：claude 应带 `additionalDirectories`/meta，codex 应在 spawn env 带 CODEX_HOME ② skillpack 目录：`ls <dataDir>/skillpack/skills/`（启停=符号链接增删）③ 对话里问 agent「你能看到哪些 skills」复核 |
| 设置/命令清单不同步 | ① 转录看 `session/update` 里 `current_mode_update`/`config_option_update`/`available_commands_update` 是否到达 ② 到达但界面没变 → 查 SSE settings/commands 事件与配置页取舍过滤（chat_settings.go） |
| 恢复会话丢上下文 | server.log 搜 `session/load failed`——load 失败会静默回退 session/new（上下文即丢），常见原因是 agent 侧线程被删或 CODEX_HOME 隔离目录变了 |

## 3. 复现与对照

- 同一操作在 claude 与 codex 各试一遍，差异表（acp/AGENTS.md §3）没覆盖的新差异，
  修完顺手补进那张表。
- 改 adapter 后跑 `cd server && go test ./internal/acp/`，形状断言在 adapter_test.go。
