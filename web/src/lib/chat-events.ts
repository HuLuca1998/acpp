import {
  parseElicitationSchema,
  type ElicitationSchema,
} from "@/lib/elicitation"
import type {
  Message,
  PendingElicitation,
  PendingPermission,
  PlanEntry,
  SendInput,
  Session,
  SessionSettings,
  SlashCommand,
  StreamEvent,
} from "@/types/acp"

/** 一次工具调用的实时状态，按 toolCallId 合并多条更新。 */
export interface LiveToolCall {
  id: string
  title: string
  kind: string
  status: string
  rawInput?: unknown
  rawOutput?: unknown
  content?: unknown
}

/** 上下文用量（usage 事件），按占比展示。 */
export interface ContextUsage {
  used: number
  size: number
}

/** 一轮进行中用户插话的排队条目：发出（被使用）前随时可撤回。 */
export interface QueuedMessage {
  id: number
  input: SendInput
}

/** 一次已裁决的权限请求（当前轮内展示用）。 */
export interface ResolvedPermission {
  id: string
  /** 工具标题（claude）或 kind 兜底。 */
  title: string
  /** 用户选中的选项名，空串表示取消。 */
  choice: string
}

export interface ChatState {
  session: Session | null
  messages: Message[]
  /** 正在流式到达的 agent 正文。 */
  streamingText: string
  /** 正在流式到达的思考过程。 */
  streamingThought: string
  liveTools: LiveToolCall[]
  /** 一轮正在跑。 */
  busy: boolean
  connected: boolean
  loading: boolean
  error: string | null
  /** 会话不存在（已被删除或 id 无效），应展示专门空态而非连接错误。 */
  notFound: boolean
  /** 非 end_turn 的结束原因，说明回答可能是残缺的。 */
  stopReason: string | null
  /** 活会话的统一设置视图，open 成功后可用。 */
  settings: SessionSettings | null
  /** 上下文用量，agent 每轮会推若干次。 */
  contextUsage: ContextUsage | null
  /** 可用斜杠命令，agent 推送全量清单，供输入框 "/" 补全。 */
  commands: SlashCommand[]
  /** 还有更早的消息没加载（消息按尾部分页）。 */
  hasEarlier: boolean
  /** agent 正在等用户作答的交互式提问。 */
  elicitation: PendingElicitation | null
  /** agent 正在等用户裁决的权限请求。 */
  permission: PendingPermission | null
  /** agent 的任务计划，每次 plan 事件整体替换；跨轮保留展示最终状态。 */
  plan: PlanEntry[] | null
  /** 当前轮内已裁决的权限请求。 */
  permissions: ResolvedPermission[]
  /** busy 期间排队的插话，轮次自然结束后合并发出；中止后保留等用户处置。 */
  queued: QueuedMessage[]
}

export const INITIAL_CHAT_STATE: ChatState = {
  session: null,
  messages: [],
  streamingText: "",
  streamingThought: "",
  liveTools: [],
  busy: false,
  connected: false,
  loading: true,
  error: null,
  notFound: false,
  stopReason: null,
  settings: null,
  contextUsage: null,
  commands: [],
  hasEarlier: false,
  elicitation: null,
  permission: null,
  plan: null,
  permissions: [],
  queued: [],
}

/** 把 elicitation 事件解析成结构化提问。 */
function parseElicitation(ev: StreamEvent): PendingElicitation | null {
  if (!ev.elicitationId) return null
  const schema = (ev.rawInput ?? null) as ElicitationSchema | null
  const questions = parseElicitationSchema(schema)
  if (questions.length === 0) return null
  return {
    id: ev.elicitationId,
    message: ev.text ?? "",
    schema,
    questions,
  }
}

/**
 * 事件 reducer：把一条 SSE 事件折进聊天状态。纯函数，seq 去重在调用方。
 *
 * 一轮只由 user_message 开启、由 turn_done/error 关闭。turn_done 之后
 * 迟到的本轮残余（中止后 agent 的收尾 chunk/tool_call）若被当作
 * "轮子在跑"会把 busy 永久卡死（还会进重放缓冲毒化每次刷新），
 * 这里在轮窗口外一律丢弃——权威内容反正在转录里。
 */
export function reduceChatEvent(prev: ChatState, ev: StreamEvent): ChatState {
  const inTurn = prev.busy
  switch (ev.kind) {
    case "user_message": {
      if (!ev.message) return prev
      // 草稿跳转/刷新的竞态：bootstrap 拉到的重建列表可能已含本轮
      // user 消息（重建 id 是小行号），SSE 重放又送来同内容的临时消息
      // （毫秒时间戳 id）——若列表末尾已是同内容的权威 user 消息，
      // 丢掉临时那条，否则同一句话显示两个气泡。
      const last = prev.messages[prev.messages.length - 1]
      const isDupOfRebuilt =
        ev.message.id > 1e12 &&
        last?.role === "user" &&
        last.id < 1e12 &&
        last.content === ev.message.content
      return {
        ...prev,
        busy: true,
        stopReason: null,
        messages: isDupOfRebuilt
          ? prev.messages
          : upsert(prev.messages, ev.message),
      }
    }

    case "message_chunk":
      if (!inTurn) return prev
      return {
        ...prev,
        streamingText: prev.streamingText + (ev.text ?? ""),
      }

    case "thought_chunk":
      if (!inTurn) return prev
      return {
        ...prev,
        streamingThought: prev.streamingThought + (ev.text ?? ""),
      }

    case "tool_call":
      if (!inTurn) return prev
      return {
        ...prev,
        liveTools: mergeTool(prev.liveTools, ev),
      }

    case "message_saved":
      // 落库的完整内容是权威版本，用它取代流式拼出来的文本。
      if (!ev.message) return prev
      return { ...prev, messages: upsert(prev.messages, ev.message) }

    case "settings":
      // agent 侧改了设置（或我们主动变更后广播回来），整体替换视图。
      if (!ev.settings) return prev
      return { ...prev, settings: ev.settings }

    case "usage":
      if (!ev.size) return prev
      return {
        ...prev,
        contextUsage: { used: ev.used ?? 0, size: ev.size },
      }

    case "commands":
      return { ...prev, commands: ev.commands ?? [] }

    case "plan": {
      // 计划整体替换；保留到下一轮开始，让用户能回看最终完成状态。
      if (!inTurn) return prev
      const entries = Array.isArray(ev.entries)
        ? (ev.entries as PlanEntry[])
        : null
      if (!entries) return prev
      return { ...prev, plan: entries }
    }

    case "permission":
      if (!inTurn) return prev
      if (!ev.permissionId || !ev.options?.length) return prev
      return {
        ...prev,
        permission: {
          id: ev.permissionId,
          toolCallId: ev.toolCallId,
          toolKind: ev.toolKind,
          title: ev.title,
          rawInput: ev.rawInput,
          content: ev.content,
          options: ev.options,
          planReview: ev.planReview,
        },
      }

    case "permission_done":
      // 裁决 / 超时后收起卡片；不匹配的 id 说明已经换了一个请求。
      if (prev.permission && prev.permission.id !== ev.permissionId) {
        return prev
      }
      return { ...prev, permission: null }

    case "elicitation":
      if (!inTurn) return prev
      return {
        ...prev,
        elicitation: parseElicitation(ev) ?? prev.elicitation,
      }

    case "elicitation_done":
      // 作答 / 超时 / agent 撤回后收起卡片；不匹配的 id 说明已经换了一轮。
      if (prev.elicitation && prev.elicitation.id !== ev.elicitationId) {
        return prev
      }
      return { ...prev, elicitation: null }

    case "turn_end":
      return {
        ...prev,
        stopReason:
          ev.stopReason && ev.stopReason !== "end_turn" ? ev.stopReason : null,
      }

    case "turn_done":
      // 本轮内容已经作为消息落库，清掉流式态避免重复渲染。
      // plan 留着展示最终状态，下一轮的 plan 事件会整体替换它。
      return {
        ...prev,
        busy: false,
        streamingText: "",
        streamingThought: "",
        liveTools: [],
        elicitation: null,
        permission: null,
        permissions: [],
      }

    case "error":
      return { ...prev, busy: false, error: ev.error ?? "unknown error" }

    default:
      return prev
  }
}

/** 把排队的多条插话合并成一轮的入参：文本空行连接，附件串联去重。 */
export function mergeInputs(inputs: SendInput[]): SendInput {
  return {
    content: inputs
      .map((i) => i.content)
      .filter(Boolean)
      .join("\n\n"),
    images: inputs.flatMap((i) => i.images ?? []),
    files: [...new Set(inputs.flatMap((i) => i.files ?? []))],
  }
}

function upsert(messages: Message[], incoming: Message): Message[] {
  const index = messages.findIndex((m) => m.id === incoming.id)
  if (index < 0) return [...messages, incoming]
  const next = [...messages]
  next[index] = incoming
  return next
}

function mergeTool(tools: LiveToolCall[], ev: StreamEvent): LiveToolCall[] {
  if (!ev.toolCallId) return tools

  const index = tools.findIndex((t) => t.id === ev.toolCallId)
  if (index < 0) {
    return [
      ...tools,
      {
        id: ev.toolCallId,
        title: ev.title ?? "",
        kind: ev.toolKind ?? "",
        status: ev.status ?? "pending",
        rawInput: ev.rawInput,
        rawOutput: ev.rawOutput,
        content: ev.content,
      },
    ]
  }

  // 后续更新只带变了的字段，空值不能覆盖已有内容。
  const next = [...tools]
  const current = next[index]
  next[index] = {
    ...current,
    title: ev.title || current.title,
    kind: ev.toolKind || current.kind,
    status: ev.status || current.status,
    rawInput: ev.rawInput ?? current.rawInput,
    rawOutput: ev.rawOutput ?? current.rawOutput,
    content: ev.content ?? current.content,
  }
  return next
}
