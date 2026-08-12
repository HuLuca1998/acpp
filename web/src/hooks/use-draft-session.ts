import { useCallback, useEffect, useMemo, useState } from "react"
import { useNavigate } from "react-router"

import { api } from "@/lib/api"
import type {
  Agent,
  SendInput,
  SessionSettings,
  SettingsPatch,
} from "@/types/acp"

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

/** 草稿态的默认设置选择：思考深度「高」，权限档「自动编辑」。 */
const DEFAULT_PATCH: SettingsPatch = { effort: "high", level: "auto-edit" }

/** 创建前把暂存选择过滤到所选 agent 真正支持的维度上。 */
function filterPatch(patch: SettingsPatch, agent: Agent | undefined) {
  const sk = agent?.skeleton
  const out: SettingsPatch = { ...patch }
  if (out.effort && !sk?.efforts?.includes(out.effort)) delete out.effort
  if (out.level && !sk?.levels?.includes(out.level)) delete out.level
  if (!sk?.planSupported) delete out.plan
  if (!sk?.fastSupported) delete out.fast
  return out
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
  // 模型之外的设置选择（思考深度/权限档/plan/fast）：本地暂存，
  // 创建会话时随模型一起一次性 apply。切换 agent 即重置——档位词汇
  // 虽是统一交集，但保留跨 agent 的旧选择容易造成误解。
  // 思考深度默认「高」；创建时按所选 agent 的骨架过滤不支持的维度。
  const [draftPatch, setDraftPatch] = useState<SettingsPatch>(DEFAULT_PATCH)

  useEffect(() => {
    // 老会话页也会挂这个 hook（hooks 不能条件调用），不启用就不拉清单。
    if (!enabled) return
    let cancelled = false
    api.agents
      .list()
      .then((res) => {
        if (cancelled) return
        setAgents(res.items)
        // 默认选 claude 的 default 模型（没有 claude 时退回第一个 agent
        // 的第一个模型），多数场景零配置直接开聊。
        const target =
          res.items.find((a) => a.flavor === "claude") ?? res.items[0]
        if (target) {
          const choices = modelChoicesOf(target, defaultModelLabel)
          const def = choices.find((c) => c.modelId === "default") ?? choices[0]
          setChoiceKey(def.key)
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

  /** 更新模型之外的设置选择（只在本地暂存，创建时统一 apply）。 */
  const applyDraftPatch = useCallback(async (patch: SettingsPatch) => {
    setDraftPatch((prev) => ({ ...prev, ...patch }))
  }, [])

  /** 换模型（choice.key 前缀是 agentId）：跨 agent 时重置其余设置选择。 */
  const selectChoice = useCallback((key: string) => {
    setChoiceKey((prev) => {
      if (prev.split(":")[0] !== key.split(":")[0]) {
        setDraftPatch(DEFAULT_PATCH)
      }
      return key
    })
  }, [])

  // 草稿态的设置视图：骨架来自所选 agent 探测缓存，当前值是本地选择。
  // 模型清单置空——模型由跨 agent 分组下拉承担，这里只渲染其余控件，
  // 与会话态的 SettingsSelectors 共用同一组件保证三态显示一致。
  const draftSettings = useMemo<SessionSettings | null>(() => {
    if (!selectedAgent) return null
    const sk = selectedAgent.skeleton
    return {
      flavor: selectedAgent.flavor,
      models: [],
      efforts: sk?.efforts ?? [],
      currentEffort: draftPatch.effort,
      levels: sk?.levels ?? [],
      currentLevel: draftPatch.level,
      planSupported: sk?.planSupported ?? false,
      planOn: draftPatch.plan ?? false,
      fastSupported: sk?.fastSupported ?? false,
      fastOn: draftPatch.fast ?? false,
    }
  }, [selectedAgent, draftPatch])

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
        // 模型与草稿态暂存的其余设置一次性应用（applySettings 内置幂等 Open）。
        const patch = filterPatch(draftPatch, selectedAgent)
        if (selected.modelId) {
          patch.model = selected.modelId
        }
        if (Object.keys(patch).length > 0) {
          await api.sessions.applySettings(session.id, patch)
        }
        await api.sessions.send(session.id, input)
        void navigate(`/sessions/${session.id}`, { replace: true })
      } catch (err) {
        setError((err as Error).message)
      } finally {
        // 成功路径也必须复位：/sessions/new 与 /sessions/:id 共用组件实例，
        // 跳转不会重挂，残留的 true 会把后续发送键永久锁成转圈。
        setCreating(false)
      }
    },
    [selected, creating, cwd, draftPatch, navigate]
  )

  return {
    agents,
    groups,
    choiceKey,
    setChoiceKey: selectChoice,
    selected,
    selectedAgent,
    cwd,
    setCwd,
    creating,
    error,
    draftSettings,
    applyDraftPatch,
    start,
  }
}
