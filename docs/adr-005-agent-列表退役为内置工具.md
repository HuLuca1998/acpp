# ADR-005：Agent 列表退役——claude/codex 作为内置工具进设置页

日期：2026-08-13　状态：已采纳

## 背景

原产品形态是开放的 Agent 注册列表（导航一级入口 + 列表页 + 详情配置页），用户可添加任意 ACP 命令。实际使用暴露两个问题：

1. 产品定位就是对接 claude / codex 两条 runtime，「注册任意 agent」是伪自由度——它们在产品语义里不是可增删的 agent，而是**固定内置的工具**（用户定性）。
2. 清空数据库后 agent 列表为空，新会话无从发起，「先注册才能用」的空态是全新/清库用户的第一道坎。

## 决策

- **导航删除 Agent 入口**，`/agents` 列表页与 `/agents/:id` 详情页退役。
- **设置页固定两个工具分区**（系统 / Claude / Codex）：原详情页的配置面整体迁入
  `components/settings/agent-tool-config.tsx`，并补上此前缺失的**启动命令编辑**
  （保存后自动重探能力）；分区支持 `?section=` 深链，概览工具卡直达。
- **后端启动时预置**：`AgentService.EnsureDefaults` 在缺失时补建 claude
  （`claude-agent-acp`）与 codex（`codex-acp`）两条记录——按 name 判存，
  **绝不覆盖**用户改过的配置；新建的后台探测一次能力缓存。清库/全新安装开箱即有。
- **API 与数据模型不动**：`/api/agents` 仍是通用 CRUD，agents 表结构不变。
  收敛发生在 UI 层，将来若要接第三方 ACP agent，API 能力仍在，届时再做入口。

## 后果

- 前端删两个路由页，i18n 移除列表页文案；概览「Agent」卡改为「工具」卡，链接指设置分区。
- 「添加 agent」流程消失；改命令走设置分区的命令编辑（含参数，空格分隔）。
- 会话侧不受影响：草稿态模型下拉仍按 agent 分组（数据源即这两条记录）。

## 关联

- [adr-001 codex/claude 差异收敛](adr-001-codex-claude-差异收敛.md)（统一词汇表是「双内置工具」可行的前提）
- [adr-004 macOS 桌面壳](adr-004-macos-桌面壳.md)（桌面版开箱即用同样依赖预置）
