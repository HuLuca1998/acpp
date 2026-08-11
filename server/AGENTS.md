# 后端规范（server/）

适用于 `server/` 下所有 Go 代码。通用规则（分包原则、跨端契约、提交与验证）见 [../AGENTS.md](../AGENTS.md)，此处只讲后端。

## 1. 分层与依赖方向

```
cmd/server   → 只做装配：读配置、连库、构建 service、挂路由、优雅关闭
httpapi      → HTTP 层：路由、handler、中间件、统一响应。不写业务逻辑
service      → 业务层：所有业务规则、事务、对 acp 的编排
db / model   → GORM 连接与数据模型
acp          → 独立的 ACP 协议客户端，不 import 本项目其他包（标准库除外）
```

依赖只允许自上而下：`httpapi → service → {db, model, acp}`。**禁止**反向依赖（如 service import httpapi）、跨层捷径（如 httpapi 直接摸 db）。

新代码先找同层邻居照着写：新 handler 照 `internal/httpapi/agent.go`，新业务照 `internal/service/agent.go`。

## 2. 命名

| 场景 | 规则 | 示例 |
| --- | --- | --- |
| 包名 | 小写单数、无下划线、宁短勿长 | `httpapi`、`acp`、`model` |
| 文件 | 小写，按资源/职责命名 | `session.go`、`broker.go` |
| 导出符号 | PascalCase；包名已含语境，不重复 | `service.AgentService`、`acp.Conn`（不是 `acp.AcpConn`） |
| 哨兵错误 | `Err` 前缀 | `ErrNotFound`、`ErrInvalid` |
| handler | 私有 struct `xxxHandler`，方法固定用 `list` / `get` / `create` / `update` / `remove` | `agentHandler.list` |

命名说明「是什么/干什么」，不说明「怎么实现」；缩写只用行业公认的（`api`、`db`、`acp`）。

## 3. 文件组织

- 按资源/职责拆同包多文件（参考 `service/` 拆为 `agent.go` / `session.go` / `chat.go` / `broker.go`）。
- 单文件超过 ~400 行是拆分信号；拆出的文件必须有独立可命名的职责。
- 新建包的门槛：有明确单一职责，且不是 `utils` / `common` 杂物包（见根规范 §1.2）。

## 4. HTTP 层铁律

- 响应只能通过 `response.go` 的 `writeData` / `writeError` 出去，handler 内禁止手写 `json.Marshal` / `w.WriteHeader`。
- 请求体解码统一走 `decodeJSON`（含 `DisallowUnknownFields`）。
- handler 只做：解参 → 调 service → 写响应。出现 if/else 业务分支就该下沉到 service。

## 5. 错误处理与日志

- service 层用哨兵错误表达「可预期失败」：`ErrNotFound`、`ErrInvalid`。需要新类别时在 service 定义，并在 `httpapi/response.go` 的 `writeError` 登记状态码映射。
- 包装错误用 `fmt.Errorf("干了什么: %w", err)` 或 `errors.Join`，保留链条。
- 错误要么处理、要么向上抛，**不许**吞掉（`_ = err`）也**不许**处理后又抛（会重复打日志）。
- 日志统一 `slog`，key-value 风格：`slog.Error("write response", "err", err)`。不用 `fmt.Println` / `log.Printf`。

## 6. 并发与 context

- 所有阻塞操作带 `context.Context`，从 `r.Context()` 一路传到底。
- 并发结构（如 broker、会话池）的锁保护范围写清楚，channel 的关闭责任归属写清楚。
- agent 子进程的生命周期归 `acp.Manager` 管，其他层不直接碰进程。

## 7. 注释与测试

- 注释写中文，解释**为什么**（约束、坑、协议行为），不复述代码在做什么。参考现有代码的注释密度。
- 测试：`go test ./...`。表驱动，测导出行为与接口契约，不为私有函数写测试。涉及 ACP 协议解析的逻辑优先补测试。
- 提交前 `gofmt` 干净、`go vet ./...` 无告警（`make lint` 已含）。
