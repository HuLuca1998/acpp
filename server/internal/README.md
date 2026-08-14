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
| service | 全部业务规则：会话/对话/技能/工作区/终端/agent 配置 | 业务 |
| system | 系统平台面：数据目录迁移、环境体检与依赖安装、版本检查与自更新。哨兵错误借 service 的（错误映射一套） | 业务 |
| httpapi | 路由、handler、中间件、统一响应。不碰 db，服务由 cmd/server 装配后传入 | HTTP |

## 跨包可复用工具

| 函数 | 位置 | 用途 |
| --- | --- | --- |
| `config.SamePath` | config/datadir.go | 两个路径解析绝对路径后是否同一位置 |
| `config.CopyDirFiles` | config/datadir.go | 目录内文件逐个拷贝（数据迁移用） |

包内的私有 helper（如 service 的 `truncateError`、acp 的 `truncate`）不在此登记——
需要跨包复用时先提升为上表的具名工具再使用。
