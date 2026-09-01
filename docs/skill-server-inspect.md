# skill 范本：server-inspect

这是「服务器观察」（[adr-019](adr-019-服务器观察能力.md)）配套的技能手册。
本轮**不做自动预置**——技能库现在是用户自管的（`<dataDir>/skills/`），
随软件分发的内置技能要先想清楚升级时怎么对待用户的修改，而用户数据
（技能 / 设置）打算另起一个仓库做多设备统一源，那件事定了再一并解决。

在此之前，把下面 `---` 之间的内容整个存成
`<dataDir>/skills/server-inspect/SKILL.md`（默认 `~/.acpp/skills/…`），
再去「技能」页把它启用。

---

```markdown
---
name: server-inspect
description: 看线上服务器实际状态的操作手册。触发场景：服务是不是挂了 / 发布上没上 / 线上报什么错 / 日志里有什么 / 容器起来没 / 磁盘满没满 / 内存 CPU 怎么样 / 某个功能线上不好使要查原因，或对话里出现 server_ls、docker_ps 等 mcp__acpp-server 工具。核心方法：远程路径从项目代码推断，不靠猜。
---

# server-inspect

## 铁律：先读代码，再连服务器

远程有什么、在哪、叫什么，**事实源是项目代码**，不是猜测也不是记忆。
连上去乱翻目录既慢又容易翻错机器。

进服务器之前，先在代码里读出这几样：

| 要什么 | 去哪读 |
| --- | --- |
| 远程根目录、容器名、端口、挂载 | `docker-compose*.y*ml`、`deploy/`、`Dockerfile` |
| 部署流程、产物落在哪 | `Makefile`、`scripts/deploy*`、`.github/workflows/` |
| 日志目录、级别、留存天数 | 项目的日志配置（Go 的 zap `director`/`retention-day`、Node 的 winston transport） |
| 健康检查端点 | compose 的 `healthcheck`、路由定义 |

读不到就承认读不到，然后用 `docker_ps` + `docker_inspect` 反查——容器的
compose label 里带着远程项目根目录，那是代码与服务器之间最硬的一条线索。

## 工具面

`mcp__acpp-server__*`，全部只读：

- `server_hosts` — 有哪些机器。备注里通常写着这台跑的是什么、项目在哪
- `server_info` — 系统 / 负载 / 内存 / 磁盘一次拿全
- `server_ls` — 列目录（`sort: mtime` 找最近在写的、`sort: size` 找占地方的）
- `server_read` — 读文件，**看日志优先 `tail`**
- `server_grep` — 搜内容，必须给 `include` 与 `maxMatches`
- `docker_ps` — 容器状态与**重启次数**
- `docker_logs` — 容器 stdout，`since` 限时间窗比拉大 `tail` 更省

## 按症状的路径

**服务没响应 / 报 502**
1. `docker_ps`（带 `filter` 只看这个项目的那组）——先看**重启次数**：在涨
   说明它起不来又被反复拉起，那比日志里任何一行都先说明问题
2. `docker_logs --tail`，容器刚重启过就加 `since` 看重启前那一段
3. 容器是好的 → 问题在入口层（nginx / 网关），去读它的 error 日志

**要定位一个报错**
1. 先 `server_ls` 看日志目录：**哪个文件多大、最后写于何时**决定接下来怎么读
2. `server_grep` 搜关键词，限定 `include` 与 `maxMatches`
3. 命中之后用 `server_read` 的 `offset` 读那一段的上下文

**资源异常（慢、卡、写不进去）**
1. `server_info` — 负载、内存、磁盘一眼看完
2. `docker_stats` — 哪个容器在吃
3. `server_ps` — 宿主上跑的进程（nginx、mysql 这类不在容器里的）

**确认某次发布上没上**
1. `docker_ps` 看镜像与「已运行多久」——刚发布的容器运行时长应该很短
2. `server_ls` 看二进制/产物目录的 mtime
3. 与代码里的 compose 比对：跑着的容器和文件里写的一致吗（drift）

## 日志有三种形态，别只看一种

1. **docker json-file** — `docker_logs` 读的就是它。**只有近期**：有轮转
   上限，翻不了多远
2. **挂载出来的文件日志** — 从 compose 的 `volumes` 反查路径，用
   `server_ls` / `server_read` / `server_grep` 读。长期留存在这里
3. **systemd journald** — `server_journal`（如果这个服务是 systemd 拉起的）

很多项目**同时有前两种**：查最近几分钟用 `docker_logs`，查昨天用文件日志。

## 成本礼节

线上机器的负载常年不低，观察不该把正经服务挤出去：

- 先 `tail`，再考虑读整段；先 `server_ls` 看大小，再决定怎么读
- `server_grep` 一定给 `include`（限定文件名）与 `maxMatches`（够用就停）
- 要跑重活之前先 `server_info` 看一眼负载
- 一次拿不完就收窄条件再来一次，**不要**原样重试同一个宽泛的查询

## 边界

- 全部只读。要改线上（重启容器、改配置）不在这套工具里，把要做的事和
  理由说清楚，交给人去做
- 一台机器上通常跑着**多个项目**，不做隔离。看之前先确认自己在看的是
  哪个项目的东西，别把别人的日志当成本项目的
```
