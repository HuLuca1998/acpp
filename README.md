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
├── docs/                       # 决策记录（adr-001 差异收敛；adr-002 工作区多面板；adr-003 messages 表退役；adr-004 macOS 桌面壳）
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
│   │   │                       #   settings（系统 + claude/codex 工具分区）/ dashboard-layout /
│   │   │                       #   placeholder / not-found
│   │   ├── hooks/              # use-chat（SSE 状态机）/ use-draft-session / use-async-data / use-mobile
│   │   ├── i18n/               # i18next 初始化 + 类型增强 + locales/{zh,en}.ts
│   │   ├── components/
│   │   │   ├── ui/             # shadcn 组件（CLI 托管区，目录级 AGENTS.md）
│   │   │   ├── shell/          # 应用外壳：侧边栏、顶栏、导航、主题/语言切换
│   │   │   ├── chat/           # 消息渲染；composer/ 输入域；cards/ 权限、计划审批、提问卡
│   │   │   ├── workspace/      # 工作区编排（dock/menu/provider）；panels/ 七类面板
│   │   │   ├── overview/       # 概览页四张卡
│   │   │   ├── settings/       # 设置页分区面板（内置工具 claude/codex 的配置面）
│   │   │   └── *.tsx           # 跨域小组件：status-dot / diff-view / dir-picker / agent-icon / list-page-states
│   │   ├── lib/                # 纯函数与客户端；README.md 是工具索引（脚本对账）
│   │   ├── types/acp.ts        # 领域类型，与 server/internal/model 对齐
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
        ├── service/
        │   ├── agent.go / session.go / broker.go / system.go / fs.go / terminal.go
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

输入框支持：**粘贴/上传图片**、**@ 引用文件**（后端读内容嵌入 prompt；文件树右键与预览面板也可添加引用，**文件夹引用嵌入两层目录清单**而非全文）、**`/` 斜杠命令补全**（清单来自 agent），以及 **turn 进行中直接插话**（不用等上一轮结束）。

单进程部署（后端托管前端产物）：`make serve`；macOS 桌面版打包：`make app`（见下节）。

## macOS 桌面版

`make app` 一键打包出 `build/app/ACPP.app`：Swift/AppKit 菜单栏壳 + 捆绑 acp-server + 前端产物，图标全部由脚本程序化绘制（仓库不存二进制），ad-hoc 签名本机直接用。选型与行为决策见 [docs/adr-004](docs/adr-004-macos-桌面壳.md)。

行为约定：

- **关闭 ≠ 退出**：关闭按钮 / Cmd+W / Cmd+Q / Dock 退出都只是隐藏窗口，服务常驻菜单栏；**真退出只有菜单栏图标右键 → 「退出 ACPP」**（系统注销/关机也会放行，并回收全部子进程）。
- **菜单栏图标**：左键切换主窗口显隐；右键菜单：打开主窗口 / 在浏览器中打开 / 允许局域网访问 / 复制局域网链接 / 重启服务 / 打开服务日志 / 退出。
- **端口固定 `48090`**，与开发态 48080 隔离——`make dev` 与桌面版互不误杀，可同时运行；数据共用 `~/.acpp`，桌面版和 dev 看到同样的会话。
- **局域网共享默认关**（工作区终端是任意命令执行面，见 §安全姿态）。菜单栏开启后服务监听 `0.0.0.0`，「复制局域网链接」得到 `http://<局域网IP>:48090/`，发给局域网内其他设备即可在浏览器使用完整 web 端。切换开关会重启后台服务（agent 上下文在 runtime 侧持久化，续聊自动恢复）。
- 服务日志：`~/Library/Logs/ACPP/server.log`。agent 子进程的 PATH 取自登录 shell——GUI app 默认拿不到 Homebrew 路径，壳启动时注入，否则拉不起 `codex-acp` / `claude-agent-acp`。

打包脚本 [scripts/build-macos-app.sh](scripts/build-macos-app.sh)（`--skip-web` 复用已有前端产物提速，`APP_VERSION` 覆盖版本号）；壳源码在 [desktop/macos/](desktop/macos/)。

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
- 每轮结束后正文、思考、工具调用分别落库，并推一条 `message_saved`；前端用库里的完整内容取代流式拼接的文本，所以偶尔丢一个 chunk 也不会留下残缺消息。

## API

统一响应 `{"data": ...}` 或 `{"error": "..."}`，列表再包一层 `{items, total, page, pageSize}`。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/health` | 健康检查与版本 |
| GET | `/api/fs/dirs` | 列目录（`?path=`，空为家目录），供工作目录选择器导航 |
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
| GET/POST | `/api/sessions` | 会话列表（`?agentId=&page=&pageSize=`，按更新时间倒序分页）/ 新建 |
| GET/DELETE | `/api/sessions/{id}` | 会话详情（**Peek：绝不拉进程**，查看记录零成本；未连接时 `settings`/`commands` 由 agent 探测缓存降级拼出，`Current*` 留空） / 删除（回收子进程，并尽力调 `session/delete` 清掉 agent 侧线程历史） |
| GET | `/api/sessions/{id}/messages` | 历史消息（`?limit=` 取尾部 N 条，`?before=<id>` 加载更早） |
| GET | `/api/sessions/{id}/fs/entries` | 工作区文件树（`?path=&depth=`，depth≤2；gitignore + 固定黑名单过滤，路径限制在会话 cwd 内） |
| GET | `/api/sessions/{id}/fs/file` | 工作区文件预览（`?path=`；1MB 截断、二进制检测，同上 path guard） |
| GET | `/api/sessions/{id}/git/overview` | git 汇总：分支、upstream、ahead/behind、变更文件（含 numstat）、未推送 commit；非仓库返回 `isRepo:false` |
| GET | `/api/sessions/{id}/git/diff` | 单文件工作区 diff（`?path=`，返回 HEAD 版与工作区版全文，行级对齐在前端） |
| GET | `/api/sessions/{id}/git/commits/{sha}` | 提交详情（文件清单）；带 `?path=` 时返回该文件在这条提交前后的全文 |
| GET/POST | `/api/sessions/{id}/terminals` | 工作区终端列表 / 新建（会话 cwd 起交互 shell，每会话上限见 `ACP_MAX_TERMINALS`） |
| DELETE | `/api/sessions/{id}/terminals/{tid}` | 关闭终端（杀 pty） |
| WS | `/api/sessions/{id}/terminals/{tid}/ws` | 终端双向流：二进制帧 = 原始字节，文本帧 = `{"type":"resize","cols","rows"}`；断线后 pty 保活 30s 供重连（带回放缓冲） |
| POST | `/api/sessions/{id}/open` | 拉起 agent 并握手，幂等。**前端不再主动调**——发消息时 `send` 顺路连接（懒连接），连接完成经 SSE 推 `settings`/`commands` |
| POST | `/api/sessions/{id}/send` | 发一轮（`{content, images?, files?}`：图片 base64、@ 引用文件路径由后端读内容嵌入），立即返回；**turn 进行中再发会插进当前轮**（claude 排队为独立一轮，codex steering 注入当前轮） |
| GET | `/api/sessions/{id}/events` | **SSE 事件流** |
| GET | `/api/sessions/{id}/transcript` | 线级转录 JSONL 原样下发（`http.ServeFile`，支持 Range 字节续读——工作区 logs 面板靠它轮询增量实时跟随） |
| GET | `/api/system` | 系统配置：当前/默认数据目录，`pendingDir` 表示已迁移待重启 |
| PUT | `/api/system/data-dir` | 迁移数据目录（`{dataDir}` 绝对路径）：`VACUUM INTO` 在线快照 + 转录拷贝 + 写 `~/.acpp/config.json`，旧数据保留，重启后生效 |
| POST | `/api/sessions/{id}/cancel` | 中止当前轮 |
| PUT | `/api/sessions/{id}/settings` | 统一设置（`{model?, effort?, level?, plan?, fast?}` 逐项可选），响应带最新 `Settings`；未连接的老会话会先幂等拉起进程再应用 |
| POST | `/api/sessions/{id}/permission` | 回传权限裁决（`{permissionId, optionId}`，optionId 空=取消） |

SSE 事件的 `kind`：`user_message`、`message_chunk`、`thought_chunk`、`tool_call`、`permission`、`permission_done`、`plan`、`settings`、`usage`、`commands`、`elicitation`、`elicitation_done`、`turn_end`、`message_saved`、`turn_done`、`error`。每条带单调递增的 `seq`，断线重连时用它去重。`settings` 在 agent 自行切档/改配置时带全量统一视图；`usage` 是上下文用量 `{used, size}`；`turn_end` 附带本轮 token 计量（两端交集字段）；`permission` 表示 agent 阻塞等用户裁决（带选项列表），裁决走上表的 permission 端点。

## 数据模型

- **Agent** — 可通过 stdio 启动的 agent 配置（`command` / `args` / `env` / `cwd`），`args` 与 `env` 以 JSON 文本存入 SQLite。产品形态上固定为内置的 claude / codex 两条记录（启动时缺失自动预置、按 name 判存不覆盖用户配置，见 adr-005），API 仍是通用的 `/api/agents`。`flavor` / `models` / `commands` / `skeleton` 是注册/更新后自动探测的缓存（拉临时会话读能力）：模型与命令供草稿态展示与 `/` 补全（条目带 `disabled` 标记，重探不清空取舍）；`skeleton` 是模型之外的设置骨架（efforts/levels/plan/fast 支持位），与模型清单一起构成未连接会话的完整降级设置视图。模型条目支持 `alias`（配置页起显示别名，所有模型下拉优先显示）；`fastPolicy` 是快速模式取舍（首探按 flavor 落默认：claude 因额外计费默认 off，其余 on；off 时快速开关不出现在任何界面）。
- **Session** — 对应一次 `session/new`，`acpSessionId` 是 agent 返回的 uuid v7，`stopReason` 记录上一轮的结束原因。`lastSettings` 是最后一次生效的统一设置当前值快照（设置视图每次变化时写回），恢复会话的工具栏靠它显示与断开前一致的当前值。`state` 语义：`active` 只表示**有一轮正在跑**；空闲子进程超时会被回收（state 归 `idle`），服务重启时遗留的 `active` 也会归一——续聊时凭 `acpSessionId` 用 `session/load` 恢复上下文，进程挂不挂着不影响会话可用性。
- **Message** — 会话内一条记录，`kind` 覆盖 `session/update` 的各类内容块，结构化内容放 `payload`。**不落库**（adr-003）：它是转录重建器的输出 DTO 与消息接口的响应契约，事实源是转录 JSONL。

## 安全姿态

三层，缺一不可，且各自边界诚实：

1. **cwd 隔离** — `session/new` 的 `cwd` 必须是绝对路径，不存在会先创建。
2. **fs 代理 path guard** — 声明了 `fs` capability，agent 的 `fs/read_text_file` / `fs/write_text_file` 会走到我们进程里，路径解析成 canonical 形式后必须落在 cwd 内。**注意这条不是可靠拦截点**：codex 用自带 shell 完全不走 fs 代理，claude 0.63 实测权限批准后也由 SDK 自行落盘。它只是纵深防御的一层，真正的隔离靠第 3 条之外的 runtime 沙箱档 + OS 兜底。
3. **权限裁决** — `session/request_permission` 挂起交给用户在界面上点选（批准/拒绝），拒绝真实生效（实测文件不会被创建）。runtime 只在当前权限档认为需要时才会问，所以它仍是策略层不是安全边界——真正的隔离要靠 runtime 自身的沙箱档位 + OS 级兜底。

**工作区终端是本机任意命令执行面**：`/terminals` 端点在会话 cwd 起真实交互 shell（用户显式操作才创建），与 agent 已有的命令执行权限面同级。服务只监听 127.0.0.1；若把 `ACP_ADDR` 改成对外地址，这个面会随之暴露，必须配合网络层访问控制。macOS 桌面版的「允许局域网访问」开关就是这个暴露面，因此默认关闭，只应在可信局域网内开启。

另外启动 agent 前会摘掉嵌套会话标记（`CLAUDECODE`、`CODEX_SANDBOX` 等）。不摘的话，从 Claude Code 终端启动本服务时 agent 会误判自己跑在另一个 agent 内部而拒绝服务——这个坑只在那种场景复现，本机开发时碰不到。

## 配置

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `ACP_ADDR` | `127.0.0.1:48080` | 监听地址（macOS 桌面版由壳固定传 `48090`，局域网开关切换 host） |
| `ACP_DATA_DIR` | `~/.acpp` | 数据根目录（db 与转录都派生于它）。优先级：本变量 > `~/.acpp/config.json` 里设置面板选定的目录 > 默认。首次启动自动创建；旧版 `server/data` 的存量数据自动迁入（拷贝，原数据保留） |
| `ACP_DSN` | `<dataDir>/acp.db` | SQLite 文件路径（显式设置时覆盖派生值） |
| `ACP_CORS_ORIGINS` | `http://localhost:45173` | 允许的跨域来源，逗号分隔 |
| `ACP_WEB_DIR` | 空 | 前端产物目录，设置后由后端托管静态文件 |
| `ACP_MAX_SESSIONS` | `8` | 同时活着的 agent 子进程上限 |
| `ACP_IDLE_TIMEOUT` | `10m` | 空闲会话子进程的回收时限（`0` 关闭）。上下文留在 agent 侧，续聊时 `session/load` 无感恢复 |
| `ACP_TURN_TIMEOUT` | `0`（不限时） | 单轮硬上限。长程任务跑几个小时是正常使用方式；turn 进行中（含等待权限/提问裁决）不会被空闲回收 |
| `ACP_MAX_TERMINALS` | `5` | 每会话的工作区终端（pty）实例上限 |
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

codex 的 `CODEX_HOME` 隔离把家目录整体重定向到 `<dataDir>/codex-home`(codex 运行数据写这里,几 MB 量级),机器级技能连 `/skills` 都不再列出——比会话级禁用(`CODEX_CONFIG` 的 `enabled=false` 只挡使用不挡显示)彻底。家目录里 `auth.json` 软链系统的(静态 key、跟随登录态、不复制密钥),`config.toml` 复制系统副本(避免 codex 写回污染系统 config),`skills` 软链技能包。副作用:切换到本方案后,旧 codex 会话的 thread 存在系统 `~/.codex`、新 home 找不到,首次恢复会回退 `session/new`(丢一次上下文),之后正常。认证不隔离:claude 用系统钥匙串登录态、codex 用系统 `~/.codex` 的 auth/config。

## 多语言

中文为默认与兜底语言，右上角切换，选择存在 localStorage（`acp-language`）。文案在 `src/i18n/locales/{zh,en}.ts`，`i18next.d.ts` 做了类型增强——写错 key 在编译期就会报错，不会等到运行时才发现少了一句翻译。

## 添加 shadcn 组件

```bash
cd web && npx shadcn@latest add <component>
```

组件基于 **Base UI**（不是 Radix），自定义触发元素用 `render={<Link to="..." />}`，不是 `asChild`。

## 尚未实现

- 侧边栏的 Tools / Logs / Connections 与 agent 的新建页仍是占位页（详情页已是配置页）。
- **技能助理**：复用对话面板、把工作目录固定到技能源目录 `<dataDir>/skills/<name>/`,让 agent 帮忙起草/优化 SKILL.md。技能管理与会话注入均已落地,助理待做。
- **工作区面板**（[adr-002](docs/adr-002-会话工作区多面板.md)）M1–M3 已落地：dockview 骨架、文件树/预览、diff / 未推送 commit、多实例 PTY 终端与引用联动。剩 M4 打磨（布局预设、diff 虚拟滚动、压力验收）。
- **默认档**：会话开在 runtime 默认档上（codex 默认 auto-edit 级、claude 默认 safe 级——两端不同），未强制归一；用户可在会话内随时切统一权限档。
- **权限裁决不落历史**：挂起/裁决过程只在当前轮展示，转录里有原始数据但重建暂未生成历史卡片。
