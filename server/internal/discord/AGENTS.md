# discord 包规范

通用后端规范见 [../../AGENTS.md](../../AGENTS.md)，此处只讲本包的硬约定。

## 文档随功能走（本包的漂移教训）

用户可见的说明分四处，各管一摊，**不许互相抄**：

| 位置 | 放什么 | 机制 |
| --- | --- | --- |
| `commands.go` 的 `slashCommands` 表 | 每条命令干什么 | **命令说明的唯一事实源**：`desc` 就是输入框打 `/` 时显示的那句话。手册与 /help 都只指路「打 `/` 看全部命令」，不再抄清单（`TestSlashCommandTable` 反向盯着，抄回去就红） |
| `guide.go` 的 `guideMD` | 用法（怎么开对话、能做什么、状态怎么看） | 频道置顶手册，是**消息**所以 markdown 会渲染。新增对话能力（附件/报告/工具面…）时必须顺手补一行 |
| `card.go` 的 `topicLine` | 这个频道绑了什么（仓库/分支/目录/模型/权限/数据库） | 频道主题，**纯文本**（连反引号都原样显示，真机实测），只放绑定信息不放用法。全量覆盖写入，≤`topicLimit`；绑定一变（/model /effort /access /db、重绑）就得刷 |
| `chat.go` 的 `discordInstructions` / `cronInstructions` | agent 的行为约定（对话 / 无人值守运行） | 不是功能清单 |
| `toolface.go` 的 `cronAddDescription` 等 | 定时任务工具的触发词与 prompt 契约 | 与 `scheduled-task` 技能是一对：工具管「想不想得起来建」，技能管「建出来的能不能跑好」 |

历史教训：加了 /skills /usage 之后注册表更新了，手册和 /help 没人记得补
（用户点名）；/db 的注册描述在默认口径反转后还写着「默认关」。所以命令
清单干脆不再有第二份——同一信息出现两处以上，要么收成单一事实源，要么
在这里登记成核对清单。

## 其他硬约定

- 本包只 import 叶子包（acp、gitrepo、mcp、webshot），业务依赖全部经
  `Deps` 闭包注入——回退面 = 删本包 + 装配几行 + 配置文件。
- 卡片色彩语言：要用户行动 = 蓝紫，过程信息 = 灰，成果/收口 = 绿，
  错误 = 红（adr-017 §11）。
- 组件 custom_id 前缀新增时必须登记 `isAskComponent`（问答卡那一族）或在
  `handleInteraction` 里加一条 case——漏了就是「该 APP 未能及时响应」（实测踩过）。
- **Components V2 的消息不能再带 `content`／`embeds` 这些老字段**，发和改都不行
  （回 400 `MESSAGE_CANNOT_USE_LEGACY_FIELDS_WITH_COMPONENTS_V2`）。一条消息用了
  `flags: 1<<15`，后续 PATCH 也只能给 `components`——包括 interaction 的
  `/webhooks/<app>/<token>/messages/@original`。真机踩过：撤销回执一直不更新，
  卡片永远停在「确认失效？」。
- 不可逆的按钮（撤销外链这类）**不与常用按钮同排**，并走二次确认：卡片在频道里
  人人可点，手机上一指宽就在旁边。布局见 `linkCard`，确认见 `revokeClicked`。
- 决策记录在 [docs/adr-016](../../../docs/adr-016-discord-频道工作区.md)
  与 [docs/adr-017](../../../docs/adr-017-discord-子区对话.md)，产品形态
  变更（默认挂载、展示统一这类）要落到 adr-017 续章。
