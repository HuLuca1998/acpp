# ADR-016：Discord 频道工作区——与会话零耦合的独立子系统

日期：2026-08-28　状态：已采纳

## 背景

要把 acpp 接进 Discord。此前的一版实现（会话桥：Discord 里开会话、转发消息流、
裁决卡）与会话体系深度耦合，与用户想要的产品形态完全不符，已整体回退
（36 个提交，reflog d749f26）。这次先与用户对齐了形态再动工：

- **Discord 完全独立于会话**：不建会话、不复用会话的任何逻辑，Discord 侧的
  活动也不出现在网页的会话列表里。用户原话「与会话完全隔离，不使用会话的一针一线」。
- **频道即工作区**：在频道里用 `/init` 弹表单，绑定「git 仓库 + 模型 + 思考深度」；
  仓库克隆到专属的 discord 工作目录（已克隆则复用）。此后这个频道的 acp
  工作目录就是这个克隆。
- **后台可管**：设置页配 bot（开关、token、工作根）；独立的 Discord 菜单页
  看/改/删频道绑定。

本篇覆盖第一期：bot 上线、/init 绑定、克隆落盘、配置与绑定管理。
频道内对话（在绑定的工作目录里跑 acp）是下一期，另立 ADR。

## 决策

### 1. 进程内叶子包，不是独立进程

子系统住在 `server/internal/discord/`，随主服务启停。上一版曾决定走进程外
（独立二进制 + 纯 HTTP 回调），这次改为进程内，理由：

- 隔离诉求的本质是**与会话解耦**，不是与进程解耦。包级依赖约束一样硬：
  `internal/discord` 在项目内只 import 纯函数叶子包 `gitrepo`，不认识
  service/model/db/acp——会话的「一针一线」在编译期就进不来。
- 配置要进设置页、绑定要进管理页，本来就需要主服务出 API；独立进程还得
  加一层配置同步，纯增复杂度。
- 回退面一样小：删 `internal/discord/` 一个包 + 装配处几行（main.go、
  router、auth、response 各一小段）+ 前端一页一分区 + 配置文件。

### 2. 存储是单文件 JSON，不进数据库

全部状态（开关、bot token、工作根、频道绑定）在 `<dataDir>/discord.json`
（0600，token 在里面）。绑定量级是「个位数频道」，上数据库表是自找迁移负担；
单文件让「回退 = 删文件」成立。

### 3. 模型清单经装配层闭包注入

/init 表单与绑定编辑框的「模型 + 思考深度」选项来自内置工具（claude/codex）
的探测缓存（agents 表）。discord 包经 `CatalogFunc` 拿到拍平后的选项，
由 cmd/server 装配（`discordCatalog`）——包本身不认识 model/db，
依赖方向与 datasource 借 Sessions 接口同一先例。

### 4. Gateway 直连，不用库

沿用上一版实测过的极简 Gateway 客户端（coder/websocket 已是依赖）：
HELLO/IDENTIFY/心跳（序号放 `d`，放 `s` 会 4002）/断线退避重连，不做 resume。
intents 只要 GUILDS——斜杠命令与 modal 走 INTERACTION_CREATE，不需要任何
intent。discordgo 这类全功能库为此引入不值当，且新 modal 组件
（Label/RadioGroup）它未必跟得上。

### 5. /init 的交互形态

- guild 级斜杠命令（即时生效），每次连接对每个 guild 幂等重注册。
- 表单是 modal：仓库 TextInput（`owner/repo` 简写按 GitHub https 解析，
  完整 URL 走 gitrepo 的安全闸）+ 模型（RadioGroup，>10 项换 String Select）
  + 思考深度（统一五档 + 默认）。
- 提交后先回 deferred（克隆可能要几分钟），后台克隆 + 落绑定，再把结果卡
  编辑回频道。克隆超时压在 12 分钟——interaction token 只活 15 分钟，
  超过连失败都没法回写。
- 重复 /init 同一频道 = 重绑（表单预填现有仓库）。

### 6. 工作目录约定

默认根 `~/acpp-discord`，与主工作区 `~/acpp` 并排；落点 `<根>/<组织>/<仓库>`，
与项目面的克隆两层同构。同一仓库多个频道共用一个克隆（「克隆过就继续」是
用户点名的行为）。解绑不删磁盘目录——里面可能有没推送的活。

## 后果

- httpapi 的 discord handler 落在 system.go（文件数贴反平铺硬线，且它与
  title-model 同属系统平台配置）。
- `gitrepo`（URL 校验 + `<组织>/<仓库>` 命名）从 project 提为叶子包，
  两处克隆共用。
- discord 包自带哨兵错误（ErrInvalid/ErrNotFound），writeError 里做同义映射
  ——代价是映射表多两行，换来包的零依赖。
- 本期未做用户白名单：bot 只受邀进用户自己的服务器，服务器成员即可信。
  对外开服务器之前必须补门禁。
