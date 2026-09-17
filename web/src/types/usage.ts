/**
 * 用量报表的领域类型，与 server/internal/usage 对齐。
 *
 * 钱一律是**整数微元**（百万分之一美元）：几万行 float 累加出来的合计会
 * 带误差尾巴，显示前才除。
 */

/** 一组账目的合计，报表里每一处「一行数字」都是它。 */
export interface UsageTotals {
  turns: number
  sessions: number

  inputTokens: number
  outputTokens: number
  cacheReadTokens: number
  cacheWriteTokens: number
  thoughtTokens: number
  totalTokens: number

  /** 实报 + 折算的合计。 */
  costMicro: number
  /** agent 自己算的那部分（claude）。 */
  reportedMicro: number
  /** 按单价表折算的那部分（codex）。 */
  estimatedMicro: number
  /** 既没实报也没折算的轮数。**零和「不知道」是两件事**，界面要分开说。 */
  unpricedTurns: number

  /** 这些轮占用的总时长（agent 真正在干活的时间）。 */
  durationMs: number
  /** 异常三层：轮次没正常收尾的、agent 报错的、工具调用的。 */
  abnormalTurns: number
  errorTurns: number
  toolCalls: number
  toolFailed: number
}

/** 合计 + 紧邻的上一个等长周期（环比）。没给时间范围时没有 previous。 */
export interface UsageSummary {
  totals: UsageTotals
  previous?: UsageTotals
}

/** 曲线上的一格。没有数据的格子也会有，值全 0。 */
export interface UsageBucket {
  /** 这一格的起点（YYYY-MM-DD，小时粒度时带 " HH"）。 */
  date: string
  totals: UsageTotals
}

/** 明细表的一行。key 为空表示「没有」（不属于任何项目、没有模型名）。 */
export interface UsageGroupRow {
  key: string
  totals: UsageTotals
}

/** 可分组的维度，与后端白名单一一对应。 */
export type UsageDimension =
  "project" | "tenant" | "flavor" | "model" | "origin" | "session" | "day"

/** agent 报错的细分。认不出的落 other——分不出类不等于可以不显示。 */
export type UsageErrorKind = "overloaded" | "quota" | "auth" | "other"

export interface UsageErrorClass {
  /** JSON-RPC 错误码。分类按码走，文案只用于展示。 */
  code: number
  kind: UsageErrorKind
  count: number
  latest: string
  latestAt: string
  sessionId: number
}

export interface UsageErrorEvent {
  sessionId: number
  turnSeq: number
  at: string
  code: number
  kind: UsageErrorKind
  message: string
  flavor: string
  project: string
}

export interface UsageErrors {
  classes: UsageErrorClass[]
  recent: UsageErrorEvent[]
}

/** 报表的筛选条：页面上那一排选择器。 */
export interface UsageQuery {
  /** YYYY-MM-DD；传 "0" 表示全量。 */
  from?: string
  to?: string
  /** 身份 id，字符串是因为 owner 的值是 "0"——数字 0 会被 query 组装函数当成空值丢掉。 */
  tenant?: string
  flavor?: string
  project?: string
  origin?: string
  model?: string
  session?: number
}

/**
 * 一个模型（或一条 runtime 方言）的四项单价，单位是**美元 / 百万 token**。
 *
 * 四项分开而不是一个平均价：缓存读占 96% 却只要输入的 1/10，用平均价
 * 折出来的数字离谱到没有参考价值。thought 是 codex 独有，留空按 output 计。
 */
export interface ModelPrice {
  input: number
  output: number
  cacheRead: number
  cacheWrite: number
  thought?: number
}

/**
 * 折算单价表。**没有内置默认价**——模型 id 一个月里就能改，猜一个填进去
 * 报表会拿它一路算下去；没配就是「未计价」。
 */
export interface PriceTable {
  /** 每保存一次加一，落进每一行账目，用来判断哪些行用的是老价。 */
  rev: number
  /** 按模型 id 精确匹配。 */
  models?: Record<string, ModelPrice>
  /** 按 runtime 方言的兜底价（claude / codex / generic）。 */
  flavors?: Record<string, ModelPrice>
}

/** 重算历史的战果。 */
export interface UsageBackfillResult {
  sessions: number
  turns: number
  /** 有转录但会话记录已经没了的份数，跳过不记。 */
  orphans: number
  failed: number
  elapsedMs: number
}
