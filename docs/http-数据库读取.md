# acpp 数据库读取 HTTP 接口文档

| 项 | 值 |
| --- | --- |
| 文档版本 | 2026-09-08 |
| 适用服务 | acpp 后端 ≥ 1.2.3（`GET /api/health` 可查） |
| 协议 | HTTP/1.1，请求与响应均为 `application/json; charset=utf-8` |
| 范围 | 数据源清单、库/表/表结构、只读 SQL 查询、MCP 工具面透传。**不含写操作**：写语句由数据源的「只读」开关统一拦截（[README §数据库](../README.md#数据库)） |

样本全部对真实实例实测（dev 实例 2026-09-08）。字段以本文为准，与 [server/internal/datasource](../server/internal/datasource) 的 Go 结构体一一对应。

---

## 1. 基础地址

| 实例 | 基础地址（BASE） | 监听 | 说明 |
| --- | --- | --- | --- |
| 桌面 app（局域网） | `http://192.168.2.16:48090` | `0.0.0.0:48090` | 菜单栏「允许局域网访问」已开。局域网 IP 由 DHCP 分配，**以 `GET /api/system` 返回的 `lanBase` 为准** |
| 本机 dev | `http://127.0.0.1:48080` | 仅回环 | 开发实例，局域网不可达 |

下文所有路径都相对 BASE。命令行用 `make serve-lan` 起的实例监听 `0.0.0.0:48080`，接口完全相同。

> **先读 §3 再从局域网发请求。** 数据库相关端点全部要求 owner 身份，而 owner 只认本机回环来源；从 `192.168.2.16` 直接打这些端点会得 401（无凭证）或 403（租户凭证）。局域网机器要调通，目前唯一办法是 SSH 隧道（§3.3）。

---

## 2. 通用约定

### 2.1 请求头

| 头 | 必填 | 值 | 说明 |
| --- | --- | --- | --- |
| `Content-Type` | POST 必填 | `application/json` | 请求体为 JSON 对象 |
| `Accept` | 否 | `application/json` | 服务端只回 JSON，不看这个头 |
| `Cookie` | 视身份 | `acpp_tenant=<租户 token>` | 租户身份的唯一载体（§3.4）。owner 不带任何凭证 |

不支持 `Authorization` 头，任何值都被忽略。

### 2.2 响应外壳

| 情况 | 形状 |
| --- | --- |
| 成功（单对象/数组） | `{"data": <对象或数组>}` |
| 成功（分页列表） | `{"data": {"items": [...], "total": <int>, "page": <int>, "pageSize": <int>}}` |
| 失败 | `{"error": "<前缀>: <说明>"}`，前缀与状态码对应（§2.3） |

`data` 里的字段一律 camelCase；缺省值的可选字段会**省略**而不是给 `null`（Go 的 `omitempty`）。

### 2.3 状态码

| 状态码 | `error` 前缀 | 含义 |
| --- | --- | --- |
| 200 | — | 成功。**SQL 语句级失败也是 200**，错误落在 `results[i].error`（§5.6） |
| 201 | — | 创建成功（本文档不涉及） |
| 400 | `invalid input` | 参数或请求体不合法（id 非数字、缺 `sql`、缺 `request`…） |
| 401 | `unauthorized` | 无身份：来源非回环且没带有效租户 cookie |
| 403 | `forbidden` | 有身份但无权：租户打 owner 专属前缀（`owner only`）、只读源跑写语句、用连接访问非绑定库 |
| 404 | `not found` | 资源不存在，**或**会话侧按 id 找的数据源不在当前项目内（刻意不区分） |
| 500 | 其它 | 服务端错误，`error` 带原始错误文本 |

### 2.4 分页与排序（仅 `GET /api/datasources`、`GET /api/tools/calls`）

| 查询参数 | 类型 | 缺省 | 说明 |
| --- | --- | --- | --- |
| `page` | int | 1 | 从 1 起，≤0 按 1 |
| `pageSize` | int | 20 | ≤0 按 20，上限 **200** |
| `sort` | string | — | 排序列，白名单见各接口 |
| `order` | `asc` / `desc` | `asc` | 仅在给了 `sort` 时生效 |

### 2.5 值的编码

| 类型 | JSON 形态 | 示例 |
| --- | --- | --- |
| 时间戳（记录字段） | RFC 3339 带时区字符串 | `"2026-09-01T15:03:42.747619+08:00"` |
| 查询结果 `rows` 里的整数、布尔 | number | `1`、`0` |
| `rows` 里的浮点、DECIMAL、字符串、日期时间 | string | `"1.5"`、`"1.25"`、`"2026-09-08 04:58:29"` |
| `rows` 里的 NULL | `null` | |

`rows` 的值由 MySQL 驱动按文本协议返回，**除整数与布尔外一律是字符串**，调用方自行转型。

---

## 3. 鉴权与访问范围

### 3.1 身份判定

| 请求来源 | 携带凭证 | 身份 | 说明 |
| --- | --- | --- | --- |
| 回环地址（127.0.0.1 / ::1） | 无 | **owner** | 全权，零配置 |
| 任意 | `acpp_tenant` 有效 | **租户** | 即使来自回环也按租户算（反向代理防提权） |
| 任意 | `acpp_tenant` 已被 owner 停用 | 被停用 | 除公开路径外一律 403 `access disabled by owner` |
| 非回环 | 无或无效 | 匿名 | 除公开路径外一律 401 `invite required` |

公开路径：`/api/health`、`/api/auth/*`、`/api/mcp/*`（后者靠每会话 token 自带鉴权，见 §7.4）。

### 3.2 各前缀的权限

| 前缀 | owner | 租户 | 说明 |
| --- | --- | --- | --- |
| `/api/datasources/*` | ✅ | ❌ 403 | 管理面：含凭证信息与任意 SQL |
| `/api/tools/*` | ✅ | ❌ 403 | 工具台：能对线上库发任意 SQL |
| `/api/system` | ✅ | ❌ 403 | |
| `/api/workspace/datasources/*` | ✅ | ✅ | 会话侧清单：按 `cwd` 所属项目过滤，租户的 `cwd` 还必须在自己的 root 内 |
| `/api/sessions/{id}/datasources/*` | ✅ | ✅（仅自己的会话） | 同上，按会话 cwd 过滤；别人的会话按 404 |
| `/api/auth/*`、`/api/health` | ✅ | ✅ | 公开 |

**租户没有任何能发 SQL 的端点**。租户查库的唯一通道是在会话里让 AI 调 `db_*` 工具（adr-010）。

### 3.3 从局域网调用（实测 2026-09-08）

对 `http://192.168.2.16:48090` 直接发请求的结果：

| 端点 | 无凭证 | 伪造/无效租户 cookie | 有效租户 cookie |
| --- | --- | --- | --- |
| `GET /api/health` | 200 | 200 | 200 |
| `GET /api/auth/me` | 200 `{"authenticated":false,"owner":false}` | 同左 | 200 `authenticated:true, owner:false` |
| `GET /api/datasources` | 401 | 401 | **403 owner only** |
| `GET /api/tools/servers` | 401 | 401 | **403 owner only** |
| `GET /api/workspace/datasources` | 401 | 401 | 200（仅清单） |

因此局域网机器要用 §5.1–5.6、§5.11–5.12，必须让请求**从主机回环发出**，即 SSH 端口转发：

```bash
# 在局域网机器上：把本地 48090 转发到主机的回环 48090
ssh -N -L 48090:127.0.0.1:48090 luca@192.168.2.16
# 之后 BASE 改用 http://127.0.0.1:48090，身份即 owner
```

服务端**没有** API key / Bearer token 这类非回环 owner 凭证，需要的话是新增能力，不在本文范围。

### 3.4 租户凭证

1. owner 在本机调 `GET /api/tenants`，每条租户带 `inviteUrl`（形如 `http://192.168.2.16:48090/?invite=<token>`），`invite` 参数即 token。
2. 兑换成 cookie：`POST /api/auth/redeem`，body `{"token":"<token>"}`，响应 `Set-Cookie: acpp_tenant=<token>; Path=/; HttpOnly; SameSite=Lax; Max-Age=31536000`。
3. 脚本可跳过兑换直接带头：`Cookie: acpp_tenant=<token>`。

---

## 4. 接口清单

| # | 方法 | 路径 | 权限 | 用途 |
| --- | --- | --- | --- | --- |
| 5.1 | GET | `/api/datasources` | owner | 数据源分页清单 |
| 5.2 | GET | `/api/datasources/{id}` | owner | 单条数据源 |
| 5.3 | GET | `/api/datasources/{id}/databases` | owner | 连接绑定的库（含表数） |
| 5.4 | GET | `/api/datasources/{id}/tables` | owner | 表与视图清单 |
| 5.5 | GET | `/api/datasources/{id}/schema` | owner | 表结构（列、索引、DDL） |
| 5.6 | POST | `/api/datasources/{id}/query` | owner | 执行只读 SQL，结构化结果 |
| 5.7 | GET | `/api/workspace/datasources` | owner / 租户 | 按目录推项目的数据源清单 |
| 5.8 | GET | `/api/workspace/datasources/{dsid}/databases` | owner / 租户 | 同 5.3，项目内可见才有 |
| 5.9 | GET | `/api/workspace/datasources/{dsid}/tables` | owner / 租户 | 同 5.4，项目内可见才有 |
| 5.10 | GET | `/api/sessions/{id}/datasources[/{dsid}/databases\|tables]` | owner / 租户 | 5.7–5.9 的会话版，cwd 取自会话 |
| 5.11 | GET | `/api/tools/servers` | owner | MCP 工具面与工具声明 |
| 5.12 | POST | `/api/tools/inspect` | owner | 向工具面透传一条 JSON-RPC |
| 5.13 | GET | `/api/tools/calls`、`/api/tools/calls/stats` | owner | 工具调用记录与统计 |
| 5.14 | GET | `/api/health`、`/api/auth/me`、`/api/system` | 见各条 | 探活、身份、实例信息 |

---

## 5. 接口详情

### 5.1 数据源清单

`GET /api/datasources` · owner

**查询参数**

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `page`、`pageSize`、`order` | | 否 | §2.4 |
| `sort` | string | 否 | 白名单：`project`、`env`、`database`、`host`、`read_only`、`updated_at` |

**返回** `{"data": {"items": DataSource[], "total", "page", "pageSize"}}`，DataSource 见 §6.1。

```bash
curl -s "$BASE/api/datasources?pageSize=200"
```

```json
{"data":{"items":[{"id":28,"project":"BDBGAME2024/pp-game","env":"local","host":"127.0.0.1","port":3306,"user":"root","database":"pp-game","params":"","note":"","sshEnabled":false,"serverId":0,"readOnly":true,"disabled":false,"createdAt":"2026-08-19T14:49:46.195247+08:00","updatedAt":"2026-09-01T15:49:14.64111+08:00","ref":"BDBGAME2024/pp-game/local","hasPassword":true}],"total":6,"page":1,"pageSize":200}}
```

按项目挑 id：过滤 `project == "BDBGAME2024/pp-game"`，`ref` 是 `<project>/<env>` 的可读标识。

### 5.2 单条数据源

`GET /api/datasources/{id}` · owner

| 路径参数 | 类型 | 说明 |
| --- | --- | --- |
| `id` | int | 数据源 id |

**返回** `{"data": DataSource}`。**错误** 400 id 非数字；404 `not found: datasource {id}`。

### 5.3 库

`GET /api/datasources/{id}/databases` · owner

**返回** `{"data": Database[]}`（§6.2）。一条连接固定对应一个库，数组恒为一项。

```json
{"data":[{"name":"pp-game","charset":"utf8mb4","collation":"utf8mb4_unicode_ci","tables":81}]}
```

### 5.4 表清单

`GET /api/datasources/{id}/tables` · owner

| 查询参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `database` | string | 否 | 省略用连接绑定的库；给了必须等于绑定库，否则 403 |

**返回** `{"data": Table[]}`（§6.3）。

```json
{"data":[{"name":"b_channels","type":"BASE TABLE","engine":"InnoDB","rows":2},{"name":"b_chat_merchant_thresholds","type":"BASE TABLE","engine":"InnoDB","rows":2,"comment":"商户最低发言余额阈值"}]}
```

### 5.5 表结构

`GET /api/datasources/{id}/schema` · owner

| 查询参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `table` | string | 是 | 表名 |
| `database` | string | 否 | 同 5.4 |

**返回** `{"data": TableDetail}`（§6.4）。

```bash
curl -s "$BASE/api/datasources/28/schema?table=b_channels"
```

```json
{"data":{"database":"pp-game","name":"b_channels","columns":[{"name":"id","type":"bigint unsigned","nullable":false,"key":"PRI","extra":"auto_increment"},{"name":"name","type":"varchar(100)","nullable":true,"comment":"渠道名称"}],"indexes":[{"name":"PRIMARY","unique":true,"type":"BTREE","columns":["id"]}],"ddl":"CREATE TABLE `b_channels` (...)"}}
```

### 5.6 执行查询

`POST /api/datasources/{id}/query` · owner

**请求头** `Content-Type: application/json`

**请求体**

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `sql` | string | 是 | 一条或多条语句，`;` 分隔。**遇错即停**：前面的结果照常返回，出错那条带 `error`，其后不执行 |
| `database` | string | 否 | 省略用连接绑定的库；给别的库 403 |
| `maxRows` | int | 否 | 单条语句返回行数上限。缺省 **500**，硬顶 **1000**（只能调小不能调大），超出时 `truncated: true` |

约束：数据源 `readOnly: true` 时任何写语句（INSERT/UPDATE/DELETE/DDL…）整次请求 403；全部语句共用一条物理连接（事务、临时表、会话变量跨语句有效）；整次调用超时 60 s。

**返回** `{"data": ExecResult}`（§6.5）。

```bash
curl -s -X POST "$BASE/api/datasources/28/query" \
  -H 'Content-Type: application/json' \
  -d '{"sql":"SELECT 1 AS a; SELECT id FROM b_channels","maxRows":1}'
```

```json
{"data":{"database":"pp-game","results":[{"statement":"SELECT 1 AS a","kind":"query","columns":["a"],"rows":[[1]],"rowCount":1,"elapsedMs":0},{"statement":"SELECT id FROM b_channels","kind":"query","columns":["id"],"rows":[[1]],"rowCount":1,"truncated":true,"elapsedMs":0}],"elapsedMs":1}}
```

语句级错误（HTTP 仍是 200）：

```json
{"data":{"database":"pp-game","results":[{"statement":"SELECT * FROM no_such_table","kind":"query","rowCount":0,"elapsedMs":0,"error":"Error 1146 (42S02): Table 'pp-game.no_such_table' doesn't exist"}],"elapsedMs":0}}
```

**错误**

| 状态 | `error` | 触发 |
| --- | --- | --- |
| 400 | `invalid input: 没有可执行的语句` | `sql` 缺失或全是空白/注释 |
| 403 | `forbidden: 数据源 … 配置为只读，不能执行写语句（UPDATE…）…` | 只读源跑写语句 |
| 403 | `forbidden: 连接 … 只对应 pp-game 库，不能用它访问 "mysql"` | `database` 不是绑定库 |
| 404 | `not found: datasource {id}` | id 不存在 |

### 5.7 会话侧数据源清单（按目录）

`GET /api/workspace/datasources` · owner / 租户

| 查询参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `cwd` | string | 是 | **绝对路径**，用于推项目（规则见下）。租户给的路径必须在自己的 root 内，否则 404 |

**项目推导规则**（`datasource/scope.go`）：

1. `cwd` 在工作区根（`GET /api/system` 的 `workspaceDir`，当前 `/Users/luca/acpp`）之内：取相对路径与其最后一段作候选。**纯路径运算，目录不必存在**——`/Users/luca/acpp/BDBGAME2024/pp-game` 可对上项目 `BDBGAME2024/pp-game`；`/Users/luca/acpp/pp-game` 对不上（候选只有 `pp-game`）。
2. 否则取最近的 git 仓库：目录名，以及 origin 的 `<组织>/<仓库>`——`/Users/luca/work/pp-game` 因 origin 而对上。
3. 推不出项目或项目没配数据源：返回空数组 `{"data":[]}`，不是错误。
4. 直接写项目名（`BDBGAME2024/pp-game`）**不是**路径，推不出项目。

**返回** `{"data": DataSource[]}`，不分页；**含已停用**（`disabled: true`）的数据源，调用方自行过滤。

```bash
curl -s "$BASE/api/workspace/datasources?cwd=/Users/luca/acpp/BDBGAME2024/pp-game"
```

### 5.8 / 5.9 会话侧库与表

`GET /api/workspace/datasources/{dsid}/databases` · `GET /api/workspace/datasources/{dsid}/tables` · owner / 租户

| 参数 | 位置 | 必填 | 说明 |
| --- | --- | --- | --- |
| `dsid` | 路径 | 是 | 数据源 id，**必须在 5.7 返回的清单里**，否则 404 `not found: datasource {dsid}`（项目外的 id 与不存在的 id 同样对待） |
| `cwd` | 查询 | 是 | 同 5.7 |
| `database` | 查询 | 否 | 仅 tables，同 5.4 |

返回结构与 5.3 / 5.4 相同。**没有会话侧的 schema 与 query 端点。**

### 5.10 会话版

`GET /api/sessions/{id}/datasources`、`…/{dsid}/databases`、`…/{dsid}/tables` · owner / 租户

与 5.7–5.9 相同，只是 `cwd` 取自会话记录，不传查询参数。`id` 是会话 id；不属于当前身份的会话按 404。

### 5.11 MCP 工具面清单

`GET /api/tools/servers` · owner

| 查询参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `cwd` | string | 否 | 同 5.7；决定 `acpp-db` 面挂不挂、有哪些工具 |

**返回** `{"data": {"items": ToolFace[], …}}`（§6.6）。`acpp-db` 是数据库面；`acpp-server` 是服务器观察面（十二个只读工具，本文不展开）。

```json
{"data":{"items":[{"name":"acpp-db","endpoint":"/api/mcp/db/{token}","mounted":true,"sourceCount":4,"tools":[{"name":"db_sources","description":"列出当前项目可用的数据库数据源…","inputSchema":{"properties":{},"type":"object"},"annotations":{"readOnlyHint":true}}]}],"total":2,"page":1,"pageSize":2}}
```

### 5.12 工具面透传

`POST /api/tools/inspect` · owner

与 agent 回连的 `/api/mcp/db/{token}` 走**同一条协议路径**，区别只在上下文来自 `cwd` 而不是会话 token；每次调用记入 5.13 的记录，来源 `manual`。

**请求头** `Content-Type: application/json`

**请求体**

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `cwd` | string | 否 | 同 5.7 |
| `server` | string | 否 | 空或省略 = `acpp-db`；`acpp-server` 走服务器面 |
| `request` | object | 是 | **原样的 JSON-RPC 2.0 消息**（§6.7）。方法：`initialize`、`ping`、`tools/list`、`tools/call`。无 `id` 视为通知 |

**返回** `{"data": InspectResult}`（§6.8）。

```bash
curl -s -X POST "$BASE/api/tools/inspect" -H 'Content-Type: application/json' -d '{
  "cwd": "/Users/luca/acpp/BDBGAME2024/pp-game",
  "request": {"jsonrpc":"2.0","id":1,"method":"tools/call",
              "params":{"name":"db_query","arguments":{"source":"local","sql":"SELECT 1 AS ok"}}}
}'
```

```json
{"data":{"response":{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"BDBGAME2024/pp-game/local · pp-game · 1 条语句 · 0ms\n\n[1] SELECT 1 AS ok\n1 行 · 0ms\nok\n1\n"}]}},"durationMs":6,"accepted":false}}
```

通知（无 `id`）：`{"data":{"durationMs":1,"accepted":true}}`，无 `response`。

**错误**

| 层 | 形状 | 触发 |
| --- | --- | --- |
| HTTP 400 | `{"error":"invalid input: request is required"}` | 缺 `request` |
| JSON-RPC `error` | `{"code":-32700,"message":"parse error"}` | `request` 不是合法 JSON-RPC |
| JSON-RPC `error` | `{"code":-32601,"message":"method \"x\" not found"}` | 未知方法 |
| JSON-RPC `error` | `{"code":-32602,"message":"unknown tool \"nope\""}` / `bad tools/call params` | 工具名或参数错 |
| 工具级 | `result.isError: true`，文本在 `content[0].text` | 工具跑了但失败（只读源跑写语句、SQL 报错、数据源不存在） |

### 5.13 调用记录

`GET /api/tools/calls` · owner

| 查询参数 | 类型 | 说明 |
| --- | --- | --- |
| `page`、`pageSize` | | §2.4 |
| `server`、`tool` | string | 精确匹配 |
| `source` | `agent` / `manual` | 来源 |
| `errorsOnly` | `1` | 只看失败 |

**返回** `{"data": {"items": MCPCall[], …}}`；MCPCall：`id`、`server`、`tool`、`sessionId`（人工为 0）、`source`、`cwd`、`args`（≤4 KB）、`result`（≤8 KB）、`isError`、`durationMs`、`createdAt`。全表只保留最近 2000 条。

`GET /api/tools/calls/stats` · owner：`{"data":{"items":[{"server","tool","count","errorCount","avgMs","lastUsedAt"}]}}`。

### 5.14 辅助

| 方法 路径 | 权限 | 返回 |
| --- | --- | --- |
| `GET /api/health` | 公开 | `{"data":{"repo":"HuLuca1998/acpp","status":"ok","version":"1.2.3"}}` |
| `GET /api/auth/me` | 公开 | `{"data":{"authenticated":<bool>,"owner":<bool>}}`，租户还带 `tenant` 信息 |
| `POST /api/auth/redeem` | 公开 | body `{"token":"…"}`；成功种 `acpp_tenant` cookie；无效 401 `invalid invite` |
| `GET /api/system` | owner | `{"data":{"dataDir","defaultDir","workspaceDir","defaultWorkspaceDir","lanBase","lanShareable"}}`——`workspaceDir` 用于 §5.7 的路径拼接，`lanBase` 是当前局域网地址 |

---

## 6. 数据结构

### 6.1 DataSource

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | int | |
| `project` | string | 项目名，`<组织>/<仓库>` 或仓库名，`(project, env)` 唯一 |
| `env` | string | 环境名：`local` / `pre` / `prod` / `live`… |
| `host`、`port`、`user` | string、int、string | MySQL 连接 |
| `database` | string | 绑定的库，一条连接只对应一个库 |
| `params` | string | 额外 DSN 参数 |
| `note` | string | 备注 |
| `sshEnabled` | bool | 是否经 SSH 隧道 |
| `serverId` | int | 隧道跳板机 id（`/api/servers`），0 = 无 |
| `serverName` | string | 跳板机名，可选 |
| `readOnly` | bool | 只读开关，**写语句的唯一执行点** |
| `disabled` | bool | 停用后不挂给 AI，5.7 仍列出 |
| `ref` | string | `<project>/<env>`，与 MCP 工具的 `source` 完整写法一致 |
| `hasPassword` | bool | 密码永不出 API，只给有无 |
| `createdAt`、`updatedAt` | string | RFC 3339 |

### 6.2 Database

`name`、`charset`（可选）、`collation`（可选）、`system`（bool，可选）、`tables`（int）。

### 6.3 Table

`name`、`type`（`BASE TABLE` / `VIEW`）、`engine`（可选）、`rows`（int，information_schema 估算值，InnoDB 不精确）、`comment`（可选）。

### 6.4 TableDetail

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `database`、`name` | string | |
| `columns` | Column[] | `name`、`type`、`nullable`、`key`（`PRI`/`UNI`/`MUL`，可选）、`default`、`extra`、`comment`（后三者可选） |
| `indexes` | Index[] | `name`、`unique`、`type`（可选）、`columns`（按索引内顺序） |
| `ddl` | string | `SHOW CREATE TABLE` 原文，可选 |

### 6.5 ExecResult / StatementResult

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `database` | string | 实际执行的库 |
| `results` | StatementResult[] | 按语句顺序，一条一项 |
| `elapsedMs` | int | 整次耗时 |

StatementResult：

| 字段 | 类型 | 出现条件 | 说明 |
| --- | --- | --- | --- |
| `statement` | string | 总是 | 语句原文 |
| `kind` | `query` / `exec` | 总是 | 有结果集 / 只有影响行数 |
| `columns` | string[] | query | 列名 |
| `rows` | any[][] | query 且有行 | 行数组，值编码见 §2.5 |
| `rowCount` | int | 总是 | 返回的行数（截断后） |
| `truncated` | bool | 超过 `maxRows` 时 | |
| `affected`、`lastInsertId` | int | exec | 本文只读场景不会出现 |
| `elapsedMs` | int | 总是 | |
| `error` | string | 该条失败时 | MySQL 原始错误，其后语句不执行 |

### 6.6 ToolFace / Declaration

ToolFace：`name`、`endpoint`（agent 回连地址形状）、`mounted`（这个上下文下会不会真的挂给 agent）、`sourceCount`、`tools`（Declaration[]）。

Declaration：`name`、`description`（给模型看的原文）、`inputSchema`（JSON Schema object）、`annotations`（`readOnlyHint` / `destructiveHint`，可选）。

### 6.7 JSON-RPC 2.0 消息

请求：`{"jsonrpc":"2.0","id":<number|string>,"method":"<m>","params":{...}}`，无 `id` 为通知。
响应：`{"jsonrpc":"2.0","id":<同请求>,"result":{...}}` 或 `{"jsonrpc":"2.0","id":…,"error":{"code":<int>,"message":"<text>"}}`。
`tools/call` 的 `params`：`{"name":"<工具名>","arguments":{...}}`；其 `result`：`{"content":[{"type":"text","text":"…"}],"isError":<bool，可选>}`。

### 6.8 InspectResult

`response`（JSON-RPC 响应对象，通知时省略）、`durationMs`（int）、`accepted`（bool，true 表示是通知、协议上没有响应）。

---

## 7. MCP 数据库工具面（`tools/call` 可用工具）

### 7.1 工具

| 工具 | 参数 | 只读 | 返回文本 |
| --- | --- | --- | --- |
| `db_sources` | 无 | ✅ | 本项目可用数据源表 |
| `db_tables` | `source` | ✅ | 该库的表清单 |
| `db_schema` | `source`、`table`（必填） | ✅ | 列、索引、建表语句 |
| `db_query` | `source`、`sql`（必填，可多条） | ✅ | 每条语句的表头与数据；写语句被拒 |
| `db_execute` | `source`、`sql` | ❌ | **只在项目存在可写数据源时才出现在 `tools/list`**，本文不涉及 |

`source`：环境名（`local`）或完整标识（`BDBGAME2024/pp-game/local`，即 DataSource.`ref`）；项目只有一条数据源时可省略。**只看得见 `cwd` 所属项目的数据源**，这是硬隔离不是约定。

### 7.2 返回文本格式

`content[0].text` 为制表符分隔的表格，前端按同一约定解析回结构化；要省事直接用 §5.6 拿 JSON。

`db_query`：

```
<数据源 ref> · <库> · <n> 条语句 · <总耗时>ms
                                   （空行）
[1] <SQL>
<行数> 行 · <耗时>ms
<列1>\t<列2>...
<值1>\t<值2>...
                                   （每条语句一段，空行分隔）
```

`db_sources`：首行 `当前项目可用数据源 N 个：`，随后表头 `数据源\t环境\t地址\t默认库\tSSH\t备注`。
`db_tables`：首行 `<ref> 的 <库> 库共 N 张表：`，表头 `表\t类型\t引擎\t行数(估算)\t注释`。
`db_schema`：首行 `<ref> · <库>.<表>`，表头 `列\t类型\t可空\t键\t默认值\t额外\t注释`，随后索引段与 DDL。

### 7.3 工具级错误样例

```json
{"result":{"content":[{"type":"text","text":"forbidden: 数据源 BDBGAME2024/pp-game/local 配置为只读，不能执行写语句（UPDATE…）。确实要改数据的话，去数据库页把这条连接的「只读」关掉"}],"isError":true}}
```

没有数据源时 `db_sources` 返回文本 `当前项目没有配置数据源。（数据源按项目隔离，需要在面板的「数据库」页里为本项目添加连接。）`，`isError` 为 false。

### 7.4 agent 回连端点（了解即可）

`POST /api/mcp/db/{token}` 是 claude / codex 子进程连的 MCP 端点（streamable-http 的无流子集），方法集与 5.12 相同。`{token}` 是每会话专属的随机凭证：会话首次挂载工具面时生成，存 `sessions.mcp_token`，**不出任何 API**；discord 子区另用内存凭证（`peer_` 前缀，进程重启作废）。人工调用请走 5.12，无效 token 得 JSON-RPC `-32000 unknown mcp endpoint`。

---

## 8. 完整调用序列

以 SSH 隧道后的 `BASE=http://127.0.0.1:48090` 为例，读 pp-game 本地库：

```bash
BASE=http://127.0.0.1:48090
# 1. 身份确认
curl -s "$BASE/api/auth/me"                       # {"data":{"authenticated":true,"owner":true}}
# 2. 按项目挑数据源 id
curl -s "$BASE/api/datasources?pageSize=200" \
  | python3 -c 'import sys,json; [print(i["id"], i["ref"], i["readOnly"]) for i in json.load(sys.stdin)["data"]["items"] if i["project"]=="BDBGAME2024/pp-game"]'
# 3. 表清单与表结构
curl -s "$BASE/api/datasources/28/tables"
curl -s "$BASE/api/datasources/28/schema?table=b_channels"
# 4. 查询
curl -s -X POST "$BASE/api/datasources/28/query" -H 'Content-Type: application/json' \
  -d '{"sql":"SELECT id, name, type FROM b_channels LIMIT 20","maxRows":20}'
```

走 MCP 工具面的等价序列（不需要 id，需要 `cwd`）：

```bash
CWD=/Users/luca/acpp/BDBGAME2024/pp-game
call() { curl -s -X POST "$BASE/api/tools/inspect" -H 'Content-Type: application/json' \
  -d "{\"cwd\":\"$CWD\",\"request\":$1}"; echo; }
call '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
call '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"db_sources","arguments":{}}}'
call '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"db_tables","arguments":{"source":"local"}}}'
call '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"db_schema","arguments":{"source":"local","table":"b_channels"}}}'
call '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"db_query","arguments":{"source":"local","sql":"SELECT id, name FROM b_channels LIMIT 20"}}}'
```
