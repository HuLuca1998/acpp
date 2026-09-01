/**
 * Discord 频道工作区（adr-016）——与会话完全独立的子系统。
 * 字段与 server/internal/discord 的视图类型对齐。
 */

export interface DiscordConfigView {
  enabled: boolean
  /** token 永不回传，只报有没有。 */
  tokenSet: boolean
  workRoot: string
}

export interface DiscordGuild {
  id: string
  name: string
}

export interface DiscordStatus {
  /** running = 启用且配了 token（循环在跑）；connected 才是真连上了。 */
  running: boolean
  connected: boolean
  botUser?: string
  botId?: string
  appId?: string
  guilds: DiscordGuild[]
  lastError?: string
}

export interface DiscordBinding {
  channelId: string
  channelName?: string
  guildId?: string
  repo: string
  cloneUrl: string
  /** 空/缺省 = 默认分支；指定分支的克隆落在 `<仓库>@<分支>` 目录。 */
  branch?: string
  workdir: string
  agent: string
  model: string
  modelLabel?: string
  /** 空串 = 跟随 agent 默认档。 */
  effort?: string
  /** 统一权限档 safe/auto-edit/full；空按 auto-edit 兜底。 */
  access?: string
  /**
   * 频道锁定的数据源（adr-018）：非零表示这个频道的 AI 只看得见这一条连接。
   * `dataSourceRef` 是 `<项目>/<环境>` 的展示快照，连接被删掉后仍说得清
   * 原本绑的是谁。
   */
  dataSourceId?: number
  dataSourceRef?: string
  /** 频道锁定的服务器（adr-019），语义同上。 */
  serverId?: number
  serverName?: string
  createdAt: string
  updatedAt: string
}

export interface DiscordModelOption {
  id: string
  label: string
}

export interface DiscordAgentOption {
  agent: string
  models: DiscordModelOption[]
  efforts: string[]
}

export interface DiscordInfo {
  config: DiscordConfigView
  status: DiscordStatus
  bindings: DiscordBinding[]
  catalog: DiscordAgentOption[]
  /** 把 bot 邀进服务器的 OAuth2 链接（连上 gateway 后才有，client_id 已填好）。 */
  inviteUrl?: string
}

/** 配置补丁：缺省不动；botToken 空串 = 清除。 */
export interface DiscordConfigPatch {
  enabled?: boolean
  botToken?: string
  workRoot?: string
}

export interface DiscordBindingPatch {
  agent: string
  model: string
  modelLabel: string
  effort: string
  access: string
}
