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
| sshdial | SSH 拨号：认证方式组装（密码/公钥/both，公钥留空走 ssh-agent）、known_hosts 指纹校验（accept-new 语义）、连接建立。数据源的隧道与服务器观察共用。不 import 本项目其他包，参数类错误用自带的 ErrInvalid 表达 | 叶子 |
| gitrepo | git 仓库的身份与地址：地址校验（ValidCloneURL，挡 file:// 等危险传输）、URL → `<组织>/<仓库>`（Name）、**目录 → 项目**（ProjectOf：往上找仓库、读 origin、剥工作树段）。**「项目就是一个 git 仓库」这条定义的执行点**，克隆、数据源归属、会话归属共用；不 import 本项目其他包 | 叶子 |
| ghcli | 本机 gh CLI 的薄封装：定位可执行文件（PATH 之外翻 homebrew 落点）、跑一条命令、把「没装」「没登录」译成哨兵错误。project（仓库清单）与 github（issue 列表）共用。不 import 本项目其他包 | 叶子 |
| db | GORM 连接与 AutoMigrate | 基础 |
| model | 数据模型（Agent / Session / Message / SkillUsage / MCPCall）与 JSON 字段类型 | 基础 |
| transcript | 会话转录 JSONL 的追加与读取（对话内容唯一的持久化） | 叶子 |
| titler | 会话标题生成：把首句派生的标题换成本机小模型（ollama）给的概括。两端 agent 的自动标题都长在各自 CLI 层，ACP 通道取不到，所以由本项目自己算。不 import 本项目其他包 | 叶子 |
| schedule | 定时任务的调度核心：任务与运行记录的单文件存储、cron 表达式解析（robfig/cron 的 parser，5 段 + IANA 时区）、整分钟扫描、同任务不重入、失败退避与自动停用、一次性任务。不知道任务怎么跑（Runner 注入）也不知道属于谁（Scope 不透明）。不 import 本项目其他包 | 叶子 |
| stream | SSE 事件形状（Event）与广播器（Broker）：多订阅者、轮内重放、慢订阅丢弃。会话流（service）用 | 叶子 |
| mcp | 我方 MCP server 的协议外壳：JSON-RPC 信封、工具声明与分发（initialize/ping/tools.list/tools.call），外加非会话调用方的回连凭证 PeerTokens（datasource/report 共用）。业务包提供工具集，协议外壳与之解耦 | 叶子 |
| webshot | 页面 → 整页 PNG：驱动本机 Chrome（headless + 自带的 mini CDP 客户端），discord 报告长图用。找不到 Chrome 由调用方降级 | 叶子 |
| gist | 内容 → GitHub secret gist 外链：发布（gh CLI）、按归属列出、撤销、过期清扫。状态全写在 gist 描述里，不落盘。discord 的报告与 HTML 交付用；gh 不可用由调用方降级 | 叶子 |
| apilog | HTTP API 请求的观测记录：httpapi 的中间件写（方法 / 路径 / 身份 / 对方 IP / 来源 / 头 / 正文 / 耗时），日志页读。凭证头抹掉、正文截 8 KB、留最近 5000 条 | 业务 |
| mcpcall | MCP 工具调用的观测记录与统计：谁调的、传了什么、拿回什么、花多久。工具台读它，数据源工具面写它（经窄接口，两包不互相 import）。留存有上限，长文本落库前截断 | 业务 |
| ask | 别的 AI 的同步问答面（adr-022）：本机 CLI 里的 claude / codex 经 `POST /api/ask` 把问题交给另一方——开会话（打 `origin=ask`）、拨权限档、发一轮、替人裁决权限与提问、阻塞到轮末、从转录取回答。同一会话不排队（409），失败的新会话即刻收掉。只是把 service 的会话/对话操作串成一条同步路径，不碰 db | 业务 |
| service | 普通会话的业务规则：会话/对话/技能/工作区/终端/agent 配置；多租户身份与隔离范围（Scope） | 业务 |
| github | GitHub issue 页（adr-023）：每个身份的关注仓库清单（落库，owner 记 0）、经 gh 拉 issue 并用 GraphQL 补看板列与 Priority、按仓库缓存在内存里后台 3 分钟一刷、内存里过滤 / 排序 / 分页。租户可用；借 service 的哨兵错误与 Scope | 业务 |
| project | 工作区项目（adr-007）：git 仓库发现、克隆（租户禁用凭证助手）、gh 远端仓库清单。磁盘即事实源，不入库；借 service 的哨兵错误与 Scope | 业务 |
| datasource | 外部 MySQL 数据源（adr-008）：连接配置（项目 + 环境两级）、SSH 隧道（拨号在 sshdial，跳板机配置经 Servers 接口取自 remote）、库表探查、多段语句执行，以及挂给会话的 MCP 工具面。连接一次性、可见性按会话 cwd 所属项目过滤。借 service 的哨兵错误与 DefaultCwd | 业务 |
| remote | 远程服务器（adr-019）：连接配置（SSH，AI 的观察目标 + 数据源的拨号跳板）、老数据源 SSH 配置的一次性迁移，以及挂给会话的只读观察工具面（文件 / Docker / 主机信息）。**不做项目隔离**——配一台服务器就等于授权 AI 观察整台机器。借 service 的哨兵错误 | 业务 |
| report | 报告展示（skill html-report 的落地端）：挂给会话的 `report_open` 工具面，把 agent 写好的单文件 HTML 报告在用户工作区打开。**不存任何东西**——报告是磁盘上的文件，「哪些报告属于这条会话」的事实源是转录里的 tool_call；路径护栏限死会话 cwd 内的 .html | 业务 |
| upload | 本机文件上传：落盘、按内容 hash 去重、列举与删除。上传件存在各自身份的家目录下，**隔离由路径本身给**——没有归属过滤这回事。借 service 的 Scope 与哨兵错误 | 业务 |
| discord | Discord 频道工作区（adr-016/017/018）：gateway 长连接、/init 绑定表单、工作树布局（一仓库一份 bare `.repo` + 每分支一棵 `.worktree/<分支>`）与数据库环境锁定、配置存储（`<dataDir>/discord.json`），以及子区对话面（@bot 开子区、独立 acp 会话池、权限/提问的编号问答桥）。**与会话零耦合**：项目内只 import 叶子包 gitrepo 与 acp，业务依赖经 Deps 闭包注入，哨兵错误自带（writeError 里同义映射） | 业务 |
| system | 系统平台面：数据目录迁移、环境体检与依赖安装、版本检查与自更新。哨兵错误借 service 的（错误映射一套） | 业务 |
| httpapi | 路由、handler、中间件、统一响应。不碰 db，服务由 cmd/server 装配后传入 | HTTP |

## 跨包可复用工具

| 函数 | 位置 | 用途 |
| --- | --- | --- |
| `config.SamePath` | config/datadir.go | 两个路径解析绝对路径后是否同一位置 |
| `config.CopyDirFiles` | config/datadir.go | 目录内文件逐个拷贝（数据迁移用） |
| `db.LikePattern` | db/like.go | 关键词 → `LIKE ? ESCAPE '\\'` 的子串模式（逃逸 `\` `%` `_`），列表搜索共用 |
| `service.RebuildMessages` | service/rebuild.go | 线级转录 → UI 消息列表的重建器 |
| `service.DegradedSettings` | service/chat_settings.go | 用探测缓存 + 设置快照拼未连接会话的设置视图 |
| `service.DeriveTitle` | service/chat_turn.go | 首条消息 → 自动会话标题 |
| `service.TruncateError` | service/chat.go | 错误文本截断（落库字段的长度保护） |

包内的私有 helper（如 service 的 `truncateError`、acp 的 `truncate`）不在此登记——
需要跨包复用时先提升为上表的具名工具再使用。
