/**
 * 系统平台面的类型：数据目录、标题模型、环境体检、版本更新。
 *
 * 从 acp.ts 拆出来的：这些与 ACP 协议域毫无关系，它们描述的是「这台机器
 * 上的本软件」——设置页的系统分区读它们，别的页面一概用不到。
 */

/** 系统配置：数据目录状态（设置面板用）。 */
export interface SystemInfo {
  /** 当前进程实际使用的数据目录。 */
  dataDir: string
  /** 默认数据目录（~/.acpp）。 */
  defaultDir: string
  /** 非空表示已迁移到新目录、等待重启生效。 */
  pendingDir?: string
  /** 工作区根：新会话工作目录的默认落点，也是访客各自 root 的父目录。 */
  workspaceDir: string
  /** 局域网里访问本服务的地址前缀（`http://<ip>:<端口>`），后端拼好。
   *  用来把相对路径拼成可以转发给同事的完整链接。 */
  lanBase?: string
  /** 这条地址当下能不能真发出去：只监听回环时给的是 127.0.0.1，
   *  那条链接只有本机点得开。 */
  lanShareable?: boolean
  /** 工作区根的默认值（~/acpp）。 */
  defaultWorkspaceDir: string
}

/**
 * 会话标题模型配置：本机 ollama 上跑的小模型，用来把「首句截断」换成
 * 真正的概括。两端 agent 的自动标题都长在各自 CLI 层，ACP 通道取不到，
 * 所以这件事由本项目自己做；没配置就沿用首句派生，功能不受影响。
 */
export interface TitleModelConfig {
  enabled: boolean
  /** ollama 地址，留空按默认 http://127.0.0.1:11434 走。 */
  baseUrl: string
  /** 模型名，如 qwen3.5:9b-mlx。启用时必填。 */
  model: string
}

/** ollama 上已安装的一个模型。size 供界面提示体积——标题这种轻活选小的更快。 */
export interface OllamaModel {
  name: string
  size: number
}

/** 环境体检的一项依赖。 */
export interface EnvDependency {
  key: string
  installed: boolean
  version?: string
  path?: string
  /** auto 可一键安装；manual 需终端手动执行；bundled 随其他依赖就位。 */
  installKind: "auto" | "manual" | "bundled"
  /** manual 时给用户复制执行的命令。 */
  installHint?: string
  /** 一键安装的前置依赖 key。 */
  requires?: string
  /** 包管理器上的最新版本；离线、或该项不查新版（brew/node/npm）时为空。 */
  latest?: string
  /** 为真表示 version 落后于 latest，按钮变「更新」。 */
  outdated?: boolean
  /**
   * 非空表示这个命令还是旧的 npm 全局安装占着，而本项已改由 Homebrew 管；
   * 两边争同一个命令名，必须先在终端跑这条命令腾位置。此时禁用一键安装。
   */
  migrateHint?: string
}

/** 环境体检结果；path 是后端进程实际使用的 PATH。 */
export interface EnvInfo {
  deps: EnvDependency[]
  path: string
}

/** 一次依赖安装的结果；ok=false 时 output 是失败输出尾巴。 */
export interface EnvInstallResult {
  key: string
  ok: boolean
  output: string
}

/** 版本检查结果（GitHub Releases）。 */
export interface UpdateInfo {
  currentVersion: string
  repo: string
  latestVersion?: string
  hasUpdate: boolean
  /** 最新版本的 release 描述（markdown 原文，按纯文本展示）。 */
  notes?: string
  /**
   * 当前版本与最新版本之间**全部待更新版本**的日志，版本从新到旧。
   * 跨版本更新时中间几版改了什么也该看得到，不能只给最后一步。
   */
  pending?: {
    version: string
    notes?: string
    publishedAt?: string
    url?: string
  }[]
  /** 待更新版本超过展示上限时，更早的还剩几个。 */
  pendingMore?: number
  publishedAt?: string
  releaseUrl?: string
  assetName?: string
  checkedAt?: string
  checkError?: string
  /** 是否支持一键更新重启（仅桌面版 .app 内为真）。 */
  canApply: boolean
}

/**
 * 一键更新的阶段：idle 没在更新；paused 是下载停着、半成品留着（再点继续
 * 就续传，进程重启后也认得）；restarting 之后进程随即被换掉；done 是装好了
 * 但要手动重开；failed 重试即续传。
 */
export type UpdatePhase =
  | "idle"
  | "downloading"
  | "paused"
  | "unpacking"
  | "installing"
  | "restarting"
  | "done"
  | "failed"

/** 一键更新的进行态（后端后台跑，前端下载期间轮询）。 */
export interface UpdateProgress {
  phase: UpdatePhase
  version?: string
  /** 字节数（续传时从上次的位置起算）；total 为 0 表示还不知道总长。 */
  downloaded: number
  total: number
  /** 最近几秒的平均下载速度（字节/秒），下载阶段之外为 0。 */
  speed: number
  startedAt?: string
  updatedAt?: string
  /** 给人看的一句话：断线续传中、已暂停、装好了怎么重启…… */
  message?: string
  /** failed 的原因。 */
  error?: string
}

/**
 * codex 的隔离 home（<dataDir>/codex-home）里那两个要改的文件。
 *
 * config.toml 是系统配置的一次性副本——给 acpp 的 codex 换模型/provider
 * 改的就是它；auth.json 软链系统的登录态，改它等于改系统那一份。
 */
export interface CodexFile {
  name: string
  /** 还没生成时为 false（config.toml 要第一次起 codex 会话才被复制出来）。 */
  exists: boolean
  size: number
  updatedAt: string
  symlink: boolean
  target?: string
}

export interface CodexHomeInfo {
  dir: string
  files: CodexFile[]
}

/** 套餐用量（plan quota）能不能用：非 ok 时 windows 为空，界面按状态给引导。 */
export type QuotaStatus =
  "ok" | "expired" | "not_logged_in" | "unavailable" | "error"

/**
 * 一个限额窗口。kind 认得的有 session（5 小时滚动窗）/ weekly（每周全模型）/
 * weekly_model（每周按模型，model 给型号名）；认不出的按 windowSeconds 说长度。
 */
export interface QuotaWindow {
  kind: "session" | "weekly" | "weekly_model" | "window" | (string & {})
  model?: string
  windowSeconds?: number
  /** 已用比例 0–100。 */
  percent: number
  /** 下次归零的时刻，服务端没给就没有。 */
  resetsAt?: string
  /** 当前真正卡着的那个窗口（claude 的 is_active）。 */
  active?: boolean
}

/**
 * 本机登录账号在订阅套餐上的限额水位，与 server/internal/system 的 PlanQuota
 * 对齐。它与「用量报表」是两回事：报表记这台机器花了多少，水位是账号在
 * 服务端还剩多少——后者只有服务端知道。
 */
export interface PlanQuota {
  flavor: "claude" | "codex"
  status: QuotaStatus
  /** 套餐名（max / pro / prolite …），原样透传。 */
  plan?: string
  windows: QuotaWindow[]
  /** codex 的额度余额（套餐窗口之外按量计费的部分）。 */
  credits?: { balance: string; unlimited: boolean }
  /** claude 的额外用量（套餐之外的付费额度），没开就没有。 */
  extra?: { percent: number; used: number; limit: number; currency?: string }
  /** 非空表示这里的 codex 走的是第三方 provider，消耗的不是这份额度。 */
  provider?: string
  fetchedAt: string
  error?: string
}
