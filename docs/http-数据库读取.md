# 用 HTTP 直接读数据库

不开页面、不起 agent，用 curl 或脚本直接查 acpp 里配好的 MySQL 数据源。
本文只讲**读**；写语句由数据源的「只读」开关统一拦住（见 [README §数据库](../README.md#数据库)），本文不放开它。

## 结论先看

| 路 | 端点 | 谁能调 | 适合 |
| --- | --- | --- | --- |
| A 工具台 | `POST /api/tools/inspect` | owner（本机回环、不带 cookie） | **脚本 / 人工**。发 MCP 的 JSON-RPC，与 AI 走同一条协议路径，返回模型看到的那段文本 |
| B REST | `POST /api/datasources/{id}/query` | owner | 要**结构化 JSON**（列名 + 行数组）时 |
| C agent 回连 | `POST /api/mcp/db/{token}` | 持有会话 token 的 agent 子进程 | **不给人用**：token 是每会话凭证，不出任何 API |

**租户 token 不行。** `/api/tools` 与 `/api/datasources` 整个前缀是 owner 专属（[auth.go 的 isOwnerOnly](../server/internal/httpapi/auth.go)），带租户 cookie 会得 `403 owner only`。租户能做的只有**列清单**（数据源 / 库 / 表），没有任何能发 SQL 的端点，详见 [§租户能做什么](#租户能做什么)。

## 地址与身份

- dev 实例 `http://127.0.0.1:48080`，桌面 app 实例 `http://127.0.0.1:48090`，API 一模一样。
- **owner 判定 = 请求来自回环地址且不带 `acpp_tenant` cookie**，零配置。所以本机 curl 天然就是 owner。
- 从别的机器调：SSH 端口转发到本机再打 `127.0.0.1`，不要给后端开外网口。
- 不要绕 vite（45173）：代理会把来源改写成回环，鉴权语义会失真。

## 路 A：工具台端点（推荐）

`POST /api/tools/inspect`，body 三个字段：

```json
{
  "cwd": "/Users/luca/work/pp-game",
  "server": "",
  "request": { "jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": { "name": "db_query", "arguments": { "source": "local", "sql": "SELECT 1" } } }
}
```

- `cwd`：**项目由工作目录推出**（工作区根下的相对路径、git 仓库名或 origin 的 `<组织>/<仓库>`，见 [scope.go](../server/internal/datasource/scope.go)），只看得见该项目的数据源。推不出项目就一条都没有，`tools/list` 仍会返回工具，但 `db_sources` 会说没有可用数据源。
- **没有真实目录时怎么填 `cwd`**：它是路径不是项目名，直接写 `BDBGAME2024/pp-game` 或 `pp-game` 推不出项目（实测得「当前项目没有配置数据源」）。两个办法：
  - 写 `<工作区根>/<项目名>`，如 `/Users/luca/acpp/BDBGAME2024/pp-game`。工作区根取 `GET /api/system` 的 `workspaceDir`；根下的推导是纯路径运算，**目录不存在也能对上**（实测）。注意必须写数据源上配的那个完整项目名，`/Users/luca/acpp/pp-game` 对不上 `BDBGAME2024/pp-game`。这是实现的副产物不是契约，脚本里用要有心理准备。
  - 干脆不要 `cwd`：走 [路 B](#路-brest-查询要结构化结果时) 按数据源 id 查，`GET /api/datasources` 返回的每条都带 `project` 与 `ref`，按 `project` 挑出 id 即可。只想拿数据的脚本推荐这条。
- `server`：留空或省略 = 数据库面 `acpp-db`；填 `acpp-server` 走服务器观察面（十二个只读工具，本文不展开）。
- `request`：**原样的 JSON-RPC 消息**。支持 `initialize` / `ping` / `tools/list` / `tools/call` 四个方法；带 `id` 才有响应，通知类消息返回 `accepted: true` 且没有 `response`。

响应外壳：

```json
{ "data": { "response": { "jsonrpc": "2.0", "id": 1, "result": { ... } }, "durationMs": 6, "accepted": false } }
```

每次调用都会记进工具台的「调用记录」，来源标 `manual`。

### 四个只读工具

| 工具 | 参数 | 返回 |
| --- | --- | --- |
| `db_sources` | 无 | 本项目可用数据源清单 |
| `db_tables` | `source` | 该数据源固定库的表清单（表名、引擎、估算行数、注释） |
| `db_schema` | `source`、`table`（必填） | 列、索引、建表语句 |
| `db_query` | `source`、`sql`（必填，可多条） | 每条语句的表头与数据 |

`source` 填环境名即可（`local` / `pre` / `prod`），要写全就用 `db_sources` 里的完整标识（如 `BDBGAME2024/pp-game/pre`）。项目只有一条数据源时可省略。每条数据源固定对应一个库，不能也不用另指定库。

`db_execute` 只在项目里**存在可写数据源**时才出现在 `tools/list` 里；全是只读连接时模型和你都看不到它。

### 一次完整的读

```bash
B=http://127.0.0.1:48080; CWD=/Users/luca/work/pp-game
call() { curl -s -X POST "$B/api/tools/inspect" -H 'Content-Type: application/json' \
  -d "{\"cwd\":\"$CWD\",\"request\":$1}"; echo; }

# 1. 有哪些工具
call '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
# 2. 有哪些数据源
call '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"db_sources","arguments":{}}}'
# 3. 表清单
call '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"db_tables","arguments":{"source":"local"}}}'
# 4. 表结构
call '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"db_schema","arguments":{"source":"local","table":"b_channels"}}}'
# 5. 查询
call '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"db_query","arguments":{"source":"local","sql":"SELECT 1 AS ok, NOW() AS now"}}}'
```

第 5 步的真实响应（2026-09-08 对 dev 实例实测）：

```json
{"data":{"response":{"jsonrpc":"2.0","id":5,"result":{"content":[{"type":"text","text":"BDBGAME2024/pp-game/local · pp-game · 1 条语句 · 0ms\n\n[1] SELECT 1 AS ok, NOW() AS now\n1 行 · 0ms\nok\tnow\n1\t2026-09-08 04:05:23\n"}]}},"durationMs":6,"accepted":false}}
```

`content[0].text` 是**制表符分隔的表格**：首行是数据源与语句数，每条语句一段（`[n] SQL` / `行数 · 耗时` / 表头行 / 数据行）。前端就是按这个约定解析回表格的，脚本可以照做；要省事直接走路 B 拿 JSON。

### 错误长什么样

- **工具级错误**（跑了但失败）：`result.isError: true`，文本在 `content[0].text`。写语句打到只读源就是这种：

  ```json
  {"result":{"content":[{"type":"text","text":"forbidden: 数据源 BDBGAME2024/pp-game/local 配置为只读，不能执行写语句（UPDATE…）。确实要改数据的话，去数据库页把这条连接的「只读」关掉"}],"isError":true}}
  ```

- **协议错误**（请求没被受理）：`error.code`。`-32700` 解析失败、`-32601` 方法不存在、`-32602` 参数错或工具名不存在（`unknown tool "nope"`）、`-32000` 端点 token 无效（只在路 C 出现）。
- **HTTP 层**：`401 invite required`（不是回环又没 cookie）、`403 owner only`（租户 cookie 打 owner 专属前缀）、`400` 缺 `request`。

## 路 B：REST 查询（要结构化结果时）

同样 owner 专属，按数据源 id 打：

```bash
B=http://127.0.0.1:48080
curl -s "$B/api/datasources"                                   # 列全部数据源，拿 id
curl -s "$B/api/datasources/28/databases"
curl -s "$B/api/datasources/28/tables?database=pp-game"
curl -s "$B/api/datasources/28/schema?database=pp-game&table=b_channels"
curl -s -X POST "$B/api/datasources/28/query" -H 'Content-Type: application/json' \
  -d '{"sql":"SELECT 1 AS ok","maxRows":50}'
```

查询 body：`sql`（必填，可多条）、`database`（省略用数据源默认库）、`maxRows`（省略走服务端默认）。响应：

```json
{"data":{"database":"acpp_demo","results":[{"statement":"SELECT 1 AS ok","kind":"query","columns":["ok"],"rows":[[1]],"rowCount":1,"elapsedMs":0}],"elapsedMs":0}}
```

`results` 按语句一条一项；`kind` 为 `query` 时有 `columns` / `rows`。这条路**不需要 `cwd`、不按项目过滤**（管理面，owner 看得见全部），也不进工具台的调用记录。按项目挑 id：

```bash
curl -s "$B/api/datasources" | python3 -c 'import sys,json; [print(i["id"], i["ref"]) for i in json.load(sys.stdin)["data"]["items"] if i["project"]=="BDBGAME2024/pp-game"]'
```

## 路 C：agent 回连端点（了解即可）

`POST /api/mcp/db/{token}` 是 claude / codex 子进程连的那个 streamable-http MCP 端点，方法与路 A 完全相同（同一份 `mcp.Server` 外壳）。它是公开路径，但 `{token}` 是**每会话专属的 24 字节随机凭证**：会话首次挂载工具面时懒生成，落在 `sessions.mcp_token`，`json:"-"` 永不出 API；discord 子区用的是另一套内存凭证（`peer_` 前缀，进程重启作废）。

人工调用没有理由走这条路：路 A 就是它的 owner 版本，区别只在上下文来源（路 A 直接给 `cwd`，路 C 由 token 反查会话的 cwd）。

## 租户能做什么

| 端点 | 租户 |
| --- | --- |
| `GET /api/workspace/datasources?cwd=…` | ✅ 列当前项目数据源（不含凭证） |
| `GET /api/workspace/datasources/{dsid}/databases`、`…/tables?database=` | ✅ |
| `GET /api/sessions/{id}/datasources`、`…/{dsid}/databases`、`…/{dsid}/tables` | ✅ 同上，按会话 cwd 过滤 |
| `POST /api/tools/inspect`、`GET /api/tools/*` | ❌ 403 owner only |
| `/api/datasources/*`（含 `query`） | ❌ 403 owner only |
| `POST /api/mcp/db/{token}` | ❌ 拿不到 token |

租户凭证的用法：`POST /api/auth/redeem` body `{"token":"<邀请 token>"}` 会种 `acpp_tenant` cookie；脚本直接 `-H 'Cookie: acpp_tenant=<token>'` 也行。但它**只能列不能查**：租户在会话里让 AI 用 `db_*` 工具是唯一的查询通道（adr-010 把能力面对齐到 owner，靠的是 MCP 工具面而不是 REST）。

要让租户或外部系统直接发 SQL，得新增能力（比如租户级只读查询端点，或独立的 API key），这是产品决策，本文不替它拍板。
