import { useCallback, useEffect, useRef, useState } from "react"

import { api } from "@/lib/api"
import {
  INITIAL_CHAT_STATE,
  reduceChatEvent,
  type ChatState,
} from "@/lib/chat-events"
import type { StreamEvent } from "@/types/acp"

const MESSAGE_PAGE = 60

/**
 * 编排任务子会话的只读观察流（adr-006）：订阅任务 SSE + 转录重建历史，
 * 权限/提问可在面板上裁决。不能发消息——任务的输入只有主会话派发的
 * 那一句。返回形状与 useChat 结构兼容，ChatStream 直接复用。
 */
export function useTaskChat(taskId: number) {
  const [state, setState] = useState<ChatState>(INITIAL_CHAT_STATE)
  const lastSeq = useRef(0)

  const applyEvent = useCallback((ev: StreamEvent) => {
    if (ev.seq <= lastSeq.current) return
    lastSeq.current = ev.seq
    setState((prev) => reduceChatEvent(prev, ev))
  }, [])

  const refreshMessages = useCallback(async () => {
    try {
      const history = await api.orchestrator.taskMessages(taskId, {
        limit: MESSAGE_PAGE,
      })
      setState((prev) => ({
        ...prev,
        messages: history.items,
        hasEarlier: history.items.length < history.total,
      }))
    } catch {
      // 拉不到就等下一轮。
    }
  }, [taskId])

  useEffect(() => {
    let cancelled = false
    setState(INITIAL_CHAT_STATE)
    lastSeq.current = 0
    if (!taskId) return

    api.orchestrator
      .taskMessages(taskId, { limit: MESSAGE_PAGE })
      .then((history) => {
        if (cancelled) return
        setState((prev) => ({
          ...prev,
          messages: history.items,
          hasEarlier: history.items.length < history.total,
          loading: false,
        }))
      })
      .catch((err) => {
        if (cancelled) return
        setState((prev) => ({
          ...prev,
          loading: false,
          error: (err as Error).message,
        }))
      })

    return () => {
      cancelled = true
    }
  }, [taskId])

  useEffect(() => {
    if (!taskId) return
    const source = new EventSource(api.orchestrator.taskEventsUrl(taskId))
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
  }, [taskId, applyEvent, refreshMessages])

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
      const res = await api.orchestrator.taskMessages(taskId, {
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
    } finally {
      loadingEarlier.current = false
    }
  }, [taskId])

  const resolvePermission = useCallback(
    async (permissionId: string, optionId: string, choiceName: string) => {
      try {
        await api.orchestrator.taskResolvePermission(
          taskId,
          permissionId,
          optionId
        )
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
    [taskId]
  )

  const resolveElicitation = useCallback(
    async (
      elicitationId: string,
      action: "accept" | "decline" | "cancel",
      content?: Record<string, string>
    ) => {
      try {
        await api.orchestrator.taskResolveElicitation(
          taskId,
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
    [taskId]
  )

  const cancelTask = useCallback(async () => {
    try {
      await api.orchestrator.taskCancel(taskId)
    } catch (err) {
      setState((prev) => ({ ...prev, error: (err as Error).message }))
    }
  }, [taskId])

  return {
    ...state,
    loadEarlier,
    resolvePermission,
    resolveElicitation,
    cancelTask,
  }
}
