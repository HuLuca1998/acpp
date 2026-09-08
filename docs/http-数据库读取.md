# acpp 数据库读取 HTTP 接口文档

| 项 | 值 |
| --- | --- |
| 文档版本 | 2026-09-08 v2（接口设计见 [adr-021](adr-021-租户只读数据库-http-面.md)） |
| 适用服务 | acpp 后端 ≥ 1.2.4（`GET /api/health` 的 `version`） |
| 协议 | HTTP/1.1，请求与响应均为 `application/json; charset=utf-8` |
| 范围 | 局域网内持**租户 token** 的程序：列数据源、执行只读查询取数据。**没有写操作** |

两步就能拿到数据：`GET /api/db/sources` 拿数据源标识，`POST /api/db/query` 带标识与 SQL。样本全部对真实实例实测。

---

## 1. 基础地址

| 实例 | 基础地址（BASE） | 说明 |
| --- | --- | --- |
| 桌面 app（局域网） | `http://192.168.2.16:48090` | 菜单栏「允许局域网访问」已开。局域网 IP 由 DHCP 分配，**以邀请链接里的主机为准**（owner 可在 `GET /api/system` 的 `lanBase` 查） |
| 本机 dev | `http://127.0.0.1:48080` | 开发实例，仅本机可达 |

下文路径都相对 BASE。

---

## 2. 鉴权

### 2.1 凭证

凭证是**租户 token**：owner 在「访客管理」页给你的邀请链接 `http://<主机>:<端口>/?invite=<token>` 里的 `invite` 参数（owner 侧对应 `GET /api/tenants` 的 `inviteUrl`）。token 长期有效，owner 停用或轮换后即失效。

### 2.2 携带方式（二选一）

| 方式 | 请求头 | 适用 |
| --- | --- | --- |
| Bearer（推荐） | `Authorization: Bearer <token>` | 脚本、外部程序 |
| Cookie | `Cookie: acpp_tenant=<token>` | 浏览器、或先经 `POST /api/auth/redeem` 兑换的客户端 |

两者是同一枚 token、同一种身份。同时带以 cookie 为准。

### 2.3 权限

| 情形 | 结果 |
| --- | --- |
| 有效 token，任何来源地址 | 租户身份，可用本文全部接口（只读） |
| 无 token 或 token 无效 | `401 {"error":"unauthorized: invite required"}` |
| token 已被 owner 停用 | `403 {"error":"forbidden: access disabled by owner"}` |
| 带 token 访问 owner 专属前缀（如 `/api/datasources`） | `403 {"error":"forbidden: owner only"}` |
| 本机回环且不带 token | owner 身份，同样可用本文接口 |

---

## 3. 通用约定

### 3.1 请求头

| 头 | 必填 | 值 |
| --- | --- | --- |
| `Authorization` | 是（或 Cookie） | `Bearer <token>` |
| `Content-Type` | POST 必填 | `application/json` |

### 3.2 响应外壳

| 情况 | 形状 |
| --- | --- |
| 成功 | `{"data": ...}` |
| 失败 | `{"error": "<前缀>: <说明>"}`，前缀与状态码对应 |

`data` 内字段一律 camelCase；缺省的可选字段**省略**而不是 `null`。

### 3.3 状态码

| 状态码 | `error` 前缀 | 含义 |
| --- | --- | --- |
| 200 | — | 成功。**SQL 语句级失败也是 200**，错误在 `results[i].error` |
| 400 | `invalid input` | 请求体不合法、`source` 匹配到多条、可写源上跑写语句、`sql` 为空、出现未知字段 |
| 401 | `unauthorized` | 无凭证或凭证无效 |
| 403 | `forbidden` | 凭证被停用；只读源上跑写语句 |
| 404 | `not found` | `source` 没有匹配到任何启用的数据源 |
| 500 | 其它 | 连库失败等服务端错误 |

### 3.4 值的编码

| 类型 | JSON 形态 | 示例 |
| --- | --- | --- |
| 记录时间戳 | RFC 3339 带时区 | `"2026-09-01T15:03:42.747619+08:00"` |
| `rows` 里的整数、布尔 | number | `1` |
| `rows` 里的浮点、DECIMAL、字符串、日期时间 | string | `"1.5"`、`"2026-09-08 04:58:29"` |
| `rows` 里的 NULL | `null` | |

`rows` 的值由 MySQL 驱动按文本协议返回，**除整数与布尔外一律是字符串**，调用方自行转型。

---

## 4. 接口

### 4.1 数据源清单

`GET /api/db/sources`

**请求头** `Authorization: Bearer <token>`

**查询参数**

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `project` | string | 否 | 只要这个项目的，不区分大小写，如 `BDBGAME2024/pp-game` |

**返回** `{"data": DataSource[]}`（§5.1），按项目、环境排序，只含启用的数据源，不含密码，不分页。

```bash
curl -s "$BASE/api/db/sources?project=BDBGAME2024/pp-game" \
  -H "Authorization: Bearer $TOKEN"
```

```json
{"data":[
  {"id":33,"project":"BDBGAME2024/pp-game","env":"live","host":"127.0.0.1","port":3306,"user":"readonly","database":"pp_game","params":"","note":"","sshEnabled":true,"serverId":4,"readOnly":true,"disabled":false,"createdAt":"2026-09-01T15:03:42.747619+08:00","updatedAt":"2026-09-01T15:49:14.641773+08:00","ref":"BDBGAME2024/pp-game/live","hasPassword":true,"serverName":"dmit-dev"},
  {"id":28,"project":"BDBGAME2024/pp-game","env":"local","host":"127.0.0.1","port":3306,"user":"root","database":"pp-game","params":"","note":"","sshEnabled":false,"serverId":0,"readOnly":true,"disabled":false,"createdAt":"2026-08-19T14:49:46.195247+08:00","updatedAt":"2026-09-01T15:49:14.64111+08:00","ref":"BDBGAME2024/pp-game/local","hasPassword":true}
]}
```

下一步要用的是 `ref`（或 `id`）。

### 4.2 执行查询

`POST /api/db/query`

**请求头** `Authorization: Bearer <token>`、`Content-Type: application/json`

**请求体**

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `source` | string | 视情况 | 数据源标识，三种写法：`ref`（`<项目>/<环境>`，如 `BDBGAME2024/pp-game/local`）、环境名（如 `prod`，仅当全局唯一）、数据源 `id`（如 `28`）。**只有一条启用数据源时可省略** |
| `sql` | string | 是 | 一条或多条语句，`;` 分隔。**遇错即停**：前面的结果照常返回，出错那条带 `error`，其后不执行 |
| `maxRows` | int | 否 | 单条语句返回行数上限。缺省 **500**，硬顶 **1000**（只能调小），超出时 `truncated: true` |

不接受其它字段（有未知字段整个请求 400）。整次调用超时 60 s；全部语句共用一条连接。

**约束**：这是只读通道。只读数据源上的写语句 403；可写数据源上的写语句 400（`这是查询通道，只能跑 SELECT/SHOW 一类语句`）。

**返回** `{"data": QueryResult}`（§5.2）。

```bash
curl -s -X POST "$BASE/api/db/query" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"source":"BDBGAME2024/pp-game/local","sql":"SELECT id, name, type FROM b_channels LIMIT 5","maxRows":5}'
```

```json
{"data":{"source":"BDBGAME2024/pp-game/local","database":"pp-game","results":[{"statement":"SELECT id, name, type FROM b_channels LIMIT 5","kind":"query","columns":["id","name","type"],"rows":[[1,"bang-账号","bc.game"],[2,"luca-账号","bc.game"]],"rowCount":2,"elapsedMs":0}],"elapsedMs":1}}
```

语句级错误（HTTP 仍是 200）：

```json
{"data":{"source":"BDBGAME2024/pp-game/local","database":"pp-game","results":[{"statement":"SELECT * FROM no_such","kind":"query","rowCount":0,"elapsedMs":0,"error":"Error 1146 (42S02): Table 'pp-game.no_such' doesn't exist"}],"elapsedMs":0}}
```

**错误**（均为实测）

| 状态 | `error` | 触发 |
| --- | --- | --- |
| 400 | `invalid input: "local" 匹配到多个数据源：BDBGAME2024/pp-game/local、acpp-demo/local` | 环境名在多个项目里都有，改用 ref |
| 400 | `invalid input: 有多个数据源，请指定其中之一：…` | 省略 `source` 但启用的数据源不止一条 |
| 400 | `invalid input: 没有可执行的语句` | `sql` 为空 |
| 400 | `invalid input: 这是查询通道，只能跑 SELECT/SHOW 一类语句（收到 DELETE…）…` | 可写源上的写语句 |
| 403 | `forbidden: 数据源 BDBGAME2024/pp-game/local 配置为只读，不能执行写语句（UPDATE…）…` | 只读源上的写语句 |
| 404 | `not found: 没有叫 "nope" 的数据源，可用的是：…` | `source` 无匹配（错误里列出全部可用 ref） |
| 404 | `not found: 没有 id 为 999 的启用数据源` | 数字 id 无匹配 |

---

## 5. 数据结构

### 5.1 DataSource

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | int | 可作 `source` |
| `ref` | string | `<project>/<env>`，**推荐作 `source`** |
| `project` | string | 项目名，`<组织>/<仓库>` 或仓库名 |
| `env` | string | 环境名：`local` / `pre` / `prod` / `live`… |
| `database` | string | 绑定的库，一条连接只对应一个库，查询时不用也不能另指定 |
| `host`、`port`、`user` | string、int、string | MySQL 连接（无密码，只有 `hasPassword`） |
| `readOnly` | bool | 数据源的只读开关；本接口无论真假都只读 |
| `disabled` | bool | 本接口只返回 `false` 的 |
| `sshEnabled`、`serverId`、`serverName` | bool、int、string | 是否经 SSH 隧道及跳板机 |
| `params`、`note` | string | DSN 参数、备注 |
| `createdAt`、`updatedAt` | string | RFC 3339 |

### 5.2 QueryResult

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `source` | string | 实际落到的数据源 `ref`（给的是环境名或 id 时据此确认） |
| `database` | string | 实际执行的库 |
| `results` | StatementResult[] | 按语句顺序一条一项 |
| `elapsedMs` | int | 整次耗时 |

StatementResult：

| 字段 | 类型 | 出现条件 | 说明 |
| --- | --- | --- | --- |
| `statement` | string | 总是 | 语句原文 |
| `kind` | `query` / `exec` | 总是 | 只读通道恒为 `query` |
| `columns` | string[] | 有结果集 | 列名 |
| `rows` | any[][] | 有行 | 行数组，值编码见 §3.4 |
| `rowCount` | int | 总是 | 返回的行数（截断后） |
| `truncated` | bool | 超过 `maxRows` 时 | |
| `elapsedMs` | int | 总是 | |
| `error` | string | 该条失败时 | MySQL 原始错误，其后语句不执行 |

---

## 6. 完整示例

curl：

```bash
BASE=http://192.168.2.16:48090
TOKEN=<邀请链接里的 invite 参数>
curl -s "$BASE/api/auth/me" -H "Authorization: Bearer $TOKEN"
# {"data":{"authenticated":true,"owner":false,"tenantName":"...","root":"..."}}
curl -s "$BASE/api/db/sources" -H "Authorization: Bearer $TOKEN"
curl -s -X POST "$BASE/api/db/query" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"source":"BDBGAME2024/pp-game/prod","sql":"SELECT COUNT(*) AS n FROM b_channels"}'
```

Python：

```python
import requests

BASE, TOKEN = "http://192.168.2.16:48090", "<invite token>"
H = {"Authorization": f"Bearer {TOKEN}"}

sources = requests.get(f"{BASE}/api/db/sources", headers=H, params={"project": "BDBGAME2024/pp-game"}).json()["data"]
ref = next(s["ref"] for s in sources if s["env"] == "prod")

r = requests.post(f"{BASE}/api/db/query", headers=H,
                  json={"source": ref, "sql": "SELECT id, name FROM b_channels LIMIT 100", "maxRows": 100})
r.raise_for_status()
for stmt in r.json()["data"]["results"]:
    if stmt.get("error"):
        raise RuntimeError(stmt["error"])
    rows = [dict(zip(stmt["columns"], row)) for row in stmt.get("rows", [])]
    print(stmt["statement"], len(rows), rows[:3])
```

---

## 附录 A：owner 专属接口（仅本机回环）

以下接口只有 owner 能用（本机回环、不带任何凭证；从局域网无法调用），列在这里便于对照。响应外壳与状态码同 §3；DataSource / StatementResult 同 §5。

| 方法 路径 | 参数 | 返回 |
| --- | --- | --- |
| `GET /api/datasources` | `page`、`pageSize`（≤200）、`sort`（`project`/`env`/`database`/`host`/`read_only`/`updated_at`）、`order` | `{items: DataSource[], total, page, pageSize}`，含停用的 |
| `GET /api/datasources/{id}` | — | DataSource |
| `GET /api/datasources/{id}/databases` | — | `[{name, charset, collation, tables}]`，恒一项 |
| `GET /api/datasources/{id}/tables` | `database`（可选，须等于绑定库） | `[{name, type, engine, rows, comment}]` |
| `GET /api/datasources/{id}/schema` | `table`（必填）、`database` | `{database, name, columns[{name,type,nullable,key,default,extra,comment}], indexes[{name,unique,type,columns}], ddl}` |
| `POST /api/datasources/{id}/query` | body `{sql, database, maxRows}` | `{database, results, elapsedMs}`；按数据源的只读开关放行写语句（可写源能写） |
| `GET /api/tools/servers` | `cwd` | MCP 工具面清单（`acpp-db` / `acpp-server`）与工具声明 |
| `POST /api/tools/inspect` | body `{cwd, server, request}`，`request` 为原样 JSON-RPC（`initialize` / `ping` / `tools/list` / `tools/call`） | `{response, durationMs, accepted}`；工具返回制表符表格文本，与 AI 看到的一致 |
| `GET /api/system` | — | `{dataDir, workspaceDir, lanBase, lanShareable, …}` |
| `GET /api/tenants` | — | 租户清单，每条带 `inviteUrl` |

工具台那条路按 `cwd` 推项目：`cwd` 必须是绝对路径，工作区根（`workspaceDir`）下的相对路径或 git 仓库的 origin 决定项目；直接写项目名推不出来。脚本没有必要走它——`/api/db` 就是为脚本设计的。
