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
| model | 数据模型（Agent / Session / Message / SkillUsage / MCPCall）与 JSON 字段类型 | 基础 |
| transcript | 会话转录 JSONL 的追加与读取（对话内容唯一的持久化） | 叶子 |
| titler | 会话标题生成：把首句派生的标题换成本机小模型（ollama）给的概括。两端 agent 的自动标题都长在各自 CLI 层，ACP 通道取不到，所以由本项目自己算。不 import 本项目其他包 | 叶子 |
| stream | SSE 事件形状（Event）与广播器（Broker）：多订阅者、轮内重放、慢订阅丢弃。会话流（service）用 | 叶子 |
| mcp | 我方 MCP server 的协议外壳：JSON-RPC 信封、工具声明与分发（initialize/ping/tools.list/tools.call）。数据源业务包提供工具集，协议外壳与之解耦 | 叶子 |
| mcpcall | MCP 工具调用的观测记录与统计：谁调的、传了什么、拿回什么、花多久。工具台读它，数据源工具面写它（经窄接口，两包不互相 import）。留存有上限，长文本落库前截断 | 业务 |
| service | 普通会话的业务规则：会话/对话/技能/工作区/终端/agent 配置；多租户身份与隔离范围（Scope） | 业务 |
| project | 工作区项目（adr-007）：git 仓库发现、克隆（租户禁用凭证助手）、gh 远端仓库清单。磁盘即事实源，不入库；借 service 的哨兵错误与 Scope | 业务 |
| datasource | 外部 MySQL 数据源（adr-008）：连接配置（项目 + 环境两级）、SSH 隧道拨号、库表探查、多段语句执行，以及挂给会话的 MCP 工具面。连接一次性、可见性按会话 cwd 所属项目过滤。借 service 的哨兵错误与 DefaultCwd | 业务 |
| upload | 本机文件上传：落盘、按内容 hash 去重、列举与删除。上传件存在各自身份的家目录下，**隔离由路径本身给**——没有归属过滤这回事。借 service 的 Scope 与哨兵错误 | 业务 |
| system | 系统平台面：数据目录迁移、环境体检与依赖安装、版本检查与自更新。哨兵错误借 service 的（错误映射一套） | 业务 |
| httpapi | 路由、handler、中间件、统一响应。不碰 db，服务由 cmd/server 装配后传入 | HTTP |

## 跨包可复用工具

| 函数 | 位置 | 用途 |
| --- | --- | --- |
| `config.SamePath` | config/datadir.go | 两个路径解析绝对路径后是否同一位置 |
| `config.CopyDirFiles` | config/datadir.go | 目录内文件逐个拷贝（数据迁移用） |
| `service.RebuildMessages` | service/rebuild.go | 线级转录 → UI 消息列表的重建器 |
| `service.DegradedSettings` | service/chat_settings.go | 用探测缓存 + 设置快照拼未连接会话的设置视图 |
| `service.DeriveTitle` | service/chat_turn.go | 首条消息 → 自动会话标题 |
| `service.TruncateError` | service/chat.go | 错误文本截断（落库字段的长度保护） |

包内的私有 helper（如 service 的 `truncateError`、acp 的 `truncate`）不在此登记——
需要跨包复用时先提升为上表的具名工具再使用。
