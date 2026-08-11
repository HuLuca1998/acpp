import { useCallback, useEffect, useMemo, useState } from "react"
import { useNavigate } from "react-router"

import { api } from "@/lib/api"
import type { Agent, SendInput } from "@/types/acp"

/** 草稿页的模型选择：跨 agent 的分组清单，选模型即选定 agent。 */
export interface ModelChoice {
  key: string
  agentId: number
  /** 空串表示「该 agent 的 runtime 默认模型」（清单未探测到时的兜底项）。 */
  modelId: string
  label: string
  description?: string
}

export interface ModelGroup {
  agent: Agent
  choices: ModelChoice[]
}

/**
 * 每个 agent 一组：有探测缓存就列模型（配置页禁用的不列），
 * 没有就给一条「默认」兜底项。
 */
function modelChoicesOf(agent: Agent, fallbackLabel: string): ModelChoice[] {
  const enabled = agent.models.filter((m) => !m.disabled)
  if (enabled.length === 0) {
    return [
      {
        key: `${agent.id}:`,
        agentId: agent.id,
        modelId: "",
        label: fallbackLabel,
      },
    ]
  }
  return enabled.map((m) => ({
    key: `${agent.id}:${m.id}`,
    agentId: agent.id,
    modelId: m.id,
    label: m.name,
    description: m.description,
  }))
}

/**
 * 草稿会话的状态与创建流程：加载 agent 清单、跨 ACP 模型选择、工作目录，
 * 首条消息落地时才真正创建会话（create → 应用所选模型 → send → 跳转）。
 */
export function useDraftSession(enabled: boolean, defaultModelLabel: string) {
  const navigate = useNavigate()

  const [agents, setAgents] = useState<Agent[] | null>(null)
  const [choiceKey, setChoiceKey] = useState("")
  const [cwd, setCwd] = useState("")
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    // 老会话页也会挂这个 hook（hooks 不能条件调用），不启用就不拉清单。
    if (!enabled) return
    let cancelled = false
    api.agents
      .list()
      .then((res) => {
        if (cancelled) return
        setAgents(res.items)
        // 默认选第一个 agent 的第一个模型，多数场景零配置直接开聊。
        const first = res.items[0]
        if (first) {
          setChoiceKey(modelChoicesOf(first, defaultModelLabel)[0].key)
        }
      })
      .catch((err: Error) => {
        if (!cancelled) setError(err.message)
      })
    return () => {
      cancelled = true
    }
  }, [enabled, defaultModelLabel])

  const groups = useMemo<ModelGroup[]>(
    () =>
      (agents ?? []).map((agent) => ({
        agent,
        choices: modelChoicesOf(agent, defaultModelLabel),
      })),
    [agents, defaultModelLabel]
  )
  const selected = groups
    .flatMap((g) => g.choices)
    .find((c) => c.key === choiceKey)
  const selectedAgent = agents?.find((a) => a.id === selected?.agentId)

  /** 首条消息落地：创建 → 应用模型 → 发送 → replace 掉草稿路由。 */
  const start = useCallback(
    async (input: SendInput) => {
      if (!selected || creating) return
      setCreating(true)
      setError(null)
      try {
        const session = await api.sessions.create({
          agentId: selected.agentId,
          cwd: cwd.trim(),
        })
        if (selected.modelId) {
          await api.sessions.open(session.id)
          await api.sessions.applySettings(session.id, {
            model: selected.modelId,
          })
        }
        await api.sessions.send(session.id, input)
        void navigate(`/sessions/${session.id}`, { replace: true })
      } catch (err) {
        setError((err as Error).message)
        setCreating(false)
      }
    },
    [selected, creating, cwd, navigate]
  )

  return {
    agents,
    groups,
    choiceKey,
    setChoiceKey,
    selected,
    selectedAgent,
    cwd,
    setCwd,
    creating,
    error,
    start,
  }
}
