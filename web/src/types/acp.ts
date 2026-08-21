// ACP (Agent Client Protocol) 领域类型，与 server/internal/model 保持一致。

export type AgentStatus = "idle" | "error"

/** agent 暴露的一条斜杠命令；发送时就是普通文本（"/plan …"）。 */
export interface SlashCommand {
  name: string
  description?: string
  /** 配置页的取舍：true 表示不在本软件里使用（缺省启用）。 */
  disabled?: boolean
}

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

/** 配置页对一批条目的取舍；key 对模型是 id、对命令是 name。 */
export interface CatalogInput {
  models?: { key: string; disabled: boolean; alias?: string }[]
  commands?: { key: string; disabled: boolean }[]
  /** 快速模式取舍；缺省不动。 */
  fastPolicy?: "on" | "off"
}

/** runtime 方言，由后端从 agent 身份识别；generic 表示未知 runtime。 */
export type AgentFlavor = "codex" | "claude" | "generic" | ""

/** 统一模型描述，id 是 runtime 自己的标识，透传不映射。 */
export interface UnifiedModel {
  id: string
  name: string
  description?: string
  /** 配置页的取舍：true 表示不在本软件里使用（缺省启用）。 */
  disabled?: boolean
  /** 用户在配置页起的显示别名，空则用原名。 */
  alias?: string
}

export interface Agent {
  id: number
  name: string
  description: string
  command: string
  args: string[]
  env: Record<string, string>
  cwd: string
  status: AgentStatus
  lastError: string
  /** 探测缓存：flavor、模型清单与斜杠命令，供草稿态在建会话前使用。 */
  flavor: AgentFlavor
  models: UnifiedModel[]
  commands: SlashCommand[]
  /** 探测缓存的设置骨架：模型之外的维度清单与开关支持位。 */
  skeleton?: {
    efforts?: EffortLevel[]
    levels?: AccessLevel[]
    planSupported?: boolean
    fastSupported?: boolean
    /** prompt 内容能力（已含方言兜底），草稿态门控附件按钮。 */
    promptImage?: boolean
    promptAudio?: boolean
    promptEmbedded?: boolean
  }
  /** 快速模式取舍：off 时快速开关不出现（空=未定，探测按 flavor 落默认）。 */
  fastPolicy?: "on" | "off" | ""
  createdAt: string
  updatedAt: string
}

export type SessionState = "active" | "idle" | "ended" | "error"

/** 统一思考深度，五档——两条 ACP 选项的交集。 */
export type EffortLevel = "low" | "medium" | "high" | "xhigh" | "max"

/** 统一权限档，三档，两条 ACP 全覆盖。 */
export type AccessLevel = "safe" | "auto-edit" | "full"

/**
 * 会话设置的统一视图（交集规范：只含两条 ACP 都支持的维度）。
 * 空数组表示该 runtime 不支持这个维度，对应控件应隐藏。
 */
export interface SessionSettings {
  flavor: AgentFlavor
  models: UnifiedModel[]
  currentModel?: string
  efforts: EffortLevel[]
  currentEffort?: EffortLevel
  levels: AccessLevel[]
  currentLevel?: AccessLevel
  planSupported: boolean
  planOn: boolean
  fastSupported: boolean
  fastOn: boolean
  /** prompt 内容能力（图片/音频/内嵌上下文）；缺省视为支持（旧后端）。 */
  prompt?: {
    image: boolean
    audio: boolean
    embeddedContext: boolean
  }
}

/** 逐项可选的设置变更，缺省字段不动。 */
export interface SettingsPatch {
  model?: string
  effort?: EffortLevel
  level?: AccessLevel
  plan?: boolean
  fast?: boolean
}

/** 一轮的 token 计量（两端交集字段）。 */
export interface TurnUsage {
  inputTokens: number
  outputTokens: number
  cachedReadTokens: number
  totalTokens: number
}

export interface Session {
  id: number
  agentId: number
  agentName: string
  /** agent 的 runtime 方言，界面用它显示品牌图标。 */
  agentFlavor?: AgentFlavor
  /** agent 侧返回的 sessionId（uuid v7），用于后续的 session/prompt。 */
  acpSessionId: string
  title: string
  cwd: string
  state: SessionState
  stopReason: string
  messageCount: number
  /** 当前是否有活着的 agent 子进程。 */
  running: boolean
  settings?: SessionSettings
  /**
   * 最近一次上报的用量快照（轮末落库）。上下文水位只经 SSE 通知流过，
   * 会话一停就没了——靠它让未连接的会话也显示最近的占用比例与费用。
   */
  lastUsage?: {
    used: number
    size: number
    cost?: { amount: number; currency: string }
  }
  /** 活会话的斜杠命令清单，open 后可用。 */
  commands?: SlashCommand[]
  /** 工作目录当前的 git 分支（detached 时是短 hash），非 git 目录为空。 */
  gitBranch?: string
  /** 会话创建者的名字；**空表示 owner 自己**（他不在租户表里）。 */
  tenantName?: string
  createdAt: string
  updatedAt: string
}

/**
 * 一个已落盘的上传件（与 server/internal/service.UploadedFile 对齐）。
 * 上传件存在各自身份的家目录下，隔离由路径本身给。
 */
export interface UploadedFile {
  name: string
  /** 绝对路径，当普通的 @ 文件引用用。 */
  path: string
  size: number
  /** 内容 sha256（十六进制）。 */
  hash: string
  /** true 表示这次上传命中了已有的同内容文件，没有重新写盘。 */
  reused?: boolean
  uploadedAt: string
}

/** 概览页的聚合统计（后端算，前端只画）。 */
export interface OverviewStats {
  /** 全量口径的总计（不是当前页的和）。 */
  sessions: number
  messages: number
  /** 最近 N 天每天的会话数与消息数，已补齐没有数据的日子。 */
  daily: { date: string; sessions: number; messages: number }[]
  byAgent: { name: string; count: number }[]
  byState: { name: string; count: number }[]
}

/** agent 计划里的一项，来自 session/update 的 plan entries。 */
export interface PlanEntry {
  content: string
  priority?: string
  status?: "pending" | "in_progress" | "completed" | string
}

/** 权限请求的一个选项，agent 提供。 */
export interface PermissionOption {
  optionId: string
  name: string
  kind: string
}

/** 「计划完成」审批的一个去向；level 为空表示继续规划（拒绝）。 */
export interface PlanChoice {
  optionId: string
  level?: AccessLevel
}

/** 「计划完成，请求开始执行」的统一视图，由后端 adapter 从权限请求翻译。 */
export interface PlanReview {
  /** markdown 计划全文。 */
  plan: string
  choices: PlanChoice[]
}

/** 一次挂起的权限请求，agent 阻塞等用户裁决。 */
export interface PendingPermission {
  id: string
  toolCallId?: string
  toolKind?: string
  /** 只有 claude 带（如 "Write hello.txt"），codex 为空。 */
  title?: string
  rawInput?: unknown
  /** diff 等内容块，只有 claude 带。 */
  content?: unknown
  options: PermissionOption[]
  /** 非空时这是「计划完成」审批，渲染专门卡片。 */
  planReview?: PlanReview
}

/** 目录浏览器的一项与一次列取结果。files 只在文件选择模式下返回。 */
export interface DirEntry {
  name: string
  path: string
  /** 文件字节数；目录恒 0。 */
  size: number
  /** RFC3339 修改时间；stat 失败时缺省。 */
  modTime?: string
}

export interface DirListing {
  path: string
  parent?: string
  dirs: DirEntry[]
  files?: DirEntry[]
}

/** 选择器侧边栏的默认位置；key 是稳定标识，显示名走 i18n。 */
export interface FsPlace {
  key: string
  path: string
}

/** 工作区文件树的一个节点，与 server/internal/service.TreeEntry 对齐。 */
export interface TreeEntry {
  name: string
  path: string
  kind: "dir" | "file"
  size?: number
  /** true = 本次已展开（children 可信，空就是真的空）；否则前端懒加载。 */
  listed?: boolean
  children?: TreeEntry[]
}

export interface TreeListing {
  root: string
  entries: TreeEntry[]
  truncated?: boolean
}

/** 工作区文件预览内容，与 server/internal/service.WorkspaceFileView 对齐。 */
export interface WorkspaceFile {
  path: string
  name: string
  size: number
  content: string
  truncated?: boolean
  binary?: boolean
}

/** 工作区终端实例，与 service.TerminalInfo 对齐。 */
export interface TerminalInfo {
  id: string
  num: number
  running: boolean
}

/** 随消息上传的一张图片（base64，无 data: 前缀）。 */
export interface ImageAttachment {
  data: string
  mimeType: string
}

/** 发一轮的入参：文本 + 可选图片 + 可选 @ 引用文件路径。 */
export interface SendInput {
  content: string
  images?: ImageAttachment[]
  files?: string[]
  /** @ 引用的数据库：`<项目>/<环境>[/<库>[/<表>]]`，现状由后端查出后嵌入。 */
  datasources?: string[]
}

/**
 * tool_call 携带的文件位置（ACP 的 locations，follow-along 设计），
 * 界面用它跟随 agent 正在触碰的文件。路径是绝对路径。
 */
export interface ToolLocation {
  path: string
  /** 1 起算的行号，agent 可不带。 */
  line?: number
}

/** SSE 推来的事件类型。 */
export type StreamEventKind =
  | "user_message"
  | "message_chunk"
  | "thought_chunk"
  | "tool_call"
  | "permission"
  | "permission_done"
  | "plan"
  | "settings"
  | "usage"
  | "commands"
  | "elicitation"
  | "elicitation_done"
  | "turn_end"
  | "turn_done"
  | "session_title"
  | "task_update"
  | "error"

export interface StreamEvent {
  kind: StreamEventKind
  /** 会话内单调递增，用于去重。 */
  seq: number
  text?: string
  toolCallId?: string
  title?: string
  toolKind?: string
  status?: string
  rawInput?: unknown
  rawOutput?: unknown
  /** tool_call 的内容块（diff 等），流式期间即可渲染。 */
  content?: unknown
  /** tool_call 触碰的文件位置，供跟随视图与文件树状态点。 */
  locations?: ToolLocation[]
  /** 这次工具调用启动了一个子代理（claude 的 Agent/Task、codex 的 subAgentActivity）。 */
  isSubagent?: boolean
  /** 这条是某个子代理干的，值为它所挂的启动调用 id。 */
  subagentOf?: string
  /** codex 专用：子代理独立 thread 的 id 与 agent 路径。 */
  subagentThreadId?: string
  subagentPath?: string
  /** plan 事件：任务条目数组。 */
  entries?: unknown
  /** agent 自行切档/改配置后的最新统一设置视图。 */
  settings?: SessionSettings
  /** usage 事件：上下文用量（按占比展示）。 */
  used?: number
  size?: number
  /** usage 事件的累计费用，只有 claude 间歇带；缺省时界面不显示。 */
  cost?: { amount: number; currency: string }
  /** commands 事件：可用斜杠命令全量清单。 */
  commands?: SlashCommand[]
  /** turn_end 事件：本轮 token 计量。 */
  usage?: TurnUsage
  /** permission 事件：ID 用于回传裁决，options 是 agent 给的选项。 */
  permissionId?: string
  options?: PermissionOption[]
  planReview?: PlanReview
  elicitationId?: string
  stopReason?: string
  error?: string
  message?: Message
}

/**
 * 全局事件流（`/api/events`）上的一条事件。它与会话流是两回事：会话流送
 * 内容，这条流只送两种「与会话内容无关的信号」。
 */
export interface ServerEvent {
  kind: "hello" | "notify"
  /** hello：连上时后端的版本，变了就说明 app 更新过。 */
  version?: string
  /** notify：这条通知的由来。 */
  event?: NoticeEvent
  sessionId?: number
  sessionTitle?: string
  /** 给人看的一句话摘要（后端已折叠空白并截断）。 */
  text?: string
  /** 决策专用：系统通知上的按钮就是这些选项，按一下即裁决。 */
  permissionId?: string
  options?: PermissionOption[]
  elicitationId?: string
}

/**
 * 通知的由来。带 `_done` 的两条是**撤回信号**——事情已经在页面上处理掉了，
 * 挂在通知中心里的那条得收回去，否则它还在替一个结束了的请求要决定。
 */
export type NoticeEvent =
  | "permission"
  | "permission_done"
  | "elicitation"
  | "elicitation_done"
  | "turn_end"
  | "error"

/**
 * agent 交互式提问的一道题，从 elicitation 的 requestedSchema 解析而来。
 * options 来自 oneOf；otherFieldId 指向对应的自由输入字段（`__other`）。
 */
export interface ElicitationQuestion {
  id: string
  title: string
  description?: string
  required: boolean
  options: { value: string; description?: string }[]
  otherFieldId?: string
}

/** 一次挂起的交互式提问。schema 保留原文，作答后合成历史卡片时要用。 */
export interface PendingElicitation {
  id: string
  message: string
  schema: unknown
  questions: ElicitationQuestion[]
}

export type MessageRole = "user" | "agent" | "system"

/** 对应 ACP 的 session/update 各类 chunk。 */
export type MessageKind =
  | "text"
  | "thought"
  | "tool_call"
  | "tool_result"
  | "permission_request"
  | "plan"
  | "elicitation"

export interface Message {
  id: number
  sessionId: number
  role: MessageRole
  kind: MessageKind
  content: string
  /** tool_call / tool_result / plan 的结构化载荷，后端以 JSON 字符串存储。 */
  payload: Record<string, unknown> | null
  createdAt: string
}

/**
 * 对话索引的一格：一条用户提问的锚点与文案。
 *
 * `messageId` 与消息列表里的 id 同源，界面用它滚到对应消息；`text` 是
 * 提问首行的截断，或本机小模型对长提问的一句话精简（见 `digested`）。
 */
export interface OutlineEntry {
  messageId: number
  text: string
  createdAt: string
  /** 文案是模型精简过的（false 表示是提问首行的截断）。 */
  digested: boolean
  /** 这一轮 agent 回答的开头，给索引气泡当第二行；没答完就没有。 */
  reply?: string
}

/** 一条会话的完整提问索引。不分页——它抽的就是「整条会话有哪些提问」。 */
export interface SessionOutline {
  items: OutlineEntry[]
  /** 还在后台等模型精简的条数，非零说明过一会儿文案还会更好。 */
  pending: number
}

export interface Paged<T> {
  items: T[]
  total: number
  page: number
  pageSize: number
}

/**
 * 分页列表的请求参数，`Paged` 的对偶。跨端契约：六个列表端点都认这四个
 * （AGENTS.md §2），`sort` 的取值是后端排序白名单里的**数据库列名**。
 */
export interface PageQuery {
  page: number
  pageSize: number
  sort?: string
  order?: "asc" | "desc"
}

// ---- 技能库（磁盘为事实源，与 server/internal/service/skill*.go 对齐）----

export interface Skill {
  name: string
  description: string
  enabled: boolean
  updatedAt: string
  /** 被 AI 调用的累计次数（后端从 tool_call 信号统计）。 */
  usageCount: number
}

export interface SkillUsage {
  name: string
  count: number
  lastUsedAt: string
}

export interface SkillDetail extends Skill {
  /** frontmatter 之后的 markdown 正文；frontmatter 由后端组装。 */
  body: string
}

export interface SkillCreateInput {
  name: string
  description: string
  body: string
}

export interface SkillUpdateInput {
  description?: string
  body?: string
  enabled?: boolean
}

export interface SkillFile {
  path: string
  size: number
  binary: boolean
  updatedAt: string
}

export interface SkillFileContent extends SkillFile {
  content: string
}

export interface SkillScriptVar {
  name: string
  label: string
}

export interface SkillScript {
  path: string
  description: string
  usage: string
  args: SkillScriptVar[]
  opts: SkillScriptVar[]
  envs: SkillScriptVar[]
  runnable: boolean
}

export interface SkillScriptRunInput {
  path: string
  args: string[]
  opts: string[]
  env: Record<string, string>
}

export interface SkillScriptRunResult {
  exitCode: number
  stdout: string
  stderr: string
  durationMs: number
  timedOut: boolean
  truncated: boolean
}

// ── 多租户与项目（adr-007）────────────────────────────────────

/** 当前身份。owner 是本机访问，租户凭邀请链接换来的 cookie 认。 */
export interface Identity {
  authenticated: boolean
  owner: boolean
  /** 凭证认识但被 owner 关停——界面显示「无权访问」而不是「请用邀请链接」。 */
  revoked?: boolean
  tenantName?: string
  /** 租户的最上层工作目录；owner 不受目录限制，为空。 */
  root?: string
}

/** 一个局域网访客的身份与隔离单元（owner 视角，带邀请凭证）。 */
export interface Tenant {
  id: number
  name: string
  root: string
  disabled: boolean
  lastSeenAt?: string
  createdAt: string
  updatedAt: string
  /** 邀请凭证本身；只有 owner 拿得到。 */
  inviteToken: string
  /** 可直接转发的邀请链接（后端拼好局域网 IP 与端口）。 */
  inviteUrl: string
  /** 这条链接现在真的能发出去用；服务只监听本机时为 false。 */
  shareable: boolean
  sessionCount: number
}

/** 工作区里的一个仓库目录，名字是相对工作区根的路径（`组织/仓库`）。 */
export interface Project {
  name: string
  path: string
  remote?: string
  branch?: string
  sessionCount: number
  updatedAt: string
}

export type CloneState = "running" | "done" | "failed"

/** 一次后台克隆任务（内存态，服务重启即消失）。 */
export interface CloneTask {
  id: string
  name: string
  url: string
  path: string
  state: CloneState
  error?: string
  startedAt: string
  endedAt?: string
}

/** gh 能看到的可克隆仓库（个人账号名下的不在其中）。 */
export interface RemoteRepo {
  name: string
  description?: string
  private: boolean
  cloneUrl: string
  updatedAt: string
}

// 数据库数据源（adr-008）与 git 域的领域类型分别在 ./db 与 ./git，从这里
// 原样转出——文件按职责拆开，但 `@/types/acp` 仍是领域类型的单一入口，
// 调用方不必记住哪个类型住在哪个文件里。
export * from "./db"
export * from "./mcp"
export * from "./git"
