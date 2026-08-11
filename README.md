# ACP Console

Agent Client Protocol 的本地管理面板：注册 agent、发起会话、与 agent 连续对话，实时看到回复、思考与工具调用。

- **前端** — Vite + React 19 + TypeScript + shadcn/ui（Base UI + Tailwind v4）+ i18next（中/英）
- **后端** — Go + net/http + GORM + SQLite（`glebarez/sqlite`，纯 Go 无需 CGO）+ 自写的 ACP 客户端

后端扮演 ACP 里的 **client** 角色：拉起 agent 子进程 → `initialize` 握手 → `session/new` → `session/prompt` → 消费 `session/update` 事件流 → 裁决权限。**不实现 agentic loop**，那是 agent（`codex-acp` / `claude-agent-acp`）自己的事。

两条 runtime 的语义差异（权限档、模型清单位置、配置项 id、rawOutput 形状等）全部收敛在 adapter 层，上层与前端只见**统一词汇表**（模型 / 思考深度五档 / 权限三档 / plan / fast）。规范原则是**交集**：两条 ACP 都能做到的才进统一接口，单端独有的功能废弃。设计与差异清单见 [docs/adr-001](docs/adr-001-codex-claude-差异收敛.md)。

## 目录结构

```
acpp/
├── AGENTS.md                   # 通用工程规范（人与 AI 协作者共同遵守，CLAUDE.md 指向它）
├── Makefile                    # 常用命令入口，make help 查看
├── docs/                       # 决策记录（adr-001：codex/claude 差异收敛）
├── scripts/                    # 开发辅助脚本（acp-probe.py：协议探针）
├── web/                        # 前端
│   ├── AGENTS.md               # 前端规范 + 设计规范
│   ├── src/
│   │   ├── main.tsx            # 入口：Theme + Tooltip + i18n + Router
│   │   ├── App.tsx             # 路由表
│   │   ├── routes/
│   │   │   ├── dashboard-layout.tsx  # dashboard-01 外壳（侧边栏 + 顶栏 + Outlet）
│   │   │   ├── overview.tsx          # 概览（真实指标卡 + 最近会话 + agent 状态）
│   │   │   ├── agents.tsx            # agent 列表
│   │   │   ├── sessions.tsx          # 会话列表
│   │   │   ├── session-new.tsx       # 草稿会话页（首条消息落地才建会话）
│   │   │   ├── session-chat.tsx      # 对话页（流式）
│   │   │   ├── placeholder.tsx       # 未实现页面的占位
│   │   │   └── not-found.tsx
│   │   ├── hooks/use-chat.ts   # SSE 订阅 + 流式状态机
│   │   ├── i18n/               # 语言资源与配置
│   │   │   ├── index.ts        # i18next 初始化（localStorage 记住选择）
│   │   │   ├── i18next.d.ts    # 让 t("chat.send") 受类型检查
│   │   │   └── locales/{zh,en}.ts
│   │   ├── components/
│   │   │   ├── ui/             # shadcn 组件（生成为起点，可直改；升级走 --diff 合并）
│   │   │   ├── app-sidebar.tsx # 侧边栏导航
│   │   │   ├── language-switcher.tsx
│   │   │   ├── chat/           # 聊天专用：markdown/工具调用/计划卡/思考块/复制
│   │   │   └── ...             # 状态点、后端状态、新建会话弹窗、切换器等
│   │   ├── lib/api.ts          # 后端 API 客户端
│   │   ├── types/acp.ts        # 领域类型，与 server/internal/model 对齐
│   │   └── index.css           # Tailwind v4 主题变量
│   └── vite.config.ts          # /api 代理到 127.0.0.1:48080
└── server/
    ├── AGENTS.md               # 后端规范
    ├── cmd/server/main.go      # 启动、优雅关闭、回收 agent 子进程
    └── internal/
        ├── acp/                # ACP 客户端
        │   ├── protocol.go     # JSON-RPC 与 ACP 类型
        │   ├── conn.go         # stdio 连接：ndjson 读写、请求关联、反向调用
        │   ├── runtime.go      # runtime 注册表 + 嵌套环境变量清理
        │   ├── adapter.go      # 统一词汇表（模型/思考深度/权限档/plan/fast）+ Adapter 接口
        │   ├── adapter_*.go    # claude / codex / generic 三个实现，差异全部住在这里
        │   └── manager.go      # 会话池、握手、turn 调度、统一设置、权限与 fs 代理
        ├── config/             # 环境变量配置
        ├── db/                 # GORM 连接 + AutoMigrate
        ├── model/              # Agent / Session / Message
        ├── service/
        │   ├── agent.go
        │   ├── session.go
        │   ├── chat.go         # ACP 事件 → 持久化 + 广播
        │   └── broker.go       # SSE 广播器与本轮内容累积
        └── httpapi/            # 路由、handler、中间件、统一响应
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

然后在界面里：**Agents → 添加 agent**（命令填 `codex-acp` 或 `claude-agent-acp`）→ 任意页面点 **新建会话** 直接进入对话。新会话与老会话是**同一个页面**，只有两处差异：草稿态的模型选择器按 agent 分组列出全部可用模型（注册后自动探测缓存，选哪个模型就用哪个 agent），且状态栏里的工作目录可点击修改；发出首条消息才真正创建会话，此后模型只能在当前 agent 内切、工作目录不可再改。

输入框支持：**粘贴/上传图片**、**@ 引用文件**（后端读内容嵌入 prompt）、**`/` 斜杠命令补全**（清单来自 agent），以及 **turn 进行中直接插话**（不用等上一轮结束）。

单进程部署（后端托管前端产物）：`make serve`。

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
| GET/POST | `/api/agents` | agent 列表 / 新建（新建后自动探测模型与命令清单） |
| GET/PUT/DELETE | `/api/agents/{id}` | agent 详情 / 更新 / 删除 |
| POST | `/api/agents/{id}/probe` | 重探统一设置能力（flavor、模型与命令清单），同步返回 |
| PUT | `/api/agents/{id}/catalog` | 配置页勾选：更新 models/commands 的启用状态（禁用只影响本软件的下拉与补全，agent 侧能力不变） |
| GET/POST | `/api/sessions` | 会话列表（支持 `?agentId=`）/ 新建 |
| GET/DELETE | `/api/sessions/{id}` | 会话详情 / 删除（回收子进程，并尽力调 `session/delete` 清掉 agent 侧线程历史） |
| GET | `/api/sessions/{id}/messages` | 历史消息 |
| POST | `/api/sessions/{id}/open` | 拉起 agent 并握手，幂等 |
| POST | `/api/sessions/{id}/send` | 发一轮（`{content, images?, files?}`：图片 base64、@ 引用文件路径由后端读内容嵌入），立即返回；**turn 进行中再发会插进当前轮**（claude 排队为独立一轮，codex steering 注入当前轮） |
| GET | `/api/sessions/{id}/events` | **SSE 事件流** |
| POST | `/api/sessions/{id}/cancel` | 中止当前轮 |
| PUT | `/api/sessions/{id}/settings` | 统一设置（`{model?, effort?, level?, plan?, fast?}` 逐项可选），响应带最新 `Settings` |
| POST | `/api/sessions/{id}/permission` | 回传权限裁决（`{permissionId, optionId}`，optionId 空=取消） |

SSE 事件的 `kind`：`user_message`、`message_chunk`、`thought_chunk`、`tool_call`、`permission`、`permission_done`、`plan`、`settings`、`usage`、`commands`、`elicitation`、`elicitation_done`、`turn_end`、`message_saved`、`turn_done`、`error`。每条带单调递增的 `seq`，断线重连时用它去重。`settings` 在 agent 自行切档/改配置时带全量统一视图；`usage` 是上下文用量 `{used, size}`；`turn_end` 附带本轮 token 计量（两端交集字段）；`permission` 表示 agent 阻塞等用户裁决（带选项列表），裁决走上表的 permission 端点。

## 数据模型

- **Agent** — 可通过 stdio 启动的 agent 配置（`command` / `args` / `env` / `cwd`），`args` 与 `env` 以 JSON 文本存入 SQLite。`flavor` / `models` / `commands` 是注册/更新后自动探测的缓存（拉临时会话读能力），供草稿态在建会话之前展示跨 agent 模型清单与 `/` 补全；每个条目带 `disabled` 标记（agent 详情页勾选，重探不清空取舍）。
- **Session** — 对应一次 `session/new`，`acpSessionId` 是 agent 返回的 uuid v7，`stopReason` 记录上一轮的结束原因。
- **Message** — 会话内一条记录，`kind` 覆盖 `session/update` 的各类内容块，结构化内容放 `payload`。

## 安全姿态

三层，缺一不可，且各自边界诚实：

1. **cwd 隔离** — `session/new` 的 `cwd` 必须是绝对路径，不存在会先创建。
2. **fs 代理 path guard** — 声明了 `fs` capability，agent 的 `fs/read_text_file` / `fs/write_text_file` 会走到我们进程里，路径解析成 canonical 形式后必须落在 cwd 内。**注意这条不是可靠拦截点**：codex 用自带 shell 完全不走 fs 代理，claude 0.63 实测权限批准后也由 SDK 自行落盘。它只是纵深防御的一层，真正的隔离靠第 3 条之外的 runtime 沙箱档 + OS 兜底。
3. **权限裁决** — `session/request_permission` 挂起交给用户在界面上点选（批准/拒绝），拒绝真实生效（实测文件不会被创建）。runtime 只在当前权限档认为需要时才会问，所以它仍是策略层不是安全边界——真正的隔离要靠 runtime 自身的沙箱档位 + OS 级兜底。

另外启动 agent 前会摘掉嵌套会话标记（`CLAUDECODE`、`CODEX_SANDBOX` 等）。不摘的话，从 Claude Code 终端启动本服务时 agent 会误判自己跑在另一个 agent 内部而拒绝服务——这个坑只在那种场景复现，本机开发时碰不到。

## 配置

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `ACP_ADDR` | `127.0.0.1:48080` | 监听地址 |
| `ACP_DSN` | `data/acp.db` | SQLite 文件路径 |
| `ACP_CORS_ORIGINS` | `http://localhost:45173` | 允许的跨域来源，逗号分隔 |
| `ACP_WEB_DIR` | 空 | 前端产物目录，设置后由后端托管静态文件 |
| `ACP_MAX_SESSIONS` | `8` | 同时活着的 agent 子进程上限 |
| `ACP_DEBUG` | 空 | 非空则打开 SQL 与 debug 日志 |

前端可用 `VITE_API_BASE` 覆盖 API 前缀，默认 `/api`。

## 多语言

中文为默认与兜底语言，右上角切换，选择存在 localStorage（`acp-language`）。文案在 `src/i18n/locales/{zh,en}.ts`，`i18next.d.ts` 做了类型增强——写错 key 在编译期就会报错，不会等到运行时才发现少了一句翻译。

## 添加 shadcn 组件

```bash
cd web && npx shadcn@latest add <component>
```

组件基于 **Base UI**（不是 Radix），自定义触发元素用 `render={<Link to="..." />}`，不是 `asChild`。

## 尚未实现

- 侧边栏的 Tools / Logs / Settings / Connections 与 agent 的新建页仍是占位页（详情页已是配置页）。
- **默认档**：会话开在 runtime 默认档上（codex 默认 auto-edit 级、claude 默认 safe 级——两端不同），未强制归一；用户可在会话内随时切统一权限档。
- **权限裁决不落历史**：挂起/裁决过程只在当前轮展示，转录里有原始数据但重建暂未生成历史卡片。
