# ACP Console

Agent Client Protocol 的本地管理面板：注册 agent、发起会话、与 agent 连续对话，实时看到回复、思考与工具调用。

- **前端** — Vite + React 19 + TypeScript + shadcn/ui（Base UI + Tailwind v4）+ i18next（中/英）
- **后端** — Go + net/http + GORM + SQLite（`glebarez/sqlite`，纯 Go 无需 CGO）+ 自写的 ACP 客户端

后端扮演 ACP 里的 **client** 角色：拉起 agent 子进程 → `initialize` 握手 → `session/new` → `session/prompt` → 消费 `session/update` 事件流 → 裁决权限。**不实现 agentic loop**，那是 agent（`codex-acp` / `claude-agent-acp`）自己的事。

## 目录结构

```
acpp/
├── AGENTS.md                   # 通用工程规范（人与 AI 协作者共同遵守，CLAUDE.md 指向它）
├── Makefile                    # 常用命令入口，make help 查看
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
│   │   │   └── ...             # 状态点、工具调用块、markdown、切换器等
│   │   ├── lib/api.ts          # 后端 API 客户端
│   │   ├── types/acp.ts        # 领域类型，与 server/internal/model 对齐
│   │   └── index.css           # Tailwind v4 主题变量
│   └── vite.config.ts          # /api 代理到 127.0.0.1:8080
└── server/
    ├── AGENTS.md               # 后端规范
    ├── cmd/server/main.go      # 启动、优雅关闭、回收 agent 子进程
    └── internal/
        ├── acp/                # ACP 客户端
        │   ├── protocol.go     # JSON-RPC 与 ACP 类型
        │   ├── conn.go         # stdio 连接：ndjson 读写、请求关联、反向调用
        │   ├── runtime.go      # runtime 注册表 + 嵌套环境变量清理
        │   └── manager.go      # 会话池、握手、turn 调度、权限与 fs 代理
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

先装一个 ACP runtime（本项目实测用 codex）：

```bash
npm i -g @agentclientprotocol/codex-acp      # 或 @agentclientprotocol/claude-agent-acp
codex login                                   # 复用本机登录态
```

```bash
make install
make dev-server   # 终端 1：后端 http://127.0.0.1:8080
make dev-web      # 终端 2：前端 http://localhost:5173
```

然后在界面里：**Agents → 添加 agent**（命令填 `codex-acp`）→ 任意页面点 **新建会话**（弹窗）→ 开聊。

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
| GET/POST | `/api/agents` | agent 列表 / 新建 |
| GET/PUT/DELETE | `/api/agents/{id}` | agent 详情 / 更新 / 删除 |
| GET/POST | `/api/sessions` | 会话列表（支持 `?agentId=`）/ 新建 |
| GET/DELETE | `/api/sessions/{id}` | 会话详情 / 删除（同时回收子进程） |
| GET | `/api/sessions/{id}/messages` | 历史消息 |
| POST | `/api/sessions/{id}/open` | 拉起 agent 并握手，幂等 |
| POST | `/api/sessions/{id}/send` | 发一轮，立即返回 |
| GET | `/api/sessions/{id}/events` | **SSE 事件流** |
| POST | `/api/sessions/{id}/cancel` | 中止当前轮 |

SSE 事件的 `kind`：`user_message`、`message_chunk`、`thought_chunk`、`tool_call`、`permission`、`plan`、`mode`、`turn_end`、`message_saved`、`turn_done`、`error`。每条带单调递增的 `seq`，断线重连时用它去重。

## 数据模型

- **Agent** — 可通过 stdio 启动的 agent 配置（`command` / `args` / `env` / `cwd`），`args` 与 `env` 以 JSON 文本存入 SQLite。
- **Session** — 对应一次 `session/new`，`acpSessionId` 是 agent 返回的 uuid v7，`stopReason` 记录上一轮的结束原因。
- **Message** — 会话内一条记录，`kind` 覆盖 `session/update` 的各类内容块，结构化内容放 `payload`。

## 安全姿态

三层，缺一不可，且各自边界诚实：

1. **cwd 隔离** — `session/new` 的 `cwd` 必须是绝对路径，不存在会先创建。
2. **fs 代理 path guard** — 声明了 `fs` capability，agent 的 `fs/read_text_file` / `fs/write_text_file` 会走到我们进程里，路径解析成 canonical 形式后必须落在 cwd 内。**注意这条只覆盖走 fs 代理的操作**：claude 走，codex 用自带 shell 完全不走。
3. **权限裁决** — `session/request_permission` 一律放行（`allow_once` 优先）。权限回调是策略层不是安全边界，用它做安全会同时失去安全性和可用性。真正的隔离要靠 runtime 自身的沙箱档位 + OS 级兜底。

另外启动 agent 前会摘掉嵌套会话标记（`CLAUDECODE`、`CODEX_SANDBOX` 等）。不摘的话，从 Claude Code 终端启动本服务时 agent 会误判自己跑在另一个 agent 内部而拒绝服务——这个坑只在那种场景复现，本机开发时碰不到。

## 配置

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `ACP_ADDR` | `127.0.0.1:8080` | 监听地址 |
| `ACP_DSN` | `data/acp.db` | SQLite 文件路径 |
| `ACP_CORS_ORIGINS` | `http://localhost:5173` | 允许的跨域来源，逗号分隔 |
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

- 侧边栏的 Tools / Logs / Settings / Connections 与 agent 的新建/详情页仍是占位页。
- **会话恢复**：服务重启后子进程没了，当前是重新 `session/new`（等于新对话）。协议里的 `session/load`（重放历史）/ `session/resume`（不重放）都没接。
- **权限交互**：现在一律自动放行，没有把请求挂起交给用户裁决的界面。
- **`session/set_mode`**：没调用，跑在 runtime 的默认档上（codex 默认是 `agent`，不是只读）。
