# 后端包地图与工具索引（internal/）

本文件是后端包职责的**唯一索引**。动手前先查这里定位该去的包；
新增/删除 internal/ 下的包**必须同步更新本表**（`make check-structure` 会对账，缺条目直接 fail）。

分层与依赖方向见 [server/AGENTS.md](../AGENTS.md) §1。本项目不设 utils 杂物包：
通用纯函数就近放在使用它的包里，被 ≥2 个包需要时提为具名叶子包并登记在此。

## 包地图

| 包 | 职责 | 层 |
| --- | --- | --- |
| acp | ACP 协议客户端：JSON-RPC 连接、会话池、adapter（claude/codex/generic 差异）、技能隔离注入。不 import 本项目其他包 | 叶子 |
| config | 环境变量配置、数据目录准备与迁移、路径工具 | 叶子 |
| db | GORM 连接与 AutoMigrate | 基础 |
| model | 数据模型（Agent / Session / Message / SkillUsage）与 JSON 字段类型 | 基础 |
| transcript | 会话转录 JSONL 的追加与读取（对话内容唯一的持久化） | 叶子 |
| stream | SSE 事件形状（Event）与广播器（Broker）：多订阅者、轮内重放、慢订阅丢弃。聊天与编排两个业务包共用 | 叶子 |
| mcp | 我方 MCP server 的协议外壳：JSON-RPC 信封、工具声明与分发（initialize/ping/tools.list/tools.call）。编排与数据源两个业务包共用，业务包只提供工具集 | 叶子 |
| service | 普通会话的业务规则：会话/对话/技能/工作区/终端/agent 配置；多租户身份与隔离范围（Scope） | 业务 |
| project | 工作区项目（adr-007）：git 仓库发现、克隆（租户禁用凭证助手）、gh 远端仓库清单。磁盘即事实源，不入库；借 service 的哨兵错误与 Scope | 业务 |
| datasource | 外部 MySQL 数据源（adr-008）：连接配置（项目 + 环境两级）、SSH 隧道拨号、库表探查、多段语句执行，以及挂给会话的 MCP 工具面。连接一次性、可见性按会话 cwd 所属项目过滤。借 service 的哨兵错误与 DefaultCwd | 业务 |
| orch | 编排（adr-006）：角色、编排主会话、spawn 任务子会话、系统 MCP 端点。借 service 的哨兵错误与导出工具（DegradedSettings/DeriveTitle/TruncateError/RebuildMessages），不被 service 反向依赖 | 业务 |
| system | 系统平台面：数据目录迁移、环境体检与依赖安装、版本检查与自更新。哨兵错误借 service 的（错误映射一套） | 业务 |
| httpapi | 路由、handler、中间件、统一响应。不碰 db，服务由 cmd/server 装配后传入 | HTTP |

## 跨包可复用工具

| 函数 | 位置 | 用途 |
| --- | --- | --- |
| `config.SamePath` | config/datadir.go | 两个路径解析绝对路径后是否同一位置 |
| `config.CopyDirFiles` | config/datadir.go | 目录内文件逐个拷贝（数据迁移用） |
| `acp.EnsureCodexHome` | acp/isolation.go | 幂等搭隔离用 codex 家目录（auth 软链/config 复制/技能包软链），编排的专属 home 复用 |
| `service.RebuildMessages` | service/rebuild.go | 线级转录 → UI 消息列表的重建器（普通会话与编排会话共用） |
| `service.DegradedSettings` | service/chat_settings.go | 用探测缓存 + 设置快照拼未连接会话的设置视图 |
| `service.DeriveTitle` | service/chat_turn.go | 首条消息 → 自动会话标题 |
| `service.TruncateError` | service/chat.go | 错误文本截断（落库字段的长度保护） |

包内的私有 helper（如 service 的 `truncateError`、acp 的 `truncate`）不在此登记——
需要跨包复用时先提升为上表的具名工具再使用。
