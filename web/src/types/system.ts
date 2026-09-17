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
