import { useCallback, useEffect, useRef, useState } from "react"

import { api, ApiError } from "@/lib/api"
import {
  INITIAL_CHAT_STATE,
  reduceChatEvent,
  type ChatState,
} from "@/lib/chat-events"
import type {
  OrchSession,
  OrchTask,
  SendInput,
  SettingsPatch,
  StreamEvent,
} from "@/types/acp"

const MESSAGE_PAGE = 60

/**
 * 编排主会话的流状态机（adr-006）：SSE 事件复用 lib/chat-events 的
 * reducer（事件形状与普通会话一致），额外消费 task_update 维护任务列表。
 * 返回形状与 useChat 结构兼容——ChatStream 等纯渲染组件可直接复用。
 * 刻意不动 use-chat：隔离契约要求编排不触碰普通会话链路。
 */
export function useOrchChat(orchId: number) {
  const [state, setState] = useState<ChatState>(INITIAL_CHAT_STATE)
  const [orchSession, setOrchSession] = useState<OrchSession | null>(null)
  const [tasks, setTasks] = useState<OrchTask[]>([])
  const lastSeq = useRef(0)

  const applyEvent = useCallback((ev: StreamEvent) => {
    if (ev.seq <= lastSeq.current) return
    lastSeq.current = ev.seq
    if (ev.kind === "task_update") {
      if (ev.task) {
        const task = ev.task
        setTasks((prev) => {
          const index = prev.findIndex((t) => t.id === task.id)
          if (index < 0) return [...prev, task]
          const next = [...prev]
          next[index] = task
          return next
        })
      }
      return
    }
    setState((prev) => reduceChatEvent(prev, ev))
  }, [])

  // 打开会话 + 拉历史与任务列表。orchId 为 0 是草稿态：首条消息才创建。
  useEffect(() => {
    let cancelled = false
    setState(INITIAL_CHAT_STATE)
    setOrchSession(null)
    setTasks([])
    lastSeq.current = 0

    if (!orchId) {
      setState((prev) => ({ ...prev, loading: false }))
      return
    }

    async function bootstrap() {
      try {
        const [session, history, taskList, settings] = await Promise.all([
          api.orchestrator.get(orchId),
          api.orchestrator.messages(orchId, { limit: MESSAGE_PAGE }),
          api.orchestrator.tasks(orchId),
          api.orchestrator.settings(orchId).catch(() => null),
        ])
        if (cancelled) return
        setOrchSession(session)
        setTasks(taskList)
        setState((prev) => ({
          ...prev,
          settings,
          messages: history.items,
          hasEarlier: history.items.length < history.total,
          busy: lastSeq.current === 0 ? session.state === "active" : prev.busy,
          loading: false,
        }))
      } catch (err) {
        if (cancelled) return
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
  }, [orchId])

  const refreshMessages = useCallback(async () => {
    try {
      const history = await api.orchestrator.messages(orchId, {
        limit: MESSAGE_PAGE,
      })
      setState((prev) => ({
        ...prev,
        messages: history.items,
        hasEarlier: history.items.length < history.total,
      }))
    } catch {
      // 拉不到就等下一轮，流式态还在。
    }
    try {
      const [session, taskList] = await Promise.all([
        api.orchestrator.get(orchId),
        api.orchestrator.tasks(orchId),
      ])
      setOrchSession(session)
      setTasks(taskList)
    } catch {
      // 详情拉不到不影响对话。
    }
  }, [orchId])

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
      const res = await api.orchestrator.messages(orchId, {
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
  }, [orchId])

  useEffect(() => {
    if (!orchId) return
    const source = new EventSource(api.orchestrator.eventsUrl(orchId))
    source.onopen = () => setState((prev) => ({ ...prev, connected: true }))
    source.onerror = () => setState((prev) => ({ ...prev, connected: false }))
    source.onmessage = (e) => {
      try {
        const ev = JSON.parse(e.data) as StreamEvent
        applyEvent(ev)
        if (ev.kind === "turn_done") void refreshMessages()
      } catch {
        // 半条 JSON 没有意义。
      }
    }
    return () => source.close()
  }, [orchId, applyEvent, refreshMessages])

  const send = useCallback(
    async (input: SendInput) => {
      setState((prev) => ({ ...prev, busy: true, error: null }))
      try {
        await api.orchestrator.send(orchId, input.content)
      } catch (err) {
        setState((prev) => ({
          ...prev,
          busy: false,
          error: (err as Error).message,
        }))
      }
    },
    [orchId]
  )

  const cancel = useCallback(async () => {
    try {
      await api.orchestrator.cancel(orchId)
    } catch (err) {
      setState((prev) => ({ ...prev, error: (err as Error).message }))
    }
  }, [orchId])

  /** 急停：中止主会话与全部在跑子任务。 */
  const stopAll = useCallback(async () => {
    try {
      await api.orchestrator.stop(orchId)
    } catch (err) {
      setState((prev) => ({ ...prev, error: (err as Error).message }))
    }
  }, [orchId])

  const applySettings = useCallback(
    async (patch: SettingsPatch) => {
      try {
        const settings = await api.orchestrator.applySettings(orchId, patch)
        setState((prev) => ({ ...prev, settings }))
      } catch (err) {
        setState((prev) => ({ ...prev, error: (err as Error).message }))
      }
    },
    [orchId]
  )

  const resolvePermission = useCallback(
    async (permissionId: string, optionId: string, choiceName: string) => {
      try {
        await api.orchestrator.resolvePermission(orchId, permissionId, optionId)
      } catch (err) {
        setState((prev) => ({ ...prev, error: (err as Error).message }))
      }
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
    [orchId]
  )

  const resolveElicitation = useCallback(
    async (
      elicitationId: string,
      action: "accept" | "decline" | "cancel",
      content?: Record<string, string>
    ) => {
      try {
        await api.orchestrator.resolveElicitation(
          orchId,
          elicitationId,
          action,
          content
        )
      } catch (err) {
        setState((prev) => ({ ...prev, error: (err as Error).message }))
      }
      setState((prev) =>
        prev.elicitation?.id === elicitationId
          ? { ...prev, elicitation: null }
          : prev
      )
    },
    [orchId]
  )

  return {
    ...state,
    orchSession,
    tasks,
    send,
    cancel,
    stopAll,
    applySettings,
    resolvePermission,
    resolveElicitation,
    loadEarlier,
  }
}
