# ADR-021：租户只读数据库 HTTP 面

日期：2026-09-08　状态：已采纳（扩展 [adr-010](adr-010-租户会话能力与-owner-对齐.md)，不推翻）

## 背景

用户要让局域网里的程序凭租户 token 直接经 HTTP 读库：列数据源、发查询拿数据。现有形状做不到这件事——

- 能发 SQL 的端点（`/api/datasources/{id}/query`、`/api/tools/inspect`）整个前缀是 owner 专属，而 owner 只认本机回环来源（adr-007），局域网请求实测无凭证 401、带租户 cookie 403。
- 租户能碰的只有会话侧清单（`/api/workspace/datasources*`），它按 `?cwd=` 推项目（adr-010），脚本没有「所在项目」，实测拼项目名对不上、拼租户根下的路径也对不上；且那一面根本没有查询端点。
- 凭证只认 cookie（SSE 与 WebSocket 带不了自定义头），脚本要先种 cookie，别扭。

## 决策

1. **新开只读数据库面 `/api/db`**，两个端点：`GET /api/db/sources`（全部启用数据源，可 `?project=` 过滤，不含密码）与 `POST /api/db/query`（`{source, sql, maxRows}`）。租户与 owner 都能用，不在 owner 专属前缀内。
2. **按数据源标识寻址，不经工作目录**：`source` 接受 `<项目>/<环境>`、环境名（全局唯一时）、数据源 id；只有一条启用数据源时可省略。解析复用 MCP 工具面的 `datasource.Resolve`，两边写法一致。
3. **永远只读**：`Execute` 的 `allowWrite` 固定为 false，与 AI 的 `db_query` 同一条护栏——只读源上的写语句 403，可写源上的写语句也只是 400「查询通道」。要改数据走界面或 AI 的执行工具。
4. **凭证多认一种载体**：`Authorization: Bearer <租户 token>`，与 cookie 是同一枚 token、同一套判定（含「带了凭证即按租户算」的反向代理防线）。不引入 API key 之类的第二种身份。

## 理由

- 与 adr-010 同一哲学：租户会话内的数据库能力本就与 owner 对齐、真正的权限交给数据库账号管。脚本面把「按 cwd 推项目」换成「按标识寻址」，只是去掉一个对脚本没有意义的间接层，可见范围（全部启用数据源）与租户在会话里能 `@` 到的并无本质差别——租户克隆任何仓库就能让 AI 挂上对应的库。
- 只读是这一面的边界而不是可选项：租户 token 靠邀请链接分发，远没有生产库写权限该有的郑重；owner 要写有管理面与 AI 的 `db_execute`。
- Bearer 与 cookie 同源同判定，是为了不新增一种要单独管理、单独轮换、单独失效的凭证。

## 后果

- 持租户 token 的任何程序都能读到全部启用数据源的数据（受数据库账号权限约束）。owner 发邀请时即视为信任其读库；要收窄只能靠数据库账号或停用数据源。
- 前缀 `/api/db` 与 owner 专属的 `/api/datasources`、`/api/discord` 不重叠，`isOwnerOnly` 无需改动。
- 回归保护：`httpapi/dbapi_test.go`（租户经 Bearer 从局域网地址列清单与查询、停用源不出现、写语句被拒、id 与 ref 同源、管理面仍 403）、`auth_test.go` 的 `TestAuth_BearerIsTenant`（Bearer 即租户、回环 + Bearer 降为租户、无效 Bearer 401）。
- 已知的相邻问题（不在本决策内）：`POST /api/datasources` 创建时 `readOnly:false` 会被 GORM 的 `default:true` 吞掉，新连接总是只读，需要再 PUT 一次才能改成可写。
