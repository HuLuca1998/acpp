# Discord 接入设计调研

> 状态:**调研完成,未动工**。这是设计方案与采集到的事实,不是已实现功能的描述。
> 动工前需过一遍 [§8 待拍板](#8-待拍板) 的产品决策。入口:README「尚未实现」一节。
>
> 修订 2026-08-21:对齐仓库这两天的变化——**通知体系落地**([adr-013](adr-013-通知体系.md),
> Discord 改为其第三个分派端,见 §4.5)、**编排与角色下线**([adr-012](adr-012-编排与角色下线.md),
> 清除全部编排引用)、能力面补全([adr-011](adr-011-acp-能力面补全.md),附件门控)、
> 权限卡不再 60 秒超时、轮中可改设置、用量快照落库等(散见各节)。

## 0. 定位

Discord 是 acpp 的**远程遥控器与通知面**,不是第二个工作台。核心价值来自一个架构事实:
Discord Bot 走**出站 WebSocket(Gateway)**,本机跑的 acpp 不需要公网 IP、不开入站端口、
不挂反向代理(README 安全姿态明确不允许反代)。人在外面用手机上的 Discord,操控家里
Mac 上跑的 agent。

能力边界(逐项对过会话页全部功能后的结论):

- **对话语义层 ≈95% 可等价实现**:流式正文、思考、工具调用、权限/plan/elicitation 裁决、
  插话、统一设置、图片、斜杠命令、`/db`。权限裁决在手机上反而是增强。
- **工作区数据层降级为命令式查询**:文件预览、diff、git 状态可查,但没有面板布局与联动。
- **不做**:终端(README 定性为「本机任意命令执行面」,不放上公网通道)、dockview 布局。

## 1. 如何申请 bot

### 1.1 创建应用(一次性,约 5 分钟)

1. 打开 [discord.com/developers/applications](https://discord.com/developers/applications),
   用自己的 Discord 账号登录 → **New Application**,起名(如 `acpp`)。
2. 左侧 **Installation**:Install Link 选 None 或仅 Guild Install,**关掉 User Install**——
   这个 bot 只为你的私人服务器服务。
3. 左侧 **Bot** 页:
   - **关掉 Public Bot**——关掉后只有你能把它拉进服务器;
   - 打开 **Message Content Intent**(特权 intent;bot 覆盖用户数 <1 万时在门户直接开,
     无需审核——2026-06 起的规则,阈值从「100 服务器」改成「1 万用户」);
   - **Reset Token** 生成 token,只显示一次,当场复制。泄露即重置(旧 token 立刻作废)。
4. 左侧 **OAuth2 → URL Generator**:scopes 勾 `bot` + `applications.commands`;
   Bot Permissions 按下表勾选;复制生成的邀请 URL,浏览器打开选进你的私人服务器。

| 权限 | 用途 | 必须 |
| --- | --- | --- |
| View Channels / Read Message History | 读频道与历史 | ✅ |
| Send Messages / Send Messages in Threads | 回消息 | ✅ |
| Create Public Threads / Manage Threads | 开子区、改子区名、归档加锁 | ✅ |
| Embed Links / Attach Files | 卡片与附件(diff、导出) | ✅ |
| Manage Channels | 仅 `/sync` 批量建频道需要 | 可选 |

准备工作还差一个**私人服务器**:自己建一个(免费),bot 与你都在里面。此外建议开
Discord 客户端的开发者模式(设置 → 高级)——虽然配对码方案(§2.3)不需要抄 ID,
排查问题时右键「复制 ID」很有用。

### 1.2 第二个 bot(测试用)

测试 bot 的申请流程完全相同(名字如 `acpp-test`),**不需要任何特权 intent**(它主要
发消息和 REST 读回)。它的用途与局限见 [§5](#5-自主测试与观察)。

### 1.3 红线

**不要用用户账号的 token 做自动化**(self-bot)。那违反 Discord ToS,会封号。所有自动化
一律走 bot token;需要以「真人用户」身份做的动作(点按钮、敲斜杠命令)只能由真人或
真人已登录的浏览器会话完成。

## 2. 软件内如何管理 bot

### 2.1 配置存储

照标题模型(title-model)的先例:配置存 `~/.acpp/config.json`,新增 `discord` 节:

```json
{
  "discord": {
    "enabled": true,
    "botToken": "…",
    "guildId": "…",
    "ownerUserId": "…",
    "testUserIds": [],
    "notifyChannelId": ""
  }
}
```

- `botToken` **永不出 API**——响应只带 `hasToken` 布尔位(同数据源密码的 `hasPassword` 模式)。
- `guildId`:bot 只服务这一个服务器;测试时 bot 恰好只在一个服务器里,可自动发现并回填。
- `ownerUserId`:唯一被响应的用户(见 §2.3 配对码)。
- `testUserIds`:测试 bot 的 uid 白名单,默认空;只在测试时填,生产语义与 owner 相同但
  仅限绑定到测试项目的频道(实现里硬限制)。
- `notifyChannelId`:通知分派频道——通知体系(adr-013)的 Discord 出口(§4.5);空 = 不分派。

### 2.2 设置页与 API

**设置 → Discord** 新分区(与 Claude / Codex / 系统并列),后端端点照 title-model 一族:

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/system/discord` | 当前配置(`hasToken` 代替 token)+ 运行态:连接状态、bot 用户名、服务器名、映射数 |
| PUT | `/api/system/discord` | 保存配置,热更生效:enabled 翻转即连/断 Gateway,不用重启后端 |
| POST | `/api/system/discord/test` | 用给定 token 当场验证:REST `GET /users/@me` + 列服务器,不落盘(同 title-model 的 test 语义) |
| POST | `/api/system/discord/pair` | 生成配对码(见 §2.3) |

页面元素:token 输入(留空=不改)、启用开关、连接状态灯(照 status-dot 惯例)、
「验证」按钮、配对码生成按钮、owner 绑定状态、频道映射列表(§3.4)、最近错误一条。

### 2.3 owner 绑定:配对码

让用户去开发者模式抄自己的 uid 又土又易错。改用配对:

1. 设置页点「绑定 Discord 账号」→ 后端生成 6 位一次性码,5 分钟有效,页面显示;
2. 用户在 Discord 里对 bot 发 `/claim <码>`;
3. 后端校验通过 → 把这个 interaction 的 `user.id` 写进 `ownerUserId`,回 ephemeral 确认;
4. `/claim` 是唯一在未绑定状态下响应任意用户的命令;已绑定后重新配对须先在设置页解绑。

### 2.4 连接生命周期

- 后端启动时若 `enabled && hasToken` 则连 Gateway;断线由 disgo 自动重连(resume/backoff)。
- **Discord 不可用不影响本体**:桥的一切失败只记日志 + 设置页显示,绝不拖累会话主链路
  (broker 对慢/死订阅者本来就是丢事件不阻塞)。
- 停用开关 = 断开 Gateway + 停掉全部事件泵;映射数据保留。

## 3. 如何设置频道 ↔ 工作目录

### 3.1 映射模型

```
Discord 服务器                       acpp
├── #pp-game      ← 频道 ↔ 项目(~/acpp/pp-game),即该频道所有新会话的默认 cwd
│   ├── 子区:修复登录超时   ← 一条会话
│   ├── 子区:重构数据层     ← 另一条会话(并行)
│   └── (归档子区)           ← 空闲回收的老会话,发消息自动复活(对应 session/load)
├── #acpp         ← 另一个项目
└── #acpp-通知    ← 全局通知频道(可选)
```

- **频道 : 项目 = 1 : 1,项目 : 会话 = 1 : N**(一个频道开任意多子区)。
- 为什么不是一频道一会话:服务器硬上限 500 频道,会话是快消品;子区自带
  「归档 ↔ 发消息复活」生命周期,与 acpp 的「空闲回收 ↔ session/load 恢复」完全同构。
- 绑定对象限定为**项目**(工作区根下的 git 仓库),不支持任意目录——数据源可见性、
  路径闸等安全底座都以项目为边界,Discord 不引入新的边界形状。cwd 细化到子目录 /
  worktree 放在会话级(§4.6)。
- 论坛频道(Forum Channel)也能绑:帖子在 API 层就是子区,代码同一套。原本设想给
  编排当任务看板,编排已下线(adr-012)——此形态暂无用例,仅记档备用,不排期。

### 3.2 事实源:SQLite 两张表

频道名、topic 都可被人手改,不做事实源;映射入库(AutoMigrate):

```go
// DiscordChannel 一条频道绑定:频道 ↔ 项目。
type DiscordChannel struct {
    ID          uint   `gorm:"primarykey"`
    ChannelID   string `gorm:"uniqueIndex"` // Discord 频道 id(雪花)
    ProjectName string                      // 项目名(相对工作区根,如 "pp-game" 或 "org/repo")
    AgentID     uint                        // 频道默认 agent;0 = 全局默认
    CreatedAt   time.Time
}

// DiscordThread 一条子区绑定:子区 ↔ 会话。
type DiscordThread struct {
    ID        uint   `gorm:"primarykey"`
    ThreadID  string `gorm:"uniqueIndex"`
    SessionID uint   `gorm:"uniqueIndex"` // 一条会话至多一个子区
    ChannelID string                      // 所属频道,冗余存,重启恢复用
    LiveMsgID string                      // 当前流式占位消息 id(崩溃恢复后好清理)
    CreatedAt time.Time
}
```

### 3.3 建立与维护绑定(Discord 侧,主入口)

| 命令 | 行为 |
| --- | --- |
| `/bind project:<名>` | 把**当前频道**绑到项目。autocomplete 走 `project.Service.List`(≤25 条,超出按输入前缀过滤)。已绑定则覆盖并提示。顺手把频道 topic 写成 `acpp: ~/acpp/<名>`(人类可读,非事实源) |
| `/unbind` | 解绑当前频道(子区映射保留,变只读——不再响应新消息,历史可看) |
| `/sync` | 可选功能:对账项目清单与某分类下的频道,缺的批量创建并绑定(频道名=项目名转小写、`/`→`-`)。需要 Manage Channels 权限;**不自动跑**,永远显式触发 |

### 3.4 web 管理面(观察 + 删除)

设置页的映射列表:频道名、项目、会话数、状态(正常/项目已删),支持删除绑定。
**创建只在 Discord 侧**(`/bind` 需要「站在那个频道里」的语境,web 端做频道选择器
要多一套 REST 拉频道列表的交互,收益低)——列表页引导「去 Discord 频道里发 /bind」。

### 3.5 异常与边界

- **频道被删**(Gateway `ChannelDelete` 事件)→ 自动解绑,日志一条。
- **项目目录被删** → 映射标「失效」,频道里新消息回一句「项目已不存在,请重新 /bind」。
- **未绑定频道 / DM**:不响应普通消息(防误触);斜杠命令可用(`/claim`、`/bind`、`/sessions` 等)。
- **子区里的消息但会话已被删**(web 侧删的)→ 回一句「会话已删除」,归档并加锁该子区。
- **Discord 删子区** → 只解绑,**不删会话**(Discord 是遥控器不是事实源,数据面永远以 acpp 为准)。

## 4. 具体实现方案

### 4.1 选型

- **库:[disgoorg/disgo](https://github.com/disgoorg/disgo)**。理由:模块化(gateway/rest/cache
  可拆)、对新交互面(Components V2、modal、autocomplete)跟进快、REST 层内置限速处理。
  备选 bwmarrin/discordgo(最老牌,但新特性滞后)。这是纯技术选型,按惯例不上升为 ADR 决策项。
- 新增依赖仅此一个;测试 bot 不需要额外依赖(REST 直接 `net/http` 也行,同库更省事)。

### 4.2 包结构与依赖方向

```
server/internal/discordbridge/     业务层,与 datasource / system 同级
├── gateway.go    连接管理:token/intents、连接与重连、Ready 恢复、命令注册
├── bridge.go     映射表读写 + 事件泵编排(每条绑定会话一个 pump goroutine)
├── pump.go       事件泵:Subscribe → 聚合 → 节流渲染 → 定稿
├── render.go     stream.Event → Discord 消息(分段/活动卡/按钮/spoiler)
├── commands.go   斜杠命令定义与 interaction 分发
└── config.go     discord 配置节的读写(config.json)
```

依赖方向 `discordbridge → {service, project, stream, model, config}`,与其他业务包同构,合规。
装配在 `cmd/server/main.go`;httpapi 新增 system_discord.go 一个 handler 文件。
**service / stream / acp / project 零改动**——桥是第二个前端,全部走现有导出方法:

| 桥的动作 | 现成挂载点 |
| --- | --- |
| 建会话 | `SessionService.Create(ctx, OwnerScope(), SessionInput{AgentID, Cwd, Title, Worktree})` |
| 发消息 | `ChatService.Send(ctx, id, SendInput{Content, Images, Files})`(异步,立即返回) |
| 订阅事件 | `ChatService.Subscribe(sessionID)`(per-session broker,多订阅者,轮内重放) |
| 订阅通知 | `stream.Hub.Subscribe(tenantID)`(装配层的 noticeHub,0 = owner,§4.5) |
| 裁决 | `ChatService.ResolvePermission / ResolveElicitation` |
| 中止/设置 | `ChatService.Cancel / ApplySettings` |
| 定稿消息 | `ChatService.Messages(sessionID, limit, before)`(转录重建) |
| 项目/目录 | `project.Service.List` / `service.ListDirs` / `service.DefaultCwd` |

### 4.3 Gateway 连接

- Intents:`GUILDS | GUILD_MESSAGES | MESSAGE_CONTENT`。
- 斜杠命令注册为 **guild 级**(即时生效;global 注册有长达 1 小时的传播延迟,自用没理由用)。
  注册在每次 Ready 时幂等执行(disgo 的 set commands 是全量覆盖语义)。
- 事件处理:`MessageCreate`(入站消息)、`InteractionCreate`(命令/按钮/modal/autocomplete)、
  `ThreadDelete` / `ChannelDelete`(解绑)、`Ready`(恢复)。
- 所有入站先过**三道闸**,不过闸的静默丢弃(按不存在处理,不回错误):
  ① `guildID` 匹配;② `user.id ∈ {ownerUserId} ∪ testUserIds`;③ bot 自己的消息跳过。

### 4.4 入站:消息 → 会话

**频道根消息**(绑定频道内):

```
MessageCreate(频道根)
→ 以该消息开子区(名 = DeriveTitle(内容),≤15 字符合子区名 100 上限)
→ SessionService.Create(cwd = 项目根, agentID = 频道默认, title 同名)
→ 写 DiscordThread 映射,启动事件泵
→ 附件里的图片下载转 base64 进 SendInput.Images(≤10MB,Discord 上限本来就低于我们 32MiB;
   **按 promptCapabilities 门控**,adr-011——agent 不支持图片时回明确提示,不静默吞掉,
   与 web 输入框按能力隐藏附件入口是同一条规则)
→ ChatService.Send(懒连接语义照旧)
```

**子区内消息**:查映射 → `Send`。正在跑 = 插话(claude 排队/codex steering,现有语义);
归档子区收到消息 Discord 自动复活它,我们这侧就是普通 send(`session/load` 恢复上下文)。
**并发满**(`ACP_MAX_SESSIONS`,第 9 条拉不起来):回「⏳ 并发已满,稍后重试」,不静默。

### 4.5 出站 ①:通知分派(adr-013 的第三个分派端,M1/M2 的全部)

后端已有全局通知底座,动工时**零后端改动**:`stream.Hub` 由装配层注入 `ChatService`,
四类值得打扰的事(permission / elicitation / turn_end / error)加两条撤回信号
(permission_done / elicitation_done)以 `stream.Notice` 广播。浏览器经 `GET /api/events`
消费(侧栏通知中心),macOS 壳发系统通知——**Discord 桥就是第三个分派端**:进程内
`noticeHub.Subscribe(0)`(owner 身份),把 Notice 渲染进 `notifyChannelId`。

- **分派判断**:adr-013 定的是「后端只广播,弹不弹由客户端判断」。Discord 的判断最简单——
  它存在的意义就是「人不在机器前」,四类默认全发;要静音就关 enabled 或清 notifyChannelId,
  不做逐类偏好(通知中心已有逐类开关,不重复一套)。
- **决策通知天生带按钮**:Notice 载荷自带 `PermissionID` + `Options`(agent 给的选项,≤4 个)
  ——这正是 macOS 系统通知动态按钮的数据源,同一份载荷在 Discord 渲染成按钮行绰绰有余
  (5 个/行上限)。M1 可先发不可点的卡,M2 接上 interaction 回传后同一张卡升级为可点。
- **撤回收口**:permission_done / elicitation_done 到达时把对应通知消息编辑为终态
  (「已在别处处理 ✓」)——与通知中心、系统通知的撤回行为一致,不会有哪端挂着已死的请求。
  桥按 `PermissionID` / `ElicitationID` 记消息 id,内存态、重启即弃,与 Hub 不重放的语义一致。
- **elicitation 对齐壳的取舍**:结构化多题(选择 + 自由输入混合)塞不进一条通知,壳的做法是
  不带按钮、点开会话回答。Discord 同策:通知卡带跳转链接;仅纯单选题上按钮,modal 承载
  多题留到 M3 后按实际形状评估,不提前承诺。
- **与 M3 泵的去重**:绑定了子区的会话,裁决卡由泵发在子区里(上下文就在旁边),Hub 这条
  不再往通知频道重复发;未绑定会话才走通知频道。判定一条:查 DiscordThread 映射。

Hub 不重放、慢订阅丢弃(缓冲 16)——错过的打扰没有补发价值,悬着的事以会话侧卡片
(转录重建)为事实源,这与桥的定稿路径(§4.5B)是同一个哲学。

### 4.5B 出站 ②:事件泵(M3 对话镜像的核心)

M1/M2 用不到本节——只有绑定子区的会话才需要逐 chunk 镜像。每条绑定会话一个常驻
goroutine(几百条也只是几百个空闲 goroutine,Go 不在乎;好处是 **web 侧发起的轮同样
被镜像**,不用琢磨订阅时机):

```
ch, cancel := chat.Subscribe(sessionID)
for ev := range ch:
  user_message      → 若非桥自己代发(pending 集合去重)→ 镜像到子区:「👤 <正文>」
  message_chunk     → 进正文缓冲
  thought_chunk     → 进思考缓冲(渲染为 ||spoiler||,默认折叠)
  tool_call(+更新)  → 按 toolCallId 合并进本轮「活动卡」(标题+状态 emoji 列表)
  permission        → 立即发裁决卡(不节流):标题+选项按钮;PlanReview 非空时渲染计划审批形态
  permission_done   → 把裁决卡编辑为终态(⚪ 已批准/已拒绝)——web 侧点的也在这收口,双端一致
  elicitation       → 选项按钮;自由文本配「✏️ 回答」按钮 → 弹 modal(modal 只能由点击触发)
  elicitation_done  → 同收口
  turn_end          → 记 stopReason 与 token 计量(定稿时做 footer)
  turn_done         → 定稿(见下);清缓冲
  error             → 「❌ <错误>」独立消息
  session_title     → 改子区名一次(改名限速 2 次/10 分钟,只发生一次,安全)
每 1.5s ticker      → flush:live 消息(创建或编辑)显示正文缓冲的尾部 ~1800 字
                      + 活动卡有变化则编辑活动卡;turn 进行中每 ~8s 触发 typing 指示
```

**节流为什么是 1.5s**:消息编辑限速约每频道 5 次/5 秒,而**每个子区是独立限速桶**——
单会话 1.5s 一次编辑吃不满桶,多会话并行互不挤兑。live 消息 + 活动卡 = 每 tick 至多
2 次编辑,仍在桶内;万一 429,disgo 的 REST 层自动排队重试,泵不感知。

**定稿**:`turn_done` 后调 `Messages()` 拿转录重建的权威消息列表,删掉 live 占位,
按重建结果分段(≤2000 字/条,断点优先落在代码块边界)重发。这与 web 前端
「流式拼接 → 重建取代」是同一模式,**broker 丢事件的容错也因此同构**——慢订阅丢了
chunk 不要紧,定稿以重建为准。超长内容(diff、长代码)转附件(`.md`/`.patch`)。
重建器如今除正文/思考/工具调用外还产出 **plan 快照**(折叠一行进度)、
**权限裁决记录**(kind=permission_request,谁请求 + 用户选了什么)与挂在末段正文上的
**本轮 token 计量**(payload.turnUsage)——定稿因此可以带裁决记录行与 token footer,
数据全部现成,桥只做渲染。

### 4.6 斜杠命令清单

| 命令 | 参数 | 落点 |
| --- | --- | --- |
| `/claim` | `code` | owner 配对(§2.3) |
| `/bind` `/unbind` `/sync` | `project`(autocomplete) | 映射管理(§3.3) |
| `/new` | `agent?` `subdir?`(autocomplete=ListDirs) `worktree?` | 开空子区+会话;subdir 细化 cwd,worktree 走 `SessionInput.Worktree` |
| `/sessions` | — | 本频道(项目)会话列表,每条带子区跳转链接;未绑定频道列全部 |
| `/watch` | `session`(autocomplete=近期会话) | 给已有 web 会话开子区并绑定(web 会话默认不自动开子区,防刷屏) |
| `/cancel` | — | 子区内:中止当前轮 |
| `/settings` | `model?` `effort?` `level?` `plan?` `fast?` | 子区内:ApplySettings;无参时显示当前值。**轮进行中也可改**(权限档与思考深度已支持轮中热切),回收重开后设置会被回放,不丢 |
| `/status` | — | 子区内:会话状态(running / stopReason / 上下文占比——用量快照已落库,**未连接的会话也有数**;claude 另有累计费用);频道根:项目概况 |
| `/db` | `env?` `database?` | 数据源速查(同 web 的本地 `/db`,ForCwd 过滤天然生效) |
| `/git` | `sub: status\|log\|diff` | 只读 git 摘要(embed);diff 超长转附件 |
| `/cat` | `path`(autocomplete) | 文件预览(code block / 附件) |

agent 侧斜杠命令(`/compact` 之类)不注册成 Discord 命令——正文里写 `/xxx` 原样透传,
agent 自己认(与 web 输入框行为一致,Discord 只是少了补全)。

### 4.7 交互回传

- 按钮 `custom_id` 方案:`p:<sessionID>:<permissionID>:<optionID>`(≤100 字符;ACP 的 id
  都是短 uuid,放得下;放不下再降级为泵内存里的序号表——挂起裁决本来就活不过进程重启)。
- 收到按钮 interaction:三道闸 → `ResolvePermission` → ack + 编辑原消息为终态。
  迟到的点击(已裁决/已失效)回 ephemeral「已处理过了」。
- **远程慢批的前提已在主干落地**:权限卡不再 60 秒自动取消,agent 无限期挂起等裁决,
  且 turn 进行中(含等待裁决)不会被空闲回收——人在外面隔一小时再批也成立。
- modal 提交 → `ResolveElicitation(id, "accept", {text: …})`。

### 4.8 回环与镜像

桥代发的 `Send` 会触发自己订阅里的 `user_message` 事件。防回环:`Send` 前把
`(sessionID, content)` 放 pending 集合,泵收到匹配的 `user_message` 时消掉且不镜像;
不匹配的(web 发的)正常镜像。零协议改动,不给 stream.Event 加字段。

### 4.9 重启恢复与失败模式

- **后端重启**:Ready 后从两张表重建全部泵;`LiveMsgID` 非空的(上次崩在流式中途)
  把残留 live 消息编辑为「⚠️ 服务重启,本轮中断」。挂起的权限卡随 agent 进程一起消失,
  按钮点击会得到「已处理过了」——诚实且无害。
- **Gateway 断线**:disgo 自动 resume;断线期间发生的 turn,其 chunk 丢了,但只要重连后
  会话还在跑或结束,定稿路径(`Messages()`)补全一切。断线超过一轮的极端情况,
  子区里缺一轮镜像——`/status` 可查,不做补发(复杂度不值)。
- **acpp 正常、Discord 全站故障**:泵的 REST 调用失败只记日志,会话主链路零影响。
- **agent 未登录**:握手失败如今返回明确的登录引导(替代模糊报错)——桥原样转发到
  子区/通知频道;登录动作只能回本机完成,Discord 里给指引即可,不装作能代办。

### 4.10 安全清单

1. 三道闸(guild / uid 白名单 / 忽略 bot),不过闸静默丢弃——不回 403,按不存在处理。
2. 一律 `OwnerScope()`;`testUserIds` 只在绑定到测试项目的频道内生效(代码硬限制)。
3. **不做终端**;`/git` `/cat` 只读;写操作只有「发消息给 agent」一条路,而那条路受
   ACP 权限裁决管辖——Discord 没有绕过面板权限体系的通道。
4. token 只进 config.json;`hasToken` 模式;文档写明泄露即在门户 Reset。
5. README 安全姿态补一节:**会话内容(代码、diff、查询结果)经由 Discord 服务器**,
   与「局域网共享前提是可信网络」同级的诚实边界;私人服务器 + 关 Public Bot 是底线。

### 4.11 里程碑

| 阶段 | 范围 | 交付判断 |
| --- | --- | --- |
| M1 通知 | config + 设置页 + 通知分派(§4.5):`noticeHub.Subscribe(0)` → 四类通知卡推 `notifyChannelId`,撤回信号编辑收口;决策卡先不可点 | 手机收到「agent 在等你批准」,web 处理后 Discord 卡片同步收口 |
| M2 遥控 | Gateway 交互面 + `/claim`、三道闸、决策卡升级为按钮裁决、`/sessions` `/cancel` `/status` | 手机上把卡住的轮批完 |
| M3 对话 | `/bind` 映射、频道根开会话、子区续聊、事件泵(§4.5B)全量、定稿分段、`/new` `/watch` `/settings` | 子区里跑完一个完整任务 |
| M4 查询 | `/db` `/git` `/cat`、附件 | 按需 |

M1 踩通知体系的 Hub,M3 踩会话流的 Broker——两个现成底座,互不重叠,都不是丢弃式
工作。每阶段独立可交付、独立提交。

## 5. 自主测试与观察

**结论:需要第二个 bot,但一个测试 bot 兼任「驱动 + 观察」,不需要第三个。**
关键平台事实(已核实):bot **不能**触发另一个 bot 的斜杠命令、**不能**点它的按钮、
**不能**提交它的 modal——interaction 只能由真人用户发起,没有任何 API 绕过。
自动化用户账号(self-bot)违反 ToS,不做。

因此测试 bot 的能力矩阵:

| 动作 | 测试 bot 能否 | 说明 |
| --- | --- | --- |
| 往频道/子区发普通消息 | ✅ | 驱动「频道根开会话」「子区续聊」「插话」全链路(桥把它的 uid 放进 `testUserIds`) |
| REST 读消息/子区/子区名 | ✅ | 断言主 bot 的流式编辑、定稿分段、活动卡、子区改名 |
| 建/删测试频道 | ✅ | 造环境、清环境 |
| 用主 bot 的斜杠命令 | ❌ | 平台限制 |
| 点主 bot 的按钮 / 交 modal | ❌ | 平台限制 |

### 5.1 测试金字塔

- **L1 Go 测试(无网络,大头)**:渲染纯函数(分段断点、活动卡合并、custom_id 编解码、
  spoiler/附件降级)、映射表迁移、防回环 pending 集合、泵状态机(把 disgo 的收发抽成
  接口,喂录制的 `stream.Event` 序列断言出站调用序列)。interaction 的 handler 逻辑
  (裁决落点)同样可测——它只是「解参 → 调 service」。通知分派同理:测试里直接向
  `stream.Hub` Publish 一条 Notice,断言桥的出站渲染与撤回编辑。
- **L2 双 bot 冒烟(真实 Discord,自动)**:同一服务器下 `#acpp-test` 频道绑一个一次性
  测试项目;测试 bot 发消息驱动,轮询 REST 读回断言。覆盖:开会话、流式出现且定稿、
  插话、归档复活、并发满提示、非白名单消息被无视。跑在本机(需要两个 token 的 env),
  不进 `make check`(依赖外网与测试服务器,做成 `make test-discord` 显式触发)。
- **L3 交互最后一公里(半自动)**:按钮/斜杠/modal 只能真人会话触发。两条路:
  ① 我用 Claude in Chrome 驱动你**已登录的 Discord 网页**点按钮、敲命令(真人会话,
  合规,全程可自主完成并截图取证);② 你手机上点一遍,我用测试 bot 的 REST 读回
  验证结果。L3 覆盖面小(就裁决与命令注册两块),每个里程碑收尾跑一次即可。

### 5.2 对你的实际要求

提供两份 token(主 bot + 测试 bot,都拉进同一个私人测试服务器)即可;L3 走 Chrome 时
你只需保持浏览器里 Discord 已登录。观察面我全程自主:测试 bot 的 REST 读回 + 后端日志
(`/tmp/acpp-dev/`)+ 会话转录 JSONL 三路对账。

## 6. 硬限制速查(设计已绕开,实现时照查)

| 项 | 值 |
| --- | --- |
| 频道/服务器 | 500(子区不计入);分类内 50 |
| 消息长度 | 2000 字符;embed 合计 6000;子区名 100 |
| 消息编辑限速 | ~5 次/5 秒/频道,**子区独立计桶** |
| 频道/子区改名 | 2 次/10 分钟 |
| 按钮 | 5 行 × 5 个;`custom_id` ≤100 字符 |
| select / autocomplete | 25 项;autocomplete 3 秒内必须应答 |
| bot 附件 | ~10MB |
| Message Content Intent | 特权;<1 万用户门户自开(2026-06 规则) |
| bot 间 interaction | 不可能:斜杠/按钮/modal 只有真人用户能触发 |

## 7. 与现有文档的同步义务(动工时)

README:目录结构(`discordbridge`)、API 表(`/api/system/discord*`)、配置表(config.json
`discord` 节)、安全姿态(内容经由 Discord)、尚未实现一节移除本条;本文档若有方案变更,
按 docs 规范新增修订记录而不改历史结论。

## 8. 待拍板

1. **频道根消息直接开会话**是否开启(vs 必须 `/new`)——推荐开,误触已被 uid 白名单挡住。
2. **`/sync` 批量建频道**要不要做(需要 Manage Channels 权限)——推荐 M3 不做,手动 `/bind` 够用。
3. **频道默认 agent**(如 #pp-game 默认 codex)要不要这层默认——推荐做,`/new` 可覆盖。
4. web 会话是否永不自动开子区(现方案:是,`/watch` 显式绑定)。
5. M1 先行还是直接 M2/M3 一起(M1 独立价值:手机收通知回电脑操作)。

## 参考

- [Discord Developer Portal](https://discord.com/developers/applications) ·
  [Threads 文档](https://docs.discord.com/developers/topics/threads) ·
  [Rate Limits 文档](https://docs.discord.com/developers/topics/rate-limits) ·
  [OAuth2 文档](https://docs.discord.com/developers/topics/oauth2) ·
  [特权 Intent 规则(1 万用户阈值)](https://support-dev.discord.com/hc/en-us/articles/40281523410967-Changes-to-Privileged-Intent-Access-for-Discord-Apps)
- [disgoorg/disgo](https://github.com/disgoorg/disgo) · [bwmarrin/discordgo](https://github.com/bwmarrin/discordgo)
- bot 间 interaction 限制:[社区确认 1](https://community.latenode.com/t/can-a-discord-bot-trigger-slash-commands-from-other-bots/7678) · [社区确认 2](https://community.latenode.com/t/can-a-discord-bot-trigger-another-bots-slash-commands/14553)
