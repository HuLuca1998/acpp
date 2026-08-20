# ACPP

Agent Client Protocol 的本地管理面板：注册 agent、发起会话、与 agent 连续对话，实时看到回复、思考与工具调用。

- **前端** — Vite + React 19 + TypeScript + shadcn/ui（Base UI + Tailwind v4）+ i18next（中/英）
- **后端** — Go + net/http + GORM + SQLite（`glebarez/sqlite`，纯 Go 无需 CGO）+ 自写的 ACP 客户端

后端扮演 ACP 里的 **client** 角色：拉起 agent 子进程 → `initialize` 握手 → `session/new` → `session/prompt` → 消费 `session/update` 事件流 → 裁决权限。**不实现 agentic loop**，那是 agent（`codex-acp` / `claude-agent-acp`）自己的事。

两条 runtime 的语义差异（权限档、模型清单位置、配置项 id、rawOutput 形状等）全部收敛在 adapter 层，上层与前端只见**统一词汇表**（模型 / 思考深度五档 / 权限三档 / plan / fast）。规范原则是**交集**：两条 ACP 都能做到的才进统一接口，单端独有的功能废弃。设计与差异清单见 [docs/adr-001](docs/adr-001-codex-claude-差异收敛.md)。

## 目录结构

```
acpp/
├── AGENTS.md                   # 通用工程规范（人与 AI 协作者共同遵守，CLAUDE.md 指向它）
├── Makefile                    # 常用命令入口，make help 查看；make check 一键全量验证
├── docs/                       # 决策记录（adr-001 差异收敛；adr-002 工作区多面板；adr-003 messages 表退役；adr-004 macOS 桌面壳；adr-007 多租户隔离与项目管理；adr-008 数据库数据源；adr-009 子代理转录；adr-010 租户会话能力与 owner 对齐；adr-011 ACP 能力面补全；adr-012 编排与角色下线）
├── scripts/                    # 开发辅助脚本（dev.sh 服务管理；check-structure.sh 结构检查；acp-probe.py 协议探针；build-macos-app.sh 桌面版打包）
├── build/                      # 编译产物：build/web（vite）+ build/server/acp-server + build/app（macOS 桌面版），不入库
├── desktop/                    # macOS 桌面壳
│   └── macos/                  # Sources/ Swift/AppKit 壳源码；IconGen/ 图标绘制脚本；Info.plist.in
├── web/                        # 前端
│   ├── AGENTS.md               # 前端规范 + 设计规范
│   ├── src/
│   │   ├── main.tsx            # 入口：Theme + Tooltip + i18n + Router
│   │   ├── App.tsx             # 路由表
│   │   ├── routes/             # 页面，与路由表一一对应：overview / sessions /
│   │   │                       #   session-chat（工作区宿主，草稿态共用）/ skills / skill-detail /
│   │   │                       #   databases / tools（MCP 工具台）/ tenants（连接）/
│   │   │                       #   settings（系统 + claude/codex 工具分区）/ dashboard-layout /
│   │   │                       #   placeholder / not-found
│   │   ├── hooks/              # use-chat（SSE 状态机）/ use-draft-session /
│   │   │                       #   use-async-data / use-mobile /
│   │   │                       #   identity-context（身份与 owner 判定）
│   │   ├── i18n/               # i18next 初始化 + 类型增强 + locales/{zh,en}.ts
│   │   ├── components/
│   │   │   ├── ui/             # shadcn 组件（CLI 托管区，目录级 AGENTS.md）
│   │   │   ├── shell/          # 应用外壳：侧边栏、顶栏、导航、主题/语言切换
│   │   │   ├── chat/           # 消息渲染；composer/ 输入域；cards/ 权限、计划审批、提问卡
│   │   │   ├── workspace/      # 工作区编排（dock/menu/provider）；panels/ 十类面板
│   │   │   ├── projects/       # 克隆仓库对话框（gh 清单 + URL）
│   │   │   ├── db/             # 数据库：连接对话框、库表浏览、SQL 结果表格
│   │   │   ├── tools/          # 工具台：工具清单、参数表单、响应视图、自定义请求、调用记录
│   │   │   ├── overview/       # 概览页四张卡
│   │   │   ├── settings/       # 设置页分区面板（内置工具 claude/codex 的配置面）
│   │   │   └── *.tsx           # 跨域小组件：status-dot / diff-view / dir-picker / agent-icon / list-page-states
│   │   ├── lib/                # 纯函数与客户端；README.md 是工具索引（脚本对账）
│   │   ├── types/              # 领域类型，与 server/internal/model 对齐（acp.ts 转出 db.ts）
│   │   └── index.css           # Tailwind v4 主题变量 + 视觉深度层
│   └── vite.config.ts          # /api 代理到 127.0.0.1:48080；outDir 指向 ../build/web
└── server/
    ├── AGENTS.md               # 后端规范
    ├── cmd/server/main.go      # 装配层：连库、构建全部 service、挂路由、优雅关闭
    └── internal/               # README.md 是包地图与跨包工具索引（脚本对账）
        ├── acp/                # ACP 客户端（目录级 AGENTS.md：铁律 / 文件地图 / runtime 差异表）
        │   ├── protocol.go     #   JSON-RPC 与 ACP 线级类型
        │   ├── conn.go         #   stdio 连接：ndjson 读写、请求关联、反向调用路由
        │   ├── runtime.go      #   runtime 注册表 + 嵌套环境变量清理
        │   ├── event.go        #   归一化事件模型（推给上层的唯一形状）
        │   ├── session.go      #   会话状态体 + 协议原语 + prompt/steering 裸调用
        │   ├── manager.go      #   会话池：Open 并发去重、握手（load 恢复优先）、回收
        │   ├── turn.go         #   轮次执行：Prompt/Interject/Cancel + 设置门面
        │   ├── updates.go      #   反向调用：update 归一化、权限与 elicitation 挂起
        │   ├── fsproxy.go      #   fs 代理（路径限制在会话 cwd）
        │   ├── adapter*.go     #   统一词汇表 + claude/codex/generic 三实现
        │   └── isolation.go    #   技能隔离注入
        ├── config/             # 环境变量配置、数据目录准备与迁移、路径工具
        ├── db/                 # GORM 连接 + AutoMigrate
        ├── model/              # Agent / Session / Message(重建 DTO) / SkillUsage
        ├── transcript/         # 会话转录 JSONL（对话内容唯一的持久化）
        ├── stream/             # SSE 事件形状与广播器（会话流的叶子包）
        ├── project/            # 工作区项目（adr-007）：git 仓库发现、克隆、gh 远端仓库清单
        ├── mcp/                # 我方 MCP server 的协议外壳（JSON-RPC + 工具分发），数据源工具面用
        ├── datasource/         # 外部 MySQL 数据源（adr-008）：连接配置、SSH 隧道、库表探查、多段执行、MCP 工具面
        ├── service/
        │   ├── agent.go / session.go / broker.go / system.go / fs.go / terminal.go
        │   ├── tenant.go / guard.go # 多租户：租户 CRUD 与隔离范围（Scope）
        │   ├── chat.go         #   服务骨架与生命周期（Peek/Open/Close/回收）
        │   ├── chat_stream.go  #   SSE 契约 + ACP 事件映射
        │   ├── chat_turn.go    #   发送、内容块组装、轮次执行
        │   ├── chat_settings.go#   配置页取舍过滤 + 统一设置
        │   ├── chat_messages.go#   转录重建读路径；rebuild.go 是重建器
        │   ├── probe.go        #   agent 能力探测
        │   └── skill*.go       #   技能库、附属文件、脚本试运行、使用统计
        └── httpapi/            # 路由、handler、中间件、统一响应（服务由装配层传入）
```

## 快速开始

先装 ACP runtime（两条都支持，可各注册一个）：

```bash
npm i -g @agentclientprotocol/codex-acp @agentclientprotocol/claude-agent-acp
codex login    # codex 复用本机登录态；claude 复用 Claude Code 登录态
```

```bash
make install
make dev          # 一键启动/重启前后端（后端 :48080，前端 :45173，日志在 /tmp/acpp-dev/）
```

`make dev` 每次都会重新编译后端——改完代码再跑一次就是更新；`make stop` 停止、`make status` 看状态。要盯实时日志时用 `make dev-server` / `make dev-web` 前台跑。端口是固定约定，被占会自动清掉旧进程，见 [AGENTS.md §4.0](AGENTS.md)。

claude 与 codex 两个工具是**内置的**（后端启动时自动预置记录，命令分别为 `claude-agent-acp` / `codex-acp`；改命令、启停模型与 `/` 命令在 **设置 → Claude / Codex** 分区里调，见 [docs/adr-005](docs/adr-005-agent-列表退役为内置工具.md)）。任意页面点 **新建会话** 直接进入对话。新会话与老会话是**同一个页面**，只有两处差异：草稿态的模型选择器按 agent 分组列出全部可用模型（探测自动缓存，选哪个模型就用哪个工具），且状态栏里的工作目录可点击修改（选择器内可就地新建子目录）；发出首条消息才真正创建会话，此后模型只能在当前 agent 内切、工作目录不可再改。

输入框支持：**粘贴/上传图片**、**@ 引用文件**（后端读内容嵌入 prompt；超过 32KB 的大文件改发 resource_link 由 agent 按需读取——芯片以链条图标标注；文件树右键与预览面板也可添加引用，**文件夹引用嵌入两层目录清单**而非全文）、**`/` 斜杠命令补全**（清单来自 agent），以及 **turn 进行中直接插话**（不用等上一轮结束）。

单进程部署（后端托管前端产物）：`make serve`；macOS 桌面版打包：`make app`（见下节）。

## macOS 桌面版

`make app` 一键打包出 `build/app/ACPP.app`：Swift/AppKit 菜单栏壳 + 捆绑 acp-server + 前端产物，图标全部由脚本程序化绘制（仓库不存二进制），ad-hoc 签名本机直接用。选型与行为决策见 [docs/adr-004](docs/adr-004-macos-桌面壳.md)。

行为约定：

- **关闭 ≠ 退出**：关闭按钮 / Cmd+W / Cmd+Q / Dock 退出都只是隐藏窗口，服务常驻菜单栏；**真退出只有菜单栏图标右键 → 「退出 ACPP」**（系统注销/关机也会放行，并回收全部子进程）。
- **菜单栏图标**：左键切换主窗口显隐；右键菜单：打开主窗口 / 在浏览器中打开 / 允许局域网访问 / 复制局域网链接 / 开机启动 / 开机最小化 / 重启服务 / 打开服务日志 / 退出。
- **开机启动**走 `SMAppService`（系统设置 › 通用 › 登录项里能看到并关掉，我们只是同一个开关的另一个入口）；未签名或不在「应用程序」下时系统会拒绝注册，此时弹窗说明原因。
- **开机最小化**：勾上后开机只驻留菜单栏，不弹窗口也不占 Dock（运行时切 `.accessory`，不用 LSUIElement）。只管开机那一下——用户从菜单栏打开窗口后就切回正常 app，Dock 图标与主菜单一并回来（WKWebView 的复制粘贴依赖主菜单的 Edit 项）。
- **端口固定 `48090`**，与开发态 48080 隔离——`make dev` 与桌面版互不误杀，可同时运行；数据共用 `~/.acpp`，桌面版和 dev 看到同样的会话。
- **局域网共享默认关**（工作区终端是任意命令执行面，见 §安全姿态）。菜单栏开启后服务监听 `0.0.0.0`，「复制局域网链接」得到 `http://<局域网IP>:48090/`，发给局域网内其他设备即可在浏览器使用完整 web 端。切换开关会重启后台服务（agent 上下文在 runtime 侧持久化，续聊自动恢复）。
- 服务日志：`~/Library/Logs/ACPP/server.log`。agent 子进程的 PATH 取自登录 shell——GUI app 默认拿不到 Homebrew 路径，壳启动时注入，否则拉不起 `codex-acp` / `claude-agent-acp`。

打包脚本 [scripts/build-macos-app.sh](scripts/build-macos-app.sh)（`--skip-web` 复用已有前端产物提速，`APP_VERSION` 覆盖版本号，版本与发布仓库经 ldflags 注入后端）；壳源码在 [desktop/macos/](desktop/macos/)。

**版本发布与更新**：`make release VERSION=0.2.0`（[scripts/release-macos.sh](scripts/release-macos.sh)）构建 → zip → git tag → GitHub Release（notes 缺省取上个 tag 以来的提交标题）。App 内 **设置 → 关于与更新** 后台每日自动检查 Releases，显示新版本描述，桌面版可一键「更新并重启」（下载 zip → 原地替换 .app → 壳正常退出回收子进程 → 自动拉起新版）；开发态只提示不安装。

## 对话是怎么流起来的

```
浏览器 ──POST /api/sessions/{id}/send──→ 立刻 202，只落库用户消息
   │                                          │
   │                                          └─→ goroutine: session/prompt（阻塞到整轮结束）
   │                                                     │
   └──GET /api/sessions/{id}/events (SSE)←─── broker ←── session/update 通知（逐 chunk）
```

关键点：

- **`session/prompt` 阻塞到整轮结束**，流式的唯一来源是 `session/update` 通知。所以 `send` 不等它，chunk 一到就经 SSE 写出并 flush，中间层不攒。
- **一条用户对话 = 一条 ACP 会话**。ACP 会话自带上下文，第二轮起只发用户这一句，不重复系统提示。
- **`stopReason` 只有 `end_turn` 算正常说完**；`max_tokens` / `max_turn_requests` / `refusal` / `cancelled` 会在界面上标出来，不会被当成完整回答。
- **`tool_call_update` 除 `toolCallId` 外全是可选**，前后端都按 id 合并，空值不覆盖已有字段。
- 每轮结束后由重建器**按时序**还原成多条消息：正文被工具调用打断处就是断点（agent「说一句 → 干点活 → 再说一句」会还原成两条消息，而不是首尾相接的一条），同一段内的连续分片仍然合并。前端收到 `turn_done` 后重新拉取消息列表，用重建结果取代流式拼接的文本，所以偶尔丢一个 chunk 也不会留下残缺消息。除正文/思考/工具调用外，重建器还产出：轮末的 **plan 快照**（kind=plan，历史里折叠成一行进度）、**权限裁决记录**（kind=permission_request，谁请求 + 用户选了什么）、挂在最后一段正文上的**本轮 token 计量**（payload.turnUsage，hover 可见）。

## API

统一响应 `{"data": ...}` 或 `{"error": "..."}`，列表再包一层 `{items, total, page, pageSize}`。

**分页协议**：所有列表端点统一收 `?page=&pageSize=`（页码从 1 起，缺省 20，上限 200，解析在 `httpapi.pageParams`），统一回 `{items, total, page, pageSize}`。事实源是数据库的走 LIMIT/OFFSET，是磁盘的（技能）在内存切片——形状对调用方一致。

**排序协议**：列表端点另收 `?sort=<字段>&order=asc|desc`。字段名是**数据库列名**（技能没有数据库，沿用同样的 snake_case 写法），走白名单校验后才拼进 `ORDER BY`——那是不能用占位符的位置；认不出的字段当没排序，不报错。排序**必须在服务端做**：客户端排序在分页列表上是错的，它只会把当前这一页重排一遍，用户以为看到的是「全部里最大的」，其实是「这 20 条里最大的」。同理，技能的内存排序也在切页之前。

前端四个列表页（会话 / 访客 / 数据库 / 技能）共用 `usePagedData` + `DataTable`：翻页、每页行数、表头三态排序（无序 → 升 → 降 → 无序）都发给后端，列显隐留在客户端（那是一个人此刻想看什么，不是配置）。行为（改排序或每页行数回第一页、删空当前页退回上一页）因此只有一套。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/health` | 健康检查与版本 |
| GET | `/api/auth/me` | 当前身份（owner / 租户 / 被停用 / 匿名）。未认证也返回 200，前端据此渲染邀请页 |
| POST | `/api/auth/redeem` | 用邀请链接里的 token 换 HttpOnly cookie（`{token}`） |
| POST | `/api/auth/logout` | 清凭证 |
| GET/POST | `/api/tenants` | 局域网访客列表（带可直接转发的邀请链接）/ 新建（`{name}`，名字即目录名） |
| PUT/DELETE | `/api/tenants/{id}` | 停用/启用、改 root / 删除（保留其会话与目录） |
| POST | `/api/tenants/{id}/rotate` | 重新生成分享链接（旧链接立刻作废） |
| GET/POST | `/api/projects` | 工作区项目（工作区根下的 git 仓库）/ 新建空项目（`{name}`，最多 `<组织>/<仓库>` 两层） |
| DELETE | `/api/projects/{name...}` | 删项目目录（会话记录保留） |
| POST | `/api/projects/clone` | 后台克隆（`{url, name?}`）；**租户强制禁用 git 凭证助手** |
| GET | `/api/projects/clones` | 克隆任务进度（内存态，只对发起者可见） |
| GET | `/api/projects/repos` | 可克隆仓库清单（gh，只要组织与协作关系，个人账号名下的不出现） |
| GET | `/api/system/env` | 环境体检：brew/node/npm、CLI 与 ACP 适配器是否就位（含版本与路径） |
| POST | `/api/system/env/install` | 一键安装缺失依赖（`{key}`，只认后端白名单：brew formula / npm -g） |
| GET | `/api/system/title-model` | 会话标题模型配置（本机 ollama：`{enabled, baseUrl, model}`） |
| PUT | `/api/system/title-model` | 存标题模型配置，热更生效（启用时必须选模型） |
| GET | `/api/system/title-model/models` | 列某个 ollama 端点上已装的模型（`?baseUrl=`，为空取默认地址） |
| POST | `/api/system/title-model/test` | 用给定配置当场生成一个标题看效果，不落盘 |
| GET | `/api/system/update` | 版本检查（GitHub Releases 缓存，后台每日刷新；`?force=1` 现查）。`pending` 带当前版本与最新版本之间**全部待更新版本**的日志（最多 5 条，更早的计入 `pendingMore`）——跨版本更新时中间几版改了什么也要看得到 |
| POST | `/api/system/update/apply` | 一键更新：下载最新 release 替换 .app 并自动重启（仅桌面版）。有会话正在生成回复时返回 `{applied:false, runningTurns}` 供前端弹确认，body 带 `{force:true}` 才真装 |
| GET | `/api/fs/dirs` | 列目录（`?path=`，空为家目录；`?files=1` 连文件、`?hidden=1` 含隐藏项；条目带大小与修改时间），供选择器导航 |
| GET | `/api/fs/places` | 选择器侧边栏的默认位置（家目录/桌面/文稿/下载/工作区；租户只有自己的 root） |
| POST | `/api/fs/dirs` | 在指定目录下新建单层子目录（`{path, name}`），选择器就地建目录 |
| GET/POST | `/api/agents` | agent 列表 / 新建（新建后自动探测模型与命令清单） |
| GET/PUT/DELETE | `/api/agents/{id}` | agent 详情 / 更新 / 删除 |
| POST | `/api/agents/{id}/probe` | 重探统一设置能力（flavor、模型与命令清单），同步返回 |
| PUT | `/api/agents/{id}/catalog` | 配置页勾选：更新 models/commands 的启用状态（禁用只影响本软件的下拉与补全，agent 侧能力不变） |
| GET/POST | `/api/skills` | 技能列表（遍历 `<dataDir>/skills`，磁盘为事实源）/ 新建（`{name, description, body}`，frontmatter 由后端组装转义） |
| GET/PUT/DELETE | `/api/skills/{name}` | 技能详情（`body` 为 frontmatter 之后的正文）/ 更新（`{description?, body?, enabled?}` 逐项可选，启停即建/删 skillpack 符号链接）/ 删除（连源目录带分发链接） |
| GET/PUT/DELETE | `/api/skills/{name}/files/{path...}` | 附属文件（`references/` / `assets/` 等）读 / 写 / 删；文本可编辑、二进制只列出，路径限制在技能目录内 |
| GET | `/api/skills/{name}/files` | 附属文件清单（带 size / binary / 修改时间） |
| GET | `/api/skills/{name}/scripts` | `scripts/` 下脚本的头部元信息（`desc/usage/arg/opt/env` 注释解析成参数控件描述） |
| POST | `/api/skills/{name}/scripts/run` | 传参试运行脚本（`{path, args, opts, env}`）：以技能目录为 cwd、60s 超时、输出各 256KB 截断，返回退出码与 stdout/stderr |
| GET/POST/DELETE | `/api/uploads` | 本机文件上传：列出传过的 / 上传（multipart `file`，单个 ≤32 MiB）/ 删除（`?hash=&name=`）。落点是各自身份的家目录（owner 是工作区根，访客是自己的 root）下的 `.acpp-uploads/<内容 hash 前 12 位>/<原名>`——**隔离由路径本身给**，不需要再加一层归属过滤；同内容不重复写盘 |
| GET/POST | `/api/sessions` | 会话列表（`?agentId=&page=&pageSize=`，按更新时间倒序分页）/ 新建（`{agentId, cwd?, title?, worktree?}`：带 worktree 时先开隔离工作区再把会话开在里面） |
| GET/DELETE | `/api/sessions/{id}` | 会话详情（**Peek：绝不拉进程**，查看记录零成本；未连接时 `settings`/`commands` 由 agent 探测缓存降级拼出，`Current*` 留空） / 删除（回收子进程，并尽力调 `session/delete` 清掉 agent 侧线程历史） |
| GET | `/api/sessions/{id}/messages` | 历史消息（`?limit=` 取尾部 N 条，`?before=<id>` 加载更早） |
| GET | `/api/sessions/{id}/fs/entries` | 工作区文件树（`?path=&depth=`，depth≤2；全量展示，仅过滤固定黑名单 .git/node_modules/.DS_Store，路径限制在会话 cwd 内） |
| GET | `/api/sessions/{id}/fs/file` | 工作区文件预览（`?path=`；1MB 截断、二进制检测，同上 path guard） |
| GET | `/api/sessions/{id}/git/overview` | git 汇总：分支、upstream、ahead/behind、变更文件（含 numstat）、未推送 commit；非仓库返回 `isRepo:false` |
| GET | `/api/sessions/{id}/git/diff` | 单文件工作区 diff（`?path=`，返回 HEAD 版与工作区版全文，行级对齐在前端） |
| GET | `/api/sessions/{id}/git/commits/{sha}` | 提交详情（文件清单）；带 `?path=` 时返回该文件在这条提交前后的全文 |
| GET | `/api/sessions/{id}/git/branches` | 分支面：当前分支、本地/远端分支、标签、worktree 清单（被占用的分支带占用者） |
| POST | `/api/sessions/{id}/git/checkout` | 切换分支（`{branch, create?}`）；脏工作区拒绝切换 |
| GET | `/api/sessions/{id}/git/history` | 提交链路（`?ref=&limit=&offset=`，`hasMore` 指示还有没有） |
| GET | `/api/sessions/{id}/git/compare` | 两 ref 对比（`?base=&head=`）：head 独有的提交 + 三点 diff 的文件变更 |
| POST/DELETE | `/api/sessions/{id}/git/worktrees` | 开/拆隔离工作区（`<仓库>/worktrees/<名字>`），拆时保留分支 |
| * | `/api/workspace/fs/*` `/git/*` | **草稿态**工作区数据面：会话还没建，目录由 `?cwd=` 给（路径闸照旧），端点形状与上面会话侧一一对应——选完工作目录文件树与 git 面板即可用（adr-002） |
| GET/POST | `/api/sessions/{id}/terminals` | 工作区终端列表 / 新建（会话 cwd 起交互 shell，每会话上限见 `ACP_MAX_TERMINALS`） |
| DELETE | `/api/sessions/{id}/terminals/{tid}` | 关闭终端（杀 pty） |
| WS | `/api/sessions/{id}/terminals/{tid}/ws` | 终端双向流：二进制帧 = 原始字节，文本帧 = `{"type":"resize","cols","rows"}`；断线后 pty 保活 30s 供重连（带回放缓冲） |
| POST | `/api/sessions/{id}/open` | 拉起 agent 并握手，幂等。**前端不再主动调**——发消息时 `send` 顺路连接（懒连接），连接完成经 SSE 推 `settings`/`commands` |
| POST | `/api/sessions/{id}/send` | 发一轮（`{content, images?, files?}`：图片 base64、@ 引用文件路径由后端读内容嵌入），立即返回；**turn 进行中再发会插进当前轮**（claude 排队为独立一轮，codex steering 注入当前轮） |
| GET | `/api/sessions/{id}/events` | **SSE 事件流** |
| GET | `/api/sessions/{id}/transcript` | 线级转录 JSONL 原样下发（`http.ServeFile`，支持 Range 字节续读——工作区 logs 面板靠它轮询增量实时跟随） |
| GET | `/api/system` | 系统配置：当前/默认数据目录，`pendingDir` 表示已迁移待重启 |
| PUT | `/api/system/data-dir` | 迁移数据目录（`{dataDir}` 绝对路径）：`VACUUM INTO` 在线快照 + 转录拷贝 + 写 `~/.acpp/config.json`，旧数据保留，重启后生效 |
| PUT | `/api/system/workspace-dir` | 改工作区根（`{workspaceDir}`）：agent 干活的地方与访客 root 的父目录，立刻生效 |
| POST | `/api/sessions/{id}/cancel` | 中止当前轮 |
| PUT | `/api/sessions/{id}/settings` | 统一设置（`{model?, effort?, level?, plan?, fast?}` 逐项可选），响应带最新 `Settings`；未连接的老会话会先幂等拉起进程再应用 |
| POST | `/api/sessions/{id}/permission` | 回传权限裁决（`{permissionId, optionId}`，optionId 空=取消） |
| GET/POST | `/api/datasources` | 数据库连接列表 / 新建（`{project, env, host, port, user, password?, database?, sshEnabled?…}`；密码永不下发，响应只给 `hasPassword` 标志位） |
| GET/PUT/DELETE | `/api/datasources/{id}` | 连接详情 / 更新（密码留空=不改） / 删除 |
| POST | `/api/datasources/{id}/test` | 测试连接（连不上返回 200 带 `{ok:false, error}`，那是配置问题不是服务故障） |
| POST | `/api/datasources/probe-databases` | 配置页选库：列出这组连接参数可见的库（参数走请求体，编辑时带 `id` 沿用已存密码） |
| POST | `/api/datasources/probe-ssh` | 配置页 SSH 页签单独测隧道，不碰 MySQL（probe 模式与失败形状同上两条） |
| GET | `/api/datasources/{id}/databases` `/tables` `/schema` | 库清单 / 表清单（`?database=`） / 表结构（`?database=&table=`，含列、索引与建表语句） |
| POST | `/api/datasources/{id}/query` | 执行 SQL（`{database?, sql, maxRows?}`，可含多条语句：按序执行、遇错即停，每条独立返回耗时与影响行数；行数硬顶 1000） |
| GET | `/api/sessions/{id}/datasources` | **会话可见的**数据源：只有当前工作目录所属项目的那几条（斜杠命令数据源） |
| GET | `/api/sessions/{id}/datasources/{dsid}/databases` `/tables` | 同上但按会话过滤，项目之外的 id 按「不存在」处理 |
| GET | `/api/workspace/datasources` 及 `.../{dsid}/databases` `/tables` | **草稿态**数据源：项目由 `?cwd=` 的目录决定——选完工作目录 @ 引用与 `/db` 即可用，不必等首条消息建会话；过滤规则与会话侧相同 |
| POST | `/api/mcp/db/{token}` | 会话的数据库 MCP 端点（agent 回连，token 为每会话专属凭证，不出现在 API 响应里） |
| GET | `/api/tools/servers` | 工具台：当前上下文（`?cwd=`）下的 MCP 工具面——工具名、给模型看的描述原文、参数 JSON Schema、只读/破坏性注解，外加这个面会不会真的挂给 agent（数据源为空就不挂） |
| POST | `/api/tools/inspect` | 工具台试运行与自定义请求（`{cwd, request}`，request 是**原样的** JSON-RPC 消息）：走与 agent 完全相同的协议路径，回完整响应与耗时；通知类消息回 `accepted:true`（协议上就没有响应） |
| GET | `/api/tools/calls` | 调用记录（`?server=&tool=&source=&errorsOnly=1` + 分页，时间倒序） |
| GET | `/api/tools/calls/stats` | 按工具聚合的调用统计（次数、失败数、平均耗时、最近使用） |
| DELETE | `/api/tools/calls` | 清空调用记录 |

SSE 事件的 `kind`：`user_message`、`message_chunk`、`thought_chunk`、`tool_call`、`permission`、`permission_done`、`plan`、`settings`、`usage`、`commands`、`elicitation`、`elicitation_done`、`turn_end`、`session_title`、`turn_done`、`error`。每条带单调递增的 `seq`，断线重连时用它去重。`settings` 在 agent 自行切档/改配置时带全量统一视图（含 `prompt` 内容能力：`{image, audio, embeddedContext}`，来自 initialize 的 `promptCapabilities`，claude/codex 由 adapter 按实测兜底，generic 按声明——前端据此门控图片按钮，后端在发送前把越界内容块收敛：resource 降级为 text、图片直接报错）；`usage` 是上下文用量 `{used, size}`（claude 会间歇附带累计费用 `cost:{amount,currency}`，状态栏顺带显示，codex 无此字段则不出现）；`turn_end` 附带本轮 token 计量（两端交集字段）；`permission` 表示 agent 阻塞等用户裁决（带选项列表），裁决走上表的 permission 端点。`session_title` 在首轮结束后标题被模型重写时发一次（带新标题）。`tool_call` 另带一组子代理字段：`isSubagent`（这次调用派出了子代理）、`subagentOf`（这条是某个子代理干的，值为它所挂的启动调用 id）、codex 专用的 `subagentThreadId` / `subagentPath`；还带 `locations`（ACP 的 follow-along 位置 `[{path, line?}]`），前端用它做「正在触碰」指示（消息流小字 + 文件树呼吸点 + 查看器跟随模式 + 子代理面板当前文件）。**这组指示只在 claude 会话里出现**——2026-08 实测 claude 的 read/edit 工具带 locations、codex 一条都不发，没有位置信息时界面静默降级（不显示，不报错）。

## 数据模型

- **Agent** — 可通过 stdio 启动的 agent 配置（`command` / `args` / `env` / `cwd`），`args` 与 `env` 以 JSON 文本存入 SQLite。产品形态上固定为内置的 claude / codex 两条记录（启动时缺失自动预置、按 name 判存不覆盖用户配置，见 adr-005），API 仍是通用的 `/api/agents`。`flavor` / `models` / `commands` / `skeleton` 是注册/更新后自动探测的缓存（拉临时会话读能力）：模型与命令供草稿态展示与 `/` 补全（条目带 `disabled` 标记，重探不清空取舍）；`skeleton` 是模型之外的设置骨架（efforts/levels/plan/fast 支持位），与模型清单一起构成未连接会话的完整降级设置视图。模型条目支持 `alias`（配置页起显示别名，所有模型下拉优先显示）；`fastPolicy` 是快速模式取舍（首探按 flavor 落默认：claude 因额外计费默认 off，其余 on；off 时快速开关不出现在任何界面）。
- **Session** — 对应一次 `session/new`，`acpSessionId` 是 agent 返回的 uuid v7，`stopReason` 记录上一轮的结束原因。`lastSettings` 是最后一次生效的统一设置当前值快照（用户改设置、或 agent 自己切档时写回；查看会话这类只读路径**不写**，它读到的可能正是一份还没拨回去的默认值），两个用途：未连接会话的工具栏靠它显示与断开前一致的当前值，**子进程重开后也按它把模型/思考深度/权限档拨回去**——这些是会话级运行时状态，跟着子进程一起死，不回放的话空闲回收一次，用户没做任何操作设置就变了；`lastUsage` 同理存最近一次上报的用量（`{used, size, cost?}`，轮末写一次）——上下文水位只经 `usage_update` 通知流过，没有这份快照的话会话一停、页面一刷新，占用比例就没了。`state` 语义：`active` 只表示**有一轮正在跑**；空闲子进程超时会被回收（state 归 `idle`），服务重启时遗留的 `active` 也会归一——续聊时凭 `acpSessionId` 用 `session/load` 恢复上下文，进程挂不挂着不影响会话可用性。
- **Message** — 会话内一条记录，`kind` 覆盖 `session/update` 的各类内容块，结构化内容放 `payload`。**不落库**（adr-003）：它是转录重建器的输出 DTO 与消息接口的响应契约，事实源是转录 JSONL。
- **Tenant** — 一位局域网访客的身份与隔离单元（adr-007）：`name`（同时是 root 目录名，建后不可改）、`token`（邀请链接与 cookie 的凭证，只对 owner 可见）、`root`（最上层工作目录）、`disabled`。owner 刻意不入表——他由 loopback 判定，没有记录也就没有「把自己停用」这种事故。`Session.tenantId` 是会话归属（`0` = owner），隔离靠查询条件执行。
- **Project / Clone** — 都不入库：项目就是工作区根下的 git 仓库目录（扫盘得来，名字是相对根的路径），克隆任务只存在于内存（进程重启时 git 子进程也一起没了，留个「进行中」的假记录只会骗人）。
- **MCPCall** — 一次 MCP 工具调用的观测记录：server、工具名、来源（`agent` = 子进程回连、`manual` = 工具台人工试运行）、会话 id、参数与返回文本、是否报错、耗时。**只记发生过的调用**，工具声明是代码不入库。参数 4KB / 返回 8KB 截断后落库，全表留最近 2000 条（超出按自增 id 裁最老的）——它是运行时观测不是账本。
- **DataSource** — 一个外部 MySQL 数据源（adr-008）。身份是**项目 + 环境**两级（`pp-game` 的 `local`/`dev`/`pre`），组合唯一，`<项目>/<环境>` 即对外标识（AI 调工具时填的 `source`）。只存配置不存连接：每次调用都是「拨号 → 执行 → 关闭」的一次性连接（含 SSH 隧道），因此没有任何运行态字段。密码类字段永不出 API，响应只带 `hasPassword` 这类布尔位。

## 工作区面板

会话页是一套 dockview 工作台，面板可自由开关与拖放（⋯ 菜单勾选，或用布局预设一次摆好）：

| 面板 | 管什么 |
| --- | --- |
| 对话 | 不可关闭，永远在 |
| 文件树 | 工作目录的目录树，右键加 @ 引用 |
| 查看器 | 文件内容 / 改动两种形态——「现在什么样」与「改了什么」是同一个阅读动作的两面 |
| 分支 | 本地/远端/标签；点选驱动其他 git 面板，⌘ 点第二条进对比模式 |
| 提交链路 | 提交历史（分页），顶部第一条是「工作区改动」，未推送的带标记 |
| 变更 | 文件清单，**跟随选择态**：没选看工作区，选提交看那条提交，选两个 ref 看对比。按目录树展示，单子目录链压缩 |
| 详情 | 提交说明 / 对比摘要 |
| 日志 | 线级转录实时跟随 |
| 子代理 | agent 派出去的活：按进行中/已完成/失败分组，展开看这次派了什么、拿回了什么 |
| 终端 | 可多实例的真实 pty |

四个 git 面板不互相说话，全部读命令总线里的同一份选择态——因此可以只开其中一个，也可以任意摆放。布局预设 **Git 工作台** 把它们按「左分支 ｜ 中链路 ｜ 右上变更 / 右下详情」一次摆好。

**AI 联动**：右键提交「让 AI 审查」、右键分支「让 AI 对比」、右键文件「让 AI 分析改动」，写好的 prompt **只填进输入框，不自动发送**——发消息是用户的动作。

## 数据库

按**项目 + 环境**管理 MySQL 连接（adr-008）：`pp-game` 的 `local` / `dev` / `pre` 是三条独立数据源，`<项目>/<环境>` 就是它对外的标识。侧边栏「数据库」页配置（连接对话框照 Navicat 分常规 / SSH / 高级三个页签），页面里可以直接浏览库表、看表结构、跑多段 SQL。

**项目是可见性边界**——这是整个功能的安全底座：

```
会话 cwd ──推项目名──→ 只取该项目的数据源 ──→ MCP 工具面 / 斜杠命令
```

一条开在 `pp-game` 目录下的会话，看得到、连得上的只有 `pp-game` 的几个环境；别的项目的连接对它而言**不存在**（会话侧按 id 直取也返回 404）。过滤的执行点只有一个（`datasource.Service.ForCwd`），界面与 AI 共用；推不出项目就一个都看不见，而不是看见全部。worktree 归属主仓库，工作区之外的目录用最近的 git 仓库名。

**每条连接管两件事：能看哪些库、能不能写。**

- **可访问的库**：一个账号常常连得到整台实例上的所有库。留空时范围就是「默认库」那一个——AI 与界面都只看得到它；要跨库就把库名逗号分隔列出来，或填 `*` 放开。
- **只读开关**（新连接默认开）：开着时写语句一律拒绝，**AI 那边连执行工具都不会挂上**。

两者都是**闸门不是边界**：明写 `别的库.表` 的 SQL 会被挡、`UPDATE` 开头的语句会被挡，但存储过程与动态 SQL 绕得过去。真正的边界始终是连接账号的授权范围——要硬保证，就给这条连接配一个只授权对应库、只有 SELECT 的账号。

**AI 怎么用**：会话所在项目有可用数据源时才挂载 `acpp-db` 这个 MCP server（没有就完全不挂，免得工具清单里多几个用不了的条目）。工具分读写两路——`db_sources`（列数据源）、`db_databases`、`db_tables`、`db_schema`（列/索引/建表语句）、`db_query`（只读查询，写语句会被拒并引导去执行工具）；`db_execute`（改数据与结构）**只在存在非只读数据源时才出现在清单里**。claude 侧预批这些工具不弹权限卡。**挂的只有工具，没有提示词**——数据源清单与用法约定（先看表结构再写 SQL、改数据前确认环境）不进会话开场，等用户真的 `@` 引用了数据库，才随引用内容一起下发。开场就铺一段数据库说明，等于每条会话都替用户按下「我要动数据库」。

**行数护栏在我们这侧**：最多 1000 行（默认 500）。不给用户的 SQL 自动加 `LIMIT`，也不在库上设任何会话变量——你的库我们只读它、不改它的行为。实现是流式游标逐行读，读满上限就取消这次查询让驱动断开，而不是把剩下几百万行读完再丢掉。**诚实的边界**：断开后正在回传结果的查询会因写失败很快中止，但还在扫描/排序、尚未吐数据的查询 MySQL 不会察觉客户端已走，会跑完那一段——要立刻杀掉得发 `KILL QUERY`，那是在库上动手，没做。

**SSH 隧道**：开启后主机/端口填的是**跳板机视角**的地址（线上库多半是 `127.0.0.1:3306`）。验证方式三选一（密码 / 公钥 / 密码和公钥），公钥留空路径则走 ssh-agent。跳板机指纹按 `~/.ssh/known_hosts` 校验，策略等价 OpenSSH 的 `accept-new`：没连过的主机首次连接自动补录指纹，指纹与记录不符则拒绝且**没有跳过开关**——那是唯一真正的中间人信号，隧道后面挂着生产库，人工核实后删掉旧记录再连。

**两个查看入口**：

- 对话里 AI 的 `db_query` 有专用渲染——数据源标识、SQL、耗时、字段表头与可滚动数据，与配置页的 SQL 控制台是同一个组件（MCP 只回文本，前端按两端约定的制表符格式解析回结构化；解析不出来退回原始文本，不编造表格）。
- 输入框里的 `/db` 是**本地斜杠命令**：前端拦截，结果浮在输入框上方，不进对话、不消耗 token、不用等 agent。`/db` 列本项目数据源，`/db dev` 列库，`/db dev mydb` 列表。

## 工具台

侧边栏「工具」页把**我方 MCP server 暴露给 agent 的工具**摊开给人看与试。页面的立场是复现 AI 那一侧：工具集、描述、参数、往返，全部走与 agent 完全相同的那条协议路径（`datasource.InspectMCP` 与会话侧的 `HandleMCP` 共用 `toolsForCwd`），页面上看到的就是模型此刻看到的那一份。

- **先选项目**——工具集本身随项目的数据源变（`db_execute` 只在存在可写连接时才挂，没有可用数据源时整个面根本不会挂给 agent，页面会明说这件事）。
- **工具清单**标两件事：读/写（MCP 标准注解 `readOnlyHint` / `destructiveHint`）与**被调用过几次**。一个从没被 AI 调过的工具，问题多半出在描述而不是实现上。
- **详情**给的是**给模型看的描述原文**、按 `inputSchema` 生成的参数表单、Schema 原文，以及 agent 侧的工具全名（`mcp__acpp-db__db_query`，claude 的预批清单用的就是它）。
- **试运行**填参数即可跑；**自定义请求**直接写 JSON-RPC 消息体（预填当前参数的 `tools/call`，另有 `initialize` / `ping` / `tools/list` 模板）——两者是同一个端点，试运行只是替你把请求体拼好了。agent 说「看不到这些工具」时，自己发一次 `tools/list` 看端点回了什么，是最快的一刀。
- **结果**分两页：**结果**是模型真正读到的那段文本（数据库查询还原成表格，与对话里同一个组件），**原始 JSON** 是完整响应。协议错误（请求没被受理）与工具错误（跑了但失败）分开标——混成一句「出错了」等于把最有用的那半句删掉。
- **会改数据的工具按下运行前先弹确认**，框里原样列出将要发出的参数。这里连的是真实数据库，跑下去的效果和 AI 自己调用一模一样。
- **调用记录**记下每一次调用（AI 的与人工试运行的都记，来源分开标）：参数、返回、耗时、成败，可按工具/来源/只看失败筛。上方是按工具聚合的统计。记录是运行时观测不是账本——参数 4KB、返回 8KB 截断落库，全表留最近 2000 条。

## 安全姿态

三层，缺一不可，且各自边界诚实：

1. **cwd 隔离** — `session/new` 的 `cwd` 必须是绝对路径，不存在会先创建。
2. **fs 代理 path guard** — 声明了 `fs` capability，agent 的 `fs/read_text_file` / `fs/write_text_file` 会走到我们进程里，路径解析成 canonical 形式后必须落在 cwd 内。**注意这条不是可靠拦截点**：codex 用自带 shell 完全不走 fs 代理，claude 0.63 实测权限批准后也由 SDK 自行落盘。它只是纵深防御的一层，真正的隔离靠第 3 条之外的 runtime 沙箱档 + OS 兜底。
3. **权限裁决** — `session/request_permission` 挂起交给用户在界面上点选（批准/拒绝），拒绝真实生效（实测文件不会被创建）。runtime 只在当前权限档认为需要时才会问，所以它仍是策略层不是安全边界——真正的隔离要靠 runtime 自身的沙箱档位 + OS 级兜底。

### 多租户（adr-007）

局域网分享打开后，访问者分两种身份：**owner** 是本机访问（loopback 判定，全权），**租户**凭 owner 发的邀请链接换到一个 HttpOnly cookie。选 cookie 而不是 Authorization header，是因为 SSE（`EventSource`）与工作区终端（WebSocket）都带不了自定义 header——三条通道要统一鉴权，只有 cookie 能做到。

隔离只有一个执行点（`service.Scope`）：数据面把租户条件写进查询本身（漏写等于查不到，不会变成越权），路径面把一切目录操作 canonical 化后钉在租户 root（`<工作区根>/<租户名>`）内。别人的会话按「不存在」处理而不是 403——403 会泄露会话是否存在，凭 id 递增就能数出别人有多少条。owner 专属面（系统设置、数据库连接管理、技能/工具的写）由集中的前缀表判定，新增路由自动继承策略。

**会话内的能力面租户与 owner 一致**（adr-010）：数据源引用/`/db`/MCP 数据库工具、终端、全部工作区面板对租户开放，只按工作目录所属项目过滤，不按身份分家；能在库里干什么交给数据库账号权限管。凭证永不经会话侧下发。

**分享链接什么时候真的能用**：服务默认只监听 `127.0.0.1`，那时任何链接发出去都打不开（访客管理页会直说，并把链接指向本机，方便 owner 自己验一眼访客视角）。要让局域网里的人能用：桌面版在菜单栏图标右键开「允许局域网访问」，命令行用 `make serve-lan`（后端托管前端产物 + 监听 `0.0.0.0:48080`）。开发态的 `make dev` 不适合分享——后端不托管前端，vite 只监听本机。

**owner 判定与反向代理**：本机访问（loopback）且**没带租户凭证**才算 owner；带了凭证一律按租户算。后半句是必要的——任何反向代理都会把来源改写成回环，只看地址的话代理后面的每个访客都会被提权成 owner。即便如此，**不要把 acpp 直接挂在反向代理后面**：代理过来的无凭证请求仍会被当作 owner。

**诚实的边界**：隔离对**界面**是硬的，对**执行**是软的。租户能开工作区终端（`cd /` 就出了 root），agent 自带的 shell 同样不受 root 约束。也就是说这套东西防的是「看见别人的东西」与「误操作走出自己的目录」，**不防有意越权的人**——真正的执行隔离需要 OS 级沙箱（每租户一个系统用户 / 容器），不在范围内。局域网共享的前提仍是可信网络。

另外，租户克隆仓库时强制禁用 git 凭证助手（`-c credential.helper=`）：不禁的话访客能借 owner 钥匙串里的凭证，把他有权访问的任何私有仓库拖下来。

**工作区终端是本机任意命令执行面**：`/terminals` 端点在会话 cwd 起真实交互 shell（用户显式操作才创建），与 agent 已有的命令执行权限面同级。服务只监听 127.0.0.1；若把 `ACP_ADDR` 改成对外地址，这个面会随之暴露，必须配合网络层访问控制。macOS 桌面版的「允许局域网访问」开关就是这个暴露面，因此默认关闭，只应在可信局域网内开启。

另外启动 agent 前会摘掉嵌套会话标记（`CLAUDECODE`、`CODEX_SANDBOX` 等）。不摘的话，从 Claude Code 终端启动本服务时 agent 会误判自己跑在另一个 agent 内部而拒绝服务——这个坑只在那种场景复现，本机开发时碰不到。

## 配置

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `ACP_ADDR` | `127.0.0.1:48080` | 监听地址（macOS 桌面版由壳固定传 `48090`，局域网开关切换 host） |
| `ACP_DATA_DIR` | `~/.acpp` | 数据根目录（db 与转录都派生于它）。优先级：本变量 > `~/.acpp/config.json` 里设置面板选定的目录 > 默认。首次启动自动创建；旧版 `server/data` 的存量数据自动迁入（拷贝，原数据保留） |
| `ACP_DSN` | `<dataDir>/acp.db` | SQLite 文件路径（显式设置时覆盖派生值） |
| 工作区根 | `~/acpp` | 不是环境变量：在 **设置 → 系统** 里选，存 `~/.acpp/config.json`。agent 干活的地方与访客 root 的父目录，与数据目录刻意分开 |
| 会话标题模型 | 关闭 | 不是环境变量：在 **设置 → 系统** 里配，存 `~/.acpp/config.json`。开启后首轮结束时用本机 ollama 把标题从「首句前 15 字」换成模型概括；关闭或调用失败都退回首句派生 |
| `ACP_CORS_ORIGINS` | `http://localhost:45173` | 允许的跨域来源，逗号分隔 |
| `ACP_WEB_DIR` | 空 | 前端产物目录，设置后由后端托管静态文件 |
| `ACP_MAX_SESSIONS` | `8` | 同时活着的 agent 子进程上限 |
| `ACP_IDLE_TIMEOUT` | `10m` | 空闲会话子进程的回收时限（`0` 关闭）。上下文留在 agent 侧，续聊时 `session/load` 无感恢复，模型/思考深度/权限档按 `lastSettings` 回放 |
| `ACP_TURN_TIMEOUT` | `0`（不限时） | 单轮硬上限。长程任务跑几个小时是正常使用方式；turn 进行中（含等待权限/提问裁决）不会被空闲回收 |
| `ACP_MAX_TERMINALS` | `5` | 每会话的工作区终端（pty）实例上限 |
| `ACP_UPDATE_REPO` | 构建注入（`HuLuca1998/acpp`） | 版本发布的 GitHub 公开仓库（owner/repo），更新检查读它的 Releases |
| `ACP_DEBUG` | 空 | 非空则打开 SQL 与 debug 日志 |

前端可用 `VITE_API_BASE` 覆盖 API 前缀，默认 `/api`。

## 技能库

系统自管一套技能，注入每条会话、替换机器级技能，项目级技能照常加载。磁盘即事实源，**不进数据库**——技能列表就是遍历目录、读 SKILL.md 的 frontmatter。数据目录下两处：

```
<dataDir>/
├── skills/<name>/            # 源目录：全部技能（含停用），SKILL.md + 可选 references/ scripts/ assets/
└── skillpack/                # 分发目录：只放注入内容，首次操作自动搭好骨架
    ├── .claude-plugin/plugin.json   # {"name":"acpp"}，两端把技能显示为 acpp:<name>
    ├── skills/<name> -> ../../skills/<name>   # 启用 = 存在这条符号链接
    └── .agents/skills -> ./skills            # codex extraRoots 的固定发现入口
```

- **启停 = 建/删 skillpack 里的符号链接**，源目录路径永远稳定；启用状态从文件系统读。
- **SKILL.md 走结构化编辑**：前端只提交 `name` / `description` / `body`，frontmatter 由后端组装并对 description 做 YAML 转义——手写一个冒号就能弄坏的东西不交给手写。name 与 description 之外的 frontmatter 行（第三方技能的 `license` 等）编辑时原样保留。
- **附属文件**（`references/` / `scripts/` / `assets/`）在详情页就地读写，路径限制在技能目录内，二进制只列出。
- **脚本头部规范**：`scripts/` 下脚本用注释键值声明元信息（`desc` / `usage` / `arg` / `opt` / `env`），页面据此渲染参数控件并支持传参试运行（以技能目录为 cwd、60s 超时、非零退出码是结果不是错误）。规范细则见 [.claude/skills/skills-manage](.claude/skills/skills-manage/SKILL.md)。
- 一切变更**只对新会话生效**——agent 在 `session/new` 时读取一次，进行中的会话不重载。

**会话注入(已落地)**:每条会话在 `session/new` 与 `session/load` 都注入技能隔离,差异全部在 adapter 的 `Isolation` 里:

| | claude | codex |
| --- | --- | --- |
| 加载技能包 | `_meta.claudeCode.options.plugins`(本地插件) | `<codex-home>/skills` 软链到 `skillpack/skills` |
| 屏蔽机器级 | `settingSources:["project"]`(不开 user 档)+ `strictMcpConfig` | 进程 env `CODEX_HOME` 重定向到 `<dataDir>/codex-home`——机器级 `~/.codex/skills` 彻底不在视野 |
| 保留项目级 | project 档保住 cwd 的 `.claude/skills` | cwd 进 `additionalDirectories` |
| 附加 | env `CLAUDE_CODE_DISABLE_AUTO_MEMORY=1` | 认证软链、配置复制自系统 `~/.codex` |

不写 `~/.codex`、`~/.claude` 一个字节。generic runtime 无可靠注入口,不隔离。

**基础提示词**（`acp.ClaudeInstructions()` / `acp.CodexInstructions()`，与隔离同批注入）：每条会话追加一段通用约定，目前只讲一件事——两步以上的请求先建待办清单再动手，逐步更新状态，且**必须用工具建**（界面的进度卡只认工具事件，正文里手写 markdown 复选框等于没建）。正文两端共用，工具那段按方言分开：claude 的待办工具是**延迟加载**的（会话开场 `TodoWrite` 不存在，`Task*` 六件套只有名字没有 schema），提示它先 `ToolSearch` 检索 `select:TaskCreate,TaskUpdate,TaskList`；codex 的 `update_plan` 原生可用，但计划每轮独立，提示它跨轮重列。

注入口两端不同：claude 走 `_meta.systemPrompt.append`（**必须传对象 `{append}`**，传字符串会整体替换 claude_code 的 preset），codex 没有协议注入口（`session/new` 的 `_meta` 只认 additionalRoots），写 `<codex-home>/AGENTS.md`——内容随版本比对覆盖。这里只放与项目无关的通用约定，按项目才成立的内容（数据库那段）不进来。

**为什么不直接关掉延迟加载**（2026-08-20 实测，claude-agent-acp 0.63.0 / agent-sdk 0.3.220）：SDK 没有这个开关。`tools` 显式数组会整体替换内置工具集（漏一个就废掉一项能力，且随版本漂移），`allowedTools` 里列出 `Task*` 不会加载 schema（实测无效），env `ENABLE_TOOL_SEARCH` 是内部 gate、设了不生效。所以只能在提示词里教它自己去取。

codex 的 `CODEX_HOME` 隔离把家目录整体重定向到 `<dataDir>/codex-home`(codex 运行数据写这里,几 MB 量级),机器级技能连 `/skills` 都不再列出——比会话级禁用(`CODEX_CONFIG` 的 `enabled=false` 只挡使用不挡显示)彻底。家目录里 `auth.json` 软链系统的(静态 key、跟随登录态、不复制密钥),`config.toml` 复制系统副本(避免 codex 写回污染系统 config),`skills` 软链技能包。副作用:切换到本方案后,旧 codex 会话的 thread 存在系统 `~/.codex`、新 home 找不到,首次恢复会回退 `session/new`(丢一次上下文),之后正常。认证不隔离:claude 用系统钥匙串登录态、codex 用系统 `~/.codex` 的 auth/config。

## 多语言

中文为默认与兜底语言，右上角切换，选择存在 localStorage（`acp-language`）。文案在 `src/i18n/locales/{zh,en}.ts`，`i18next.d.ts` 做了类型增强——写错 key 在编译期就会报错，不会等到运行时才发现少了一句翻译。

## 添加 shadcn 组件

```bash
cd web && npx shadcn@latest add <component>
```

组件基于 **Base UI**（不是 Radix），自定义触发元素用 `render={<Link to="..." />}`，不是 `asChild`。

## 尚未实现

- 侧边栏的 Logs 与 agent 的新建页仍是占位页（详情页已是配置页）。
- **Discord 接入**：调研完成、未动工——bot 申请、软件内管理、频道 ↔ 工作目录映射、实现方案与测试策略见 [docs/discord-接入设计调研.md](docs/discord-接入设计调研.md)。
- **技能助理**：复用对话面板、把工作目录固定到技能源目录 `<dataDir>/skills/<name>/`,让 agent 帮忙起草/优化 SKILL.md。技能管理与会话注入均已落地,助理待做。
- **工作区面板**（[adr-002](docs/adr-002-会话工作区多面板.md)）M1–M4 已落地：dockview 骨架、九类面板、布局预设、多实例 PTY 终端与联动。剩 diff 虚拟滚动与压力验收。
- **默认档**：会话开在 runtime 默认档上（codex 默认 auto-edit 级、claude 默认 safe 级——两端不同），未强制归一；用户可在会话内随时切统一权限档。
