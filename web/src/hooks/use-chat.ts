import { useCallback, useEffect, useRef, useState } from "react"

import { api } from "@/lib/api"
import {
  parseElicitationSchema,
  type ElicitationSchema,
} from "@/lib/elicitation"
import type {
  Message,
  PendingElicitation,
  Session,
  SessionCaps,
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
  /** 非 end_turn 的结束原因，说明回答可能是残缺的。 */
  stopReason: string | null
  /** 活会话的可配置能力（模式/模型/配置项），open 成功后可用。 */
  caps: SessionCaps | null
  /** agent 正在等用户作答的交互式提问。 */
  elicitation: PendingElicitation | null
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
  stopReason: null,
  caps: null,
  elicitation: null,
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

        case "mode":
          // agent 侧切档（或我们主动切换后广播回来），同步到 caps。
          if (!ev.modeId || !prev.caps?.modes) return prev
          return {
            ...prev,
            caps: {
              ...prev.caps,
              modes: { ...prev.caps.modes, currentModeId: ev.modeId },
            },
          }

        case "config":
          if (!ev.configOptions) return prev
          return {
            ...prev,
            caps: { ...prev.caps, configOptions: ev.configOptions },
          }

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
          return {
            ...prev,
            busy: false,
            streamingText: "",
            streamingThought: "",
            liveTools: [],
            elicitation: null,
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
          caps: session.caps ?? null,
          messages: history.items,
          loading: false,
        }))
      } catch (err) {
        if (cancelled) return
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
  const refreshMessages = useCallback(async () => {
    try {
      const history = await api.sessions.messages(sessionId)
      setState((prev) => ({ ...prev, messages: history.items }))
    } catch {
      // 拉不到就等下一轮，流式态还在，界面不至于空白。
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

  const setMode = useCallback(
    async (modeId: string) => {
      try {
        const caps = await api.sessions.setMode(sessionId, modeId)
        setState((prev) => ({ ...prev, caps }))
      } catch (err) {
        setState((prev) => ({ ...prev, error: (err as Error).message }))
      }
    },
    [sessionId]
  )

  const setModel = useCallback(
    async (modelId: string) => {
      try {
        const caps = await api.sessions.setModel(sessionId, modelId)
        setState((prev) => ({ ...prev, caps }))
      } catch (err) {
        setState((prev) => ({ ...prev, error: (err as Error).message }))
      }
    },
    [sessionId]
  )

  const setConfig = useCallback(
    async (configId: string, value: string) => {
      try {
        const caps = await api.sessions.setConfig(sessionId, configId, value)
        setState((prev) => ({ ...prev, caps }))
      } catch (err) {
        setState((prev) => ({ ...prev, error: (err as Error).message }))
      }
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

  return { ...state, send, cancel, setMode, setModel, setConfig, resolveElicitation }
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
  }
  return next
}
