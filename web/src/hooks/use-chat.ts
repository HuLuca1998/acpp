import { useCallback, useEffect, useMemo, useRef, useState } from "react"

import { api, ApiError } from "@/lib/api"
import {
  INITIAL_CHAT_STATE,
  mergeInputs,
  reconcileMessages,
  reduceChatEvent,
  type ChatState,
} from "@/lib/chat/chat-events"
import {
  claimFirstSend,
  isOptimisticMessage,
  optimisticUserMessage,
  releaseFirstSend,
} from "@/lib/chat/first-send"
import { markSessionActive } from "@/lib/session-activity"
import type {
  Message,
  SendInput,
  SettingsPatch,
  StreamEvent,
} from "@/types/acp"

// 状态与条目类型定义在 lib/chat/chat-events.ts（纯 reducer 层），这里转发给消费方。
export type {
  ChatState,
  ContextUsage,
  LiveToolCall,
  QueuedMessage,
  ResolvedPermission,
} from "@/lib/chat/chat-events"

/**
 * 初次进入拉取的消息条数：一屏出头即可——打开只看最新，
 * 更早的滚动到顶时才自动补，加载与渲染成本都摊到需要时。
 */
const MESSAGE_PAGE = 30

/** 文本分片的合帧窗口：流式期间肉眼看不出 80ms 与逐包上屏的差别，
 *  渲染次数却从每包一次降到每秒十来次。 */
const CHUNK_FLUSH_MS = 80

export function useChat(sessionId: number) {
  const [state, setState] = useState<ChatState>(INITIAL_CHAT_STATE)
  // 去重游标：SSE 断线重连会重放本轮事件，靠 seq 跳过已处理的。
  const lastSeq = useRef(0)

  // 正文/思考分片的合帧缓冲：一轮能来几千条 chunk，每条都 setState 会让
  // 整棵聊天子树以网络到包的频率重渲染。攒住纯文本分片、按窗口合并上屏；
  // 其余事件（工具调用、权限、turn_done）到达时先冲掉缓冲再应用，
  // 事件之间的相对顺序不变。
  const chunkBuf = useRef({ text: "", thought: "" })
  const chunkTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const flushChunks = useCallback(() => {
    if (chunkTimer.current) {
      clearTimeout(chunkTimer.current)
      chunkTimer.current = null
    }
    const { text, thought } = chunkBuf.current
    if (!text && !thought) return
    chunkBuf.current = { text: "", thought: "" }
    // 走 reducer 而不是直接拼字符串：轮窗口外的残余分片该不该丢，
    // 判断逻辑只在 reducer 里有一份（seq 去重已在入口做过，传 0 即可）。
    setState((prev) => {
      let next = prev
      if (text) {
        next = reduceChatEvent(next, { kind: "message_chunk", text, seq: 0 })
      }
      if (thought) {
        next = reduceChatEvent(next, { kind: "thought_chunk", text: thought, seq: 0 })
      }
      return next
    })
  }, [])

  const applyEvent = useCallback(
    (ev: StreamEvent) => {
      if (ev.seq <= lastSeq.current) return
      lastSeq.current = ev.seq
      if (ev.kind === "message_chunk" || ev.kind === "thought_chunk") {
        const key = ev.kind === "message_chunk" ? "text" : "thought"
        chunkBuf.current[key] += ev.text ?? ""
        if (!chunkTimer.current) {
          chunkTimer.current = setTimeout(flushChunks, CHUNK_FLUSH_MS)
        }
        return
      }
      flushChunks()
      setState((prev) => reduceChatEvent(prev, ev))
    },
    [flushChunks]
  )

  // 打开会话 + 拉历史。sessionId 为 0 表示草稿态（会话还没创建），
  // 不打开、不订阅，只把 loading 清掉让页面直接可用。
  useEffect(() => {
    let cancelled = false
    setState(INITIAL_CHAT_STATE)
    lastSeq.current = 0

    if (!sessionId) {
      setState((prev) => ({ ...prev, loading: false }))
      return
    }

    // 草稿页交棒的首发（lib/first-send）：立即乐观渲染用户气泡与思考态，
    // 不等 agent 握手（派发由草稿页在后台继续，可能数十秒）。失败时收起
    // 思考态、把错误落到告警条上；已失败的交棒进入时直接按失败态渲染。
    const first = claimFirstSend(sessionId, (error) => {
      if (cancelled) return
      setState((prev) => ({ ...prev, busy: false, error }))
    })
    if (first) {
      setState((prev) => ({
        ...prev,
        loading: false,
        busy: first.error === undefined,
        error: first.error ?? null,
        messages: [optimisticUserMessage(sessionId, first.send.input)],
      }))
    }

    async function bootstrap() {
      try {
        // 只读加载：详情与历史都是毫秒级，不拉 agent 进程——
        // 查看记录零成本，真正发消息时后端才会连接（Send 内置幂等 Open）。
        const [session, history] = await Promise.all([
          api.sessions.get(sessionId),
          api.sessions.messages(sessionId, { limit: MESSAGE_PAGE }),
        ])
        if (cancelled) return
        // 空转录的 items 后端已兜底成 []，这里再保一道（老后端发 null）。
        const items = history.items ?? []
        setState((prev) => ({
          ...prev,
          session,
          settings: session.settings ?? null,
          commands: session.commands ?? [],
          // 上下文水位从会话快照起步：SSE 的 usage 只在轮内流过，没有它
          // 的话刚打开或已停止的会话看不到占用比例。实时事件到了再覆盖。
          contextUsage:
            prev.contextUsage ??
            (session.lastUsage && session.lastUsage.size > 0
              ? session.lastUsage
              : null),
          // 首发的乐观消息还没落转录：空历史不许冲掉本地已渲染的内容。
          messages: items.length > 0 ? items : prev.messages,
          hasEarlier: items.length < history.total,
          // busy 以后端权威状态起步：active 表示这一轮还在跑（刷新页面时
          // SSE 重放会接上流式），其余状态一律静止——别把 UI 卡在假 busy 上。
          // SSE 若已推进过状态（lastSeq>0）或首发乐观态已置忙，以那边为准，
          // 不许旧快照回拨。
          busy: prev.busy || (lastSeq.current === 0 && session.state === "active"),
          loading: false,
        }))
      } catch (err) {
        if (cancelled) return
        // 404 是"会话已不存在"，与 agent 连不上是两码事，分开表达。
        // 例外：首发失败时后端会回收从没连上过的空会话——此时保留乐观
        // 气泡与错误告警，好过一个"会话不存在"空页把用户刚说的话吞掉。
        if (err instanceof ApiError && err.status === 404) {
          setState((prev) =>
            prev.messages.length > 0
              ? { ...prev, loading: false }
              : { ...prev, loading: false, notFound: true }
          )
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
      releaseFirstSend(sessionId)
    }
  }, [sessionId])

  // 把「这条会话正在跑」告诉侧边栏：它的列表只在路由变化时拉，靠这条
  // 广播才能实时亮起呼吸点。卸载时清掉——页面都不在了，标记不该留着。
  useEffect(() => {
    markSessionActive(sessionId, state.busy)
  }, [sessionId, state.busy])
  useEffect(() => {
    return () => markSessionActive(sessionId, false)
  }, [sessionId])

  // turn 结束后重新拉取消息：后端从转录重建的列表是权威版本，
  // 会替换流式期间的临时用户消息与流式拼接内容。
  // 顺带刷新会话详情——agent 这一轮可能切了 git 分支。
  const refreshMessages = useCallback(async () => {
    try {
      const history = await api.sessions.messages(sessionId, {
        limit: MESSAGE_PAGE,
      })
      setState((prev) => {
        // 刚发出、服务端还没回声的乐观气泡不能被这份重建列表冲掉：
        // turn_done 同时触发本次拉取与排队插话的 flush，插话的气泡是在
        // 请求发出之后才进 messages 的，而重建列表按定义不含它——无条件
        // 覆盖会让它闪现一下就消失，等几秒后 user_message 回声才重现。
        const pending = prev.messages.filter(
          (m) =>
            isOptimisticMessage(m) &&
            !history.items.some(
              (h) => h.role === "user" && h.content === m.content
            )
        )
        // 内容没变的消息复用旧引用，memo 的消息条不因整表替换而重渲。
        const items = reconcileMessages(prev.messages, history.items)
        return {
          ...prev,
          messages: pending.length ? [...items, ...pending] : items,
          hasEarlier: history.items.length < history.total,
        }
      })
    } catch {
      // 拉不到就等下一轮，流式态还在，界面不至于空白。
    }
    try {
      const session = await api.sessions.get(sessionId)
      setState((prev) => ({
        ...prev,
        session,
        contextUsage:
          prev.contextUsage ??
          (session.lastUsage && session.lastUsage.size > 0
            ? session.lastUsage
            : null),
      }))
    } catch {
      // 会话详情拉不到不影响对话，下一轮再试。
    }
  }, [sessionId])

  // 「加载更早」：以当前最早一条重建消息的 id 为游标向前翻页。
  // 由滚动到顶的哨兵自动触发，ref 防重入（观察器可能连续报告可见）。
  const loadingEarlier = useRef(false)
  const loadEarlier = useCallback(async () => {
    if (loadingEarlier.current) return
    loadingEarlier.current = true
    try {
      let before = 0
      setState((prev) => {
        before = prev.messages[0]?.id ?? 0
        return prev
      })
      if (!before) return
      const res = await api.sessions.messages(sessionId, {
        limit: MESSAGE_PAGE,
        before,
      })
      setState((prev) => {
        const merged = [...res.items, ...prev.messages]
        return {
          ...prev,
          messages: merged,
          hasEarlier: res.items.length > 0 && merged.length < res.total,
        }
      })
    } catch (err) {
      setState((prev) => ({ ...prev, error: (err as Error).message }))
    } finally {
      loadingEarlier.current = false
    }
  }, [sessionId])

  // 订阅事件流。EventSource 自身会重连，重放的事件靠 seq 去重。
  useEffect(() => {
    if (!sessionId) return
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

  // 用户刚中止过：轮次停下后排队的插话不自动发出（中止=都停下），
  // 保留在排队条里等用户撤回或手动发送。下一次 send 复位。
  const cancelling = useRef(false)

  const send = useCallback(
    async (input: SendInput) => {
      cancelling.current = false
      // 乐观上屏：send 在后端要先把 agent 进程拉起来（空闲回收后的会话
      // 得重新握手 + session/load，几秒到几十秒），期间 HTTP 不返回、
      // SSE 也没有回声——不先画出这条气泡，用户就是对着一个吞掉自己
      // 消息的输入框干等。服务端回声到达时由 reducer 原位替换（那边认
      // payload.local 的乐观标记）。
      const optimistic = optimisticUserMessage(sessionId, input)
      setState((prev) => ({
        ...prev,
        busy: true,
        error: null,
        messages: [...prev.messages, optimistic],
      }))
      try {
        await api.sessions.send(sessionId, input)
      } catch (err) {
        // 发失败了就把乐观气泡收回去——留着它等于谎称消息已经发出。
        setState((prev) => ({
          ...prev,
          busy: false,
          error: (err as Error).message,
          messages: prev.messages.filter((m) => m.id !== optimistic.id),
        }))
      }
    },
    [sessionId]
  )

  /** busy 期间的插话入队：不直接发，浮在输入框上方，发出前可撤回。 */
  const queueSeq = useRef(0)
  const enqueue = useCallback((input: SendInput) => {
    queueSeq.current += 1
    const item = { id: queueSeq.current, input }
    setState((prev) => ({ ...prev, queued: [...prev.queued, item] }))
  }, [])

  /** 从排队里移除一条（撤回或转为立即发送时调用）。 */
  const removeQueued = useCallback((id: number) => {
    setState((prev) => ({
      ...prev,
      queued: prev.queued.filter((q) => q.id !== id),
    }))
  }, [])

  // 轮次自然结束（busy 落回 false）时把排队的插话合并成一轮发出。
  useEffect(() => {
    if (state.busy || state.queued.length === 0) return
    if (cancelling.current) return
    const inputs = state.queued.map((q) => q.input)
    setState((prev) => ({ ...prev, queued: [] }))
    void send(mergeInputs(inputs))
  }, [state.busy, state.queued, send])

  const cancel = useCallback(async () => {
    cancelling.current = true
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

  // 返回值引用只在状态真变时更换：宿主页面每次重渲染（比如打字的每个
  // 按键）都会重跑这里，展开成新对象会让 memo 的 ChatStream 形同虚设。
  return useMemo(
    () => ({
      ...state,
      send,
      enqueue,
      removeQueued,
      cancel,
      applySettings,
      resolvePermission,
      resolveElicitation,
      loadEarlier,
    }),
    [
      state,
      send,
      enqueue,
      removeQueued,
      cancel,
      applySettings,
      resolvePermission,
      resolveElicitation,
      loadEarlier,
    ]
  )
}
