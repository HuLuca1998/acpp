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
  /** 活会话的斜杠命令清单，open 后可用。 */
  commands?: SlashCommand[]
  /** 工作目录当前的 git 分支（detached 时是短 hash），非 git 目录为空。 */
  gitBranch?: string
  createdAt: string
  updatedAt: string
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
}

export interface DirListing {
  path: string
  parent?: string
  dirs: DirEntry[]
  files?: DirEntry[]
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

/** 一条文件级 git 变更，与 service.GitFileChange 对齐；-1 表示无行数概念。 */
export interface GitFileChange {
  path: string
  status: string
  added: number
  deleted: number
}

export interface GitCommit {
  sha: string
  short: string
  subject: string
  author: string
  time: number
}

/** diff / commit 面板共享的一次性视图，与 service.GitOverview 对齐。 */
export interface GitOverview {
  isRepo: boolean
  branch?: string
  upstream?: string
  ahead: number
  behind: number
  files: GitFileChange[]
  commits: GitCommit[]
}

/** 单文件 diff 的两端全文，行级对齐在前端做。 */
export interface GitDiffView {
  path: string
  oldText: string
  newText: string
  binary?: boolean
  truncated?: boolean
}

export interface GitCommitDetail {
  commit: GitCommit
  files: GitFileChange[]
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
  | "message_saved"
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
  /** plan 事件：任务条目数组。 */
  entries?: unknown
  /** agent 自行切档/改配置后的最新统一设置视图。 */
  settings?: SessionSettings
  /** usage 事件：上下文用量（按占比展示）。 */
  used?: number
  size?: number
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

export interface Paged<T> {
  items: T[]
  total: number
  page: number
  pageSize: number
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
