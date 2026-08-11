import type { Agent, Message, Paged, Session, SessionCaps } from "@/types/acp"

/** 开发环境走 vite proxy，生产环境同源。可用 VITE_API_BASE 覆盖。 */
const BASE = import.meta.env.VITE_API_BASE ?? "/api"

export class ApiError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = "ApiError"
    this.status = status
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    headers: {
      "Content-Type": "application/json",
      ...init?.headers,
    },
    ...init,
  })

  const body = await res.json().catch(() => null)

  if (!res.ok) {
    throw new ApiError(res.status, body?.error ?? res.statusText)
  }

  return body?.data as T
}

export const api = {
  health: () => request<{ status: string; version: string }>("/health"),

  agents: {
    list: () => request<Paged<Agent>>("/agents"),
    get: (id: number) => request<Agent>(`/agents/${id}`),
    create: (input: Partial<Agent>) =>
      request<Agent>("/agents", {
        method: "POST",
        body: JSON.stringify(input),
      }),
    update: (id: number, input: Partial<Agent>) =>
      request<Agent>(`/agents/${id}`, {
        method: "PUT",
        body: JSON.stringify(input),
      }),
    remove: (id: number) =>
      request<null>(`/agents/${id}`, { method: "DELETE" }),
  },

  sessions: {
    list: (params?: { agentId?: number }) => {
      const qs = params?.agentId ? `?agentId=${params.agentId}` : ""
      return request<Paged<Session>>(`/sessions${qs}`)
    },
    get: (id: number) => request<Session>(`/sessions/${id}`),
    create: (input: { agentId: number; title?: string; cwd?: string }) =>
      request<Session>("/sessions", {
        method: "POST",
        body: JSON.stringify(input),
      }),
    remove: (id: number) =>
      request<null>(`/sessions/${id}`, { method: "DELETE" }),
    messages: (id: number) =>
      request<Paged<Message>>(`/sessions/${id}/messages`),

    /** 拉起 agent 子进程并完成 ACP 握手，重复调用是安全的。 */
    open: (id: number) =>
      request<Session>(`/sessions/${id}/open`, { method: "POST" }),
    /** 发一轮对话。立即返回，agent 的回复走 SSE。 */
    send: (id: number, content: string) =>
      request<Message>(`/sessions/${id}/send`, {
        method: "POST",
        body: JSON.stringify({ content }),
      }),
    cancel: (id: number) =>
      request<null>(`/sessions/${id}/cancel`, { method: "POST" }),
    /** 切换审批/沙箱模式，返回最新能力快照。 */
    setMode: (id: number, modeId: string) =>
      request<SessionCaps>(`/sessions/${id}/mode`, {
        method: "POST",
        body: JSON.stringify({ modeId }),
      }),
    /** 切换模型，返回最新能力快照。 */
    setModel: (id: number, modelId: string) =>
      request<SessionCaps>(`/sessions/${id}/model`, {
        method: "POST",
        body: JSON.stringify({ modelId }),
      }),
    /** 设置配置项（协作模式、推理档等），返回最新能力快照。 */
    setConfig: (id: number, configId: string, value: string) =>
      request<SessionCaps>(`/sessions/${id}/config`, {
        method: "POST",
        body: JSON.stringify({ configId, value }),
      }),
    /** 把交互式提问的作答回给阻塞中的 agent。 */
    resolveElicitation: (
      id: number,
      elicitationId: string,
      action: "accept" | "decline" | "cancel",
      content?: Record<string, string>
    ) =>
      request<null>(`/sessions/${id}/elicitation`, {
        method: "POST",
        body: JSON.stringify({ elicitationId, action, content }),
      }),
    /** SSE 事件流地址。 */
    eventsUrl: (id: number) => `${BASE}/sessions/${id}/events`,
  },
}
