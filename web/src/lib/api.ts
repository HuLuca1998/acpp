import type {
  Agent,
  CatalogInput,
  DirListing,
  Message,
  Paged,
  SendInput,
  Session,
  SessionSettings,
  SettingsPatch,
  TreeListing,
  WorkspaceFile,
} from "@/types/acp"

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
    /** 重探 agent 的统一设置能力（flavor 与模型清单），同步返回最新记录。 */
    probe: (id: number) =>
      request<Agent>(`/agents/${id}/probe`, { method: "POST" }),
    /** 配置页勾选：更新 models/commands 的启用状态。 */
    updateCatalog: (id: number, input: CatalogInput) =>
      request<Agent>(`/agents/${id}/catalog`, {
        method: "PUT",
        body: JSON.stringify(input),
      }),
  },

  sessions: {
    list: (params?: { agentId?: number; page?: number; pageSize?: number }) => {
      const qs = new URLSearchParams()
      if (params?.agentId) qs.set("agentId", String(params.agentId))
      if (params?.page) qs.set("page", String(params.page))
      if (params?.pageSize) qs.set("pageSize", String(params.pageSize))
      const s = qs.toString()
      return request<Paged<Session>>(`/sessions${s ? `?${s}` : ""}`)
    },
    get: (id: number) => request<Session>(`/sessions/${id}`),
    create: (input: { agentId: number; title?: string; cwd?: string }) =>
      request<Session>("/sessions", {
        method: "POST",
        body: JSON.stringify(input),
      }),
    remove: (id: number) =>
      request<null>(`/sessions/${id}`, { method: "DELETE" }),
    /** 消息列表：limit 取尾部 N 条，before 是「加载更早」的游标。 */
    messages: (id: number, params?: { limit?: number; before?: number }) => {
      const qs = new URLSearchParams()
      if (params?.limit) qs.set("limit", String(params.limit))
      if (params?.before) qs.set("before", String(params.before))
      const s = qs.toString()
      return request<Paged<Message>>(
        `/sessions/${id}/messages${s ? `?${s}` : ""}`
      )
    },

    /** 拉起 agent 子进程并完成 ACP 握手，重复调用是安全的。 */
    open: (id: number) =>
      request<Session>(`/sessions/${id}/open`, { method: "POST" }),
    /** 发一轮对话（文本 + 可选图片/文件引用）。立即返回，agent 的回复走 SSE。 */
    send: (id: number, input: SendInput) =>
      request<Message>(`/sessions/${id}/send`, {
        method: "POST",
        body: JSON.stringify(input),
      }),
    cancel: (id: number) =>
      request<null>(`/sessions/${id}/cancel`, { method: "POST" }),
    /** 应用统一设置变更（模型/思考深度/权限档/plan/fast，逐项可选）。 */
    applySettings: (id: number, patch: SettingsPatch) =>
      request<SessionSettings>(`/sessions/${id}/settings`, {
        method: "PUT",
        body: JSON.stringify(patch),
      }),
    /** 把权限裁决回给阻塞中的 agent；optionId 空串表示取消。 */
    resolvePermission: (id: number, permissionId: string, optionId: string) =>
      request<null>(`/sessions/${id}/permission`, {
        method: "POST",
        body: JSON.stringify({ permissionId, optionId }),
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

    /** 工作区文件树：path 为空从会话 cwd 开始，depth ≤ 2。 */
    workspaceTree: (id: number, params?: { path?: string; depth?: number }) => {
      const qs = new URLSearchParams()
      if (params?.path) qs.set("path", params.path)
      if (params?.depth) qs.set("depth", String(params.depth))
      const s = qs.toString()
      return request<TreeListing>(
        `/sessions/${id}/fs/entries${s ? `?${s}` : ""}`
      )
    },
    /** 工作区文件内容（预览用，路径限制在会话 cwd 内）。 */
    workspaceFile: (id: number, path: string) =>
      request<WorkspaceFile>(
        `/sessions/${id}/fs/file?path=${encodeURIComponent(path)}`
      ),
  },

  fs: {
    /** 列目录（withFiles 时连同文件），供选择器导航；path 为空从家目录开始。 */
    dirs: (path?: string, withFiles?: boolean) => {
      const params = new URLSearchParams()
      if (path) params.set("path", path)
      if (withFiles) params.set("files", "1")
      const qs = params.toString()
      return request<DirListing>(`/fs/dirs${qs ? `?${qs}` : ""}`)
    },
  },
}
