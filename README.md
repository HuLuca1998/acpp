# ACP Console

Agent Client Protocol 的本地管理面板：注册 agent、发起会话、查看消息流与工具调用。

- **前端** — Vite + React 19 + TypeScript + shadcn/ui（Base UI + Tailwind v4）
- **后端** — Go + net/http + GORM + SQLite（`glebarez/sqlite`，纯 Go 无需 CGO）

## 目录结构

```
acpp/
├── Makefile                    # 常用命令入口，make help 查看
├── web/                        # 前端
│   ├── src/
│   │   ├── main.tsx            # 入口：ThemeProvider + TooltipProvider + BrowserRouter
│   │   ├── App.tsx             # 路由表
│   │   ├── routes/             # 页面
│   │   │   ├── dashboard-layout.tsx  # dashboard-01 外壳（侧边栏 + 顶栏 + Outlet）
│   │   │   ├── overview.tsx          # 概览（指标卡 + 图表 + 数据表）
│   │   │   ├── agents.tsx            # agent 列表
│   │   │   ├── sessions.tsx          # 会话列表
│   │   │   ├── placeholder.tsx       # 未实现页面的占位
│   │   │   └── not-found.tsx
│   │   ├── components/
│   │   │   ├── ui/             # shadcn 组件，由 CLI 生成，尽量不手改
│   │   │   ├── app-sidebar.tsx # 侧边栏导航配置
│   │   │   ├── nav-*.tsx       # 导航分组
│   │   │   └── ...             # dashboard-01 自带的图表 / 数据表 / 指标卡
│   │   ├── lib/api.ts          # 后端 API 客户端
│   │   ├── types/acp.ts        # ACP 领域类型，与 server/internal/model 对齐
│   │   ├── hooks/
│   │   ├── data/               # 静态示例数据
│   │   └── index.css           # Tailwind v4 主题变量
│   ├── components.json         # shadcn 配置
│   └── vite.config.ts          # /api 代理到 127.0.0.1:8080
└── server/                     # 后端
    ├── cmd/server/main.go      # 启动、优雅关闭
    ├── internal/
    │   ├── config/             # 环境变量配置
    │   ├── db/                 # GORM 连接 + AutoMigrate
    │   ├── model/              # Agent / Session / Message + JSON 字段类型
    │   ├── service/            # 业务逻辑，哨兵错误 ErrNotFound / ErrInvalid
    │   └── httpapi/            # 路由、handler、中间件、统一响应
    └── data/                   # SQLite 文件（已 gitignore）
```

## 快速开始

```bash
make install      # 安装依赖

make dev-server   # 终端 1：后端 http://127.0.0.1:8080
make dev-web      # 终端 2：前端 http://localhost:5173
```

前端通过 Vite 代理把 `/api` 转发到后端，开发期无跨域问题。

单进程部署（后端托管前端产物）：

```bash
make serve        # 构建前后端并由 Go 服务在 :8080 提供全部内容
```

## API

所有响应统一为 `{"data": ...}` 或 `{"error": "..."}`，列表额外包一层 `{items, total, page, pageSize}`。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/health` | 健康检查与版本 |
| GET | `/api/agents` | agent 列表 |
| POST | `/api/agents` | 新建 agent |
| GET | `/api/agents/{id}` | agent 详情 |
| PUT | `/api/agents/{id}` | 更新 agent |
| DELETE | `/api/agents/{id}` | 删除 agent |
| GET | `/api/sessions` | 会话列表，支持 `?agentId=` |
| POST | `/api/sessions` | 新建会话 |
| GET | `/api/sessions/{id}` | 会话详情 |
| DELETE | `/api/sessions/{id}` | 删除会话 |
| GET | `/api/sessions/{id}/messages` | 会话消息 |
| POST | `/api/sessions/{id}/messages` | 追加消息 |

## 数据模型

- **Agent** — 一个可通过 stdio 启动的 agent 进程配置（`command` / `args` / `env` / `cwd`），`args` 与 `env` 以 JSON 文本存入 SQLite。
- **Session** — 对应一次 `session/new`，`acpSessionId` 保存 agent 侧返回的会话 ID，`stopReason` 记录 `session/prompt` 的结束原因。
- **Message** — 会话内的一条记录，`kind` 覆盖 `session/update` 的各类内容块（`text` / `thought` / `tool_call` / `tool_result` / `permission_request` / `plan`），结构化内容放在 `payload`。

## 配置

后端全部通过环境变量配置：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `ACP_ADDR` | `127.0.0.1:8080` | 监听地址 |
| `ACP_DSN` | `data/acp.db` | SQLite 文件路径 |
| `ACP_CORS_ORIGINS` | `http://localhost:5173` | 允许的跨域来源，逗号分隔 |
| `ACP_WEB_DIR` | 空 | 前端产物目录，设置后由后端托管静态文件 |
| `ACP_DEBUG` | 空 | 非空则打开 SQL 与 debug 日志 |

前端可用 `VITE_API_BASE` 覆盖 API 前缀，默认 `/api`。

## 添加 shadcn 组件

```bash
cd web
npx shadcn@latest add <component>
```

组件基于 **Base UI**（不是 Radix），自定义触发元素用 `render={<Link to="..." />}`，不是 `asChild`。

## 尚未实现

侧边栏中 Tools / Logs / Settings / Connections 等入口目前是占位页；真正的 ACP 进程管理（stdio 启动、`initialize` 握手、`session/prompt` 转发、`session/update` 推流）尚未接入，当前后端只负责配置与记录的持久化。
