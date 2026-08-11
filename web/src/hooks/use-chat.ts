import { useCallback, useEffect, useRef, useState } from "react"

import { api, ApiError } from "@/lib/api"
import {
  parseElicitationSchema,
  type ElicitationSchema,
} from "@/lib/elicitation"
import type {
  Message,
  PendingElicitation,
  PendingPermission,
  PlanEntry,
  Session,
  SessionSettings,
  SettingsPatch,
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
  /** agent 正在等用户作答的交互式提问。 */
  elicitation: PendingElicitation | null
  /** agent 正在等用户裁决的权限请求。 */
  permission: PendingPermission | null
  /** agent 的任务计划，每次 plan 事件整体替换；跨轮保留展示最终状态。 */
  plan: PlanEntry[] | null
  /** 当前轮内已裁决的权限请求。 */
  permissions: ResolvedPermission[]
}

const INITIAL: ChatState = {
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
  elicitation: null,
  permission: null,
  plan: null,
  permissions: [],
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

export function useChat(sessionId: number) {
  const [state, setState] = useState<ChatState>(INITIAL)
  // 去重游标：SSE 断线重连会重放本轮事件，靠 seq 跳过已处理的。
  const lastSeq = useRef(0)

  const applyEvent = useCallback((ev: StreamEvent) => {
    if (ev.seq <= lastSeq.current) return
    lastSeq.current = ev.seq

    setState((prev) => {
      switch (ev.kind) {
        case "user_message":
          if (!ev.message) return prev
          return {
            ...prev,
            busy: true,
            stopReason: null,
            messages: upsert(prev.messages, ev.message),
          }

        case "message_chunk":
          return {
            ...prev,
            busy: true,
            streamingText: prev.streamingText + (ev.text ?? ""),
          }

        case "thought_chunk":
          return {
            ...prev,
            busy: true,
            streamingThought: prev.streamingThought + (ev.text ?? ""),
          }

        case "tool_call":
          return {
            ...prev,
            busy: true,
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

        case "plan": {
          // 计划整体替换；保留到下一轮开始，让用户能回看最终完成状态。
          const entries = Array.isArray(ev.rawInput)
            ? (ev.rawInput as PlanEntry[])
            : null
          if (!entries) return prev
          return { ...prev, busy: true, plan: entries }
        }

        case "permission":
          if (!ev.permissionId || !ev.options?.length) return prev
          return {
            ...prev,
            busy: true,
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
              ev.stopReason && ev.stopReason !== "end_turn"
                ? ev.stopReason
                : null,
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
    })
  }, [])

  // 打开会话 + 拉历史。
  useEffect(() => {
    let cancelled = false
    setState(INITIAL)
    lastSeq.current = 0

    async function bootstrap() {
      try {
        const [session, history] = await Promise.all([
          api.sessions.open(sessionId),
          api.sessions.messages(sessionId),
        ])
        if (cancelled) return
        setState((prev) => ({
          ...prev,
          session,
          settings: session.settings ?? null,
          messages: history.items,
          loading: false,
        }))
      } catch (err) {
        if (cancelled) return
        // 404 是"会话已不存在"，与 agent 连不上是两码事，分开表达。
        if (err instanceof ApiError && err.status === 404) {
          setState((prev) => ({ ...prev, loading: false, notFound: true }))
          return
        }
        setState((prev) => ({
          ...prev,
          loading: false,
          error: (err as Error).message,
        }))
      }
    }

    void bootstrap()
    return () => {
      cancelled = true
    }
  }, [sessionId])

  // turn 结束后重新拉取消息：后端从转录重建的列表是权威版本，
  // 会替换流式期间的临时用户消息与流式拼接内容。
  // 顺带刷新会话详情——agent 这一轮可能切了 git 分支。
  const refreshMessages = useCallback(async () => {
    try {
      const history = await api.sessions.messages(sessionId)
      setState((prev) => ({ ...prev, messages: history.items }))
    } catch {
      // 拉不到就等下一轮，流式态还在，界面不至于空白。
    }
    try {
      const session = await api.sessions.get(sessionId)
      setState((prev) => ({ ...prev, session }))
    } catch {
      // 会话详情拉不到不影响对话，下一轮再试。
    }
  }, [sessionId])

  // 订阅事件流。EventSource 自身会重连，重放的事件靠 seq 去重。
  useEffect(() => {
    const source = new EventSource(api.sessions.eventsUrl(sessionId))

    source.onopen = () => setState((prev) => ({ ...prev, connected: true }))
    source.onerror = () => setState((prev) => ({ ...prev, connected: false }))
    source.onmessage = (e) => {
      try {
        const ev = JSON.parse(e.data) as StreamEvent
        applyEvent(ev)
        if (ev.kind === "turn_done") void refreshMessages()
      } catch {
        // 半条 JSON 没有意义，丢掉即可。
      }
    }

    return () => source.close()
  }, [sessionId, applyEvent, refreshMessages])

  const send = useCallback(
    async (content: string) => {
      setState((prev) => ({ ...prev, busy: true, error: null }))
      try {
        await api.sessions.send(sessionId, content)
      } catch (err) {
        setState((prev) => ({
          ...prev,
          busy: false,
          error: (err as Error).message,
        }))
      }
    },
    [sessionId]
  )

  const cancel = useCallback(async () => {
    try {
      await api.sessions.cancel(sessionId)
    } catch (err) {
      setState((prev) => ({ ...prev, error: (err as Error).message }))
    }
  }, [sessionId])

  const applySettings = useCallback(
    async (patch: SettingsPatch) => {
      try {
        const settings = await api.sessions.applySettings(sessionId, patch)
        setState((prev) => ({ ...prev, settings }))
      } catch (err) {
        setState((prev) => ({ ...prev, error: (err as Error).message }))
      }
    },
    [sessionId]
  )

  const resolvePermission = useCallback(
    async (permissionId: string, optionId: string, choiceName: string) => {
      try {
        await api.sessions.resolvePermission(sessionId, permissionId, optionId)
      } catch (err) {
        setState((prev) => ({ ...prev, error: (err as Error).message }))
      }
      // 收起卡片并留一条本轮内的裁决记录。
      setState((prev) => {
        if (prev.permission?.id !== permissionId) return prev
        return {
          ...prev,
          permission: null,
          permissions: [
            ...prev.permissions,
            {
              id: permissionId,
              title: prev.permission.title || prev.permission.toolKind || "",
              choice: choiceName,
            },
          ],
        }
      })
    },
    [sessionId]
  )

  const resolveElicitation = useCallback(
    async (
      elicitationId: string,
      action: "accept" | "decline" | "cancel",
      content?: Record<string, string>
    ) => {
      try {
        await api.sessions.resolveElicitation(
          sessionId,
          elicitationId,
          action,
          content
        )
      } catch (err) {
        setState((prev) => ({ ...prev, error: (err as Error).message }))
      }
      // 收起卡片，同时把问答立即落成一条临时历史消息——
      // turn 结束后 refreshMessages 会用转录重建的权威版本替换它。
      setState((prev) => {
        if (prev.elicitation?.id !== elicitationId) return prev
        const answered: Message = {
          id: Date.now(),
          sessionId,
          role: "agent",
          kind: "elicitation",
          content: prev.elicitation.message,
          payload: {
            action,
            schema: prev.elicitation.schema,
            answers: content ?? {},
          },
          createdAt: new Date().toISOString(),
        }
        return {
          ...prev,
          elicitation: null,
          messages: [...prev.messages, answered],
        }
      })
    },
    [sessionId]
  )

  return {
    ...state,
    send,
    cancel,
    applySettings,
    resolvePermission,
    resolveElicitation,
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
