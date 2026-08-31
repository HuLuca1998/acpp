# Discord bot 申请与配置手册

acpp 的 Discord 接入（adr-016/017）需要一个 Discord bot。本手册覆盖：申请
新 bot、必需的开关、拿 token、邀请进服务器、配进 acpp，以及 dev / app 双
后端的隔离约定。

## 双 bot 约定（为什么要两个）

dev 后端（48080）与 app 后端（48090）**必须用不同的 bot**：Discord 允许同
一 token 多条 gateway 连接，事件会**同时投给两个后端**——同一条消息双回
复、interaction 互抢，看起来就是「bot 精神分裂」。分工：

| 用途 | bot | 数据目录 |
| --- | --- | --- |
| 开发/测试（dev 48080） | 现有 bot `acpp`（测试服务器与频道绑定都在它身上） | `~/.acpp-dev`（dev.sh 自动播种与隔离） |
| 正式使用（app 48090） | 新申请的 bot（本手册的产物） | `~/.acpp` |

`scripts/dev.sh` 首次运行会把 `~/.acpp` 播种到 `~/.acpp-dev`，并顺手把
`~/.acpp/discord.json` 的 `enabled` 置 false——app 更新出 discord 功能后
不会拿旧 token 抢线，在 app 设置页填新 bot 的 token 即可。

## 申请步骤（约 5 分钟）

1. 打开 <https://discord.com/developers/applications>，右上 **New Application**，
   起名（如 `acpp`），Create。
2. 左栏 **Bot**：
   - **Icon** 上传头像：仓库里现成的 [docs/assets/discord-bot-avatar.png](assets/discord-bot-avatar.png)
     （SVG 源同目录，改设计后用 Chrome headless 重渲染即可）。
   - **Privileged Gateway Intents** 打开 **MESSAGE CONTENT INTENT**（必需——
     bot 要读消息正文才能对话；SERVER MEMBERS / PRESENCE 不需要）。
   - **Reset Token** 拿到 bot token，**只在这一刻完整显示**，存好。
3. 左栏 **Installation**：
   - Install Link 选 **Guild Install**；
   - Scopes 勾 `bot` + `applications.commands`；
   - Bot Permissions 勾：**View Channels、Send Messages、Send Messages in
     Threads、Create Public Threads、Manage Threads、Manage Messages（置顶
     手册要用）、Embed Links、Attach Files、Read Message History、Add
     Reactions、Manage Channels（写频道主题要用）**。
4. 用生成的 Install Link 打开、选服务器、授权——bot 出现在成员列表（离线）。

## 配进 acpp

设置页 → Discord 分区：打开启用、粘贴 token、保存。状态变「已连接」即上线
（gateway 由后端维护，断线自动重连）。然后在目标频道里输入 `/init` 绑定
仓库与模型——绑定完成后 bot 会在频道里发布并置顶一份使用手册。

## 排查

- **斜杠命令不出现**：命令按 guild 注册、连上即时生效；bot 刚邀请进来要等
  一次重连（设置页关开一次 discord 即可）。
- **@bot 没反应**：确认 @ 的是 bot（用户或它的同名集成角色都行——两个都
  认）；主频道只认 @，闲聊不响应是设计。
- **消息双回复**：两个后端在用同一个 token，回去看「双 bot 约定」。
- **token 泄露**：Developer Portal → Bot → Reset Token，旧 token 立即作废，
  acpp 设置页换新的。
