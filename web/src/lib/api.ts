import type {
  Agent,
  CatalogInput,
  DirEntry,
  DirListing,
  EnvInfo,
  EnvInstallResult,
  GitCommitDetail,
  GitDiffView,
  GitOverview,
  Message,
  OrchSession,
  OrchTask,
  Paged,
  Role,
  RoleInput,
  SendInput,
  Session,
  SessionSettings,
  SettingsPatch,
  Skill,
  SkillCreateInput,
  SkillDetail,
  SkillFile,
  SkillFileContent,
  SkillScript,
  SkillScriptRunInput,
  SkillScriptRunResult,
  SkillUpdateInput,
  SkillUsage,
  SystemInfo,
  TerminalInfo,
  TreeListing,
  UpdateInfo,
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

  system: {
    get: () => request<SystemInfo>("/system"),
    /** 迁移数据目录（拷贝式，重启后端后生效）。 */
    migrateDataDir: (dataDir: string) =>
      request<SystemInfo>("/system/data-dir", {
        method: "PUT",
        body: JSON.stringify({ dataDir }),
      }),
    /** 环境体检：依赖是否就位（brew/node/适配器/CLI）。 */
    env: () => request<EnvInfo>("/system/env"),
    /** 一键安装缺失依赖；key 只认后端白名单，安装可能耗时数分钟。 */
    envInstall: (key: string) =>
      request<EnvInstallResult>("/system/env/install", {
        method: "POST",
        body: JSON.stringify({ key }),
      }),
    /** 版本检查（缓存结果；force 现查 GitHub Releases）。 */
    update: (force?: boolean) =>
      request<UpdateInfo>(`/system/update${force ? "?force=1" : ""}`),
    /** 一键更新：下载最新 release 替换 .app 并自动重启（仅桌面版）。 */
    updateApply: () =>
      request<{ message: string }>("/system/update/apply", { method: "POST" }),
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

    /**
     * 增量读转录 JSONL（logs 面板轮询用）：转录 append-only，用 Range
     * 字节偏移续读。返回新增文本与下一次的偏移；无新内容返回 null。
     * reset=true 表示拿到的是全量而非增量，调用方必须整体替换而不是追加。
     */
    transcriptChunk: async (
      id: number,
      offset: number
    ): Promise<{
      chunk: string
      nextOffset: number
      reset: boolean
    } | null> => {
      // no-store 是必须的：ServeFile 带 Last-Modified，浏览器会把首个 200
      // 全量缓存起来，之后带 Range 的请求被缓存直接用 200 全量应答——
      // 前端把全量当增量追加，日志每轮翻倍。
      const res = await fetch(`${BASE}/sessions/${id}/transcript`, {
        headers: offset > 0 ? { Range: `bytes=${offset}-` } : {},
        cache: "no-store",
      })
      // 416：偏移已到文件末尾，没有新内容。
      if (res.status === 416) return null
      if (!res.ok) throw new ApiError(res.status, res.statusText)
      const chunk = await res.text()
      if (chunk === "") return null
      // 偏移按字节推进（JSONL 里的中文是多字节，不能用字符数）。
      const bytes = new TextEncoder().encode(chunk).length
      // 防御：请求了 Range 却收到 200（中间层没执行 Range），这是全量。
      const reset = offset > 0 && res.status === 200
      return { chunk, nextOffset: reset ? bytes : offset + bytes, reset }
    },

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

    /** git 汇总：分支/领先落后/变更文件/未推送 commit，diff 与 commit 面板共享。 */
    gitOverview: (id: number) =>
      request<GitOverview>(`/sessions/${id}/git/overview`),
    /** 工作区单文件 diff（HEAD 版对工作区版的两端全文）。 */
    gitDiff: (id: number, path: string) =>
      request<GitDiffView>(
        `/sessions/${id}/git/diff?path=${encodeURIComponent(path)}`
      ),
    /** 提交详情（文件清单）。 */
    gitCommit: (id: number, sha: string) =>
      request<GitCommitDetail>(`/sessions/${id}/git/commits/${sha}`),
    /** 某文件在一条提交前后的全文。 */
    gitCommitFile: (id: number, sha: string, path: string) =>
      request<GitDiffView>(
        `/sessions/${id}/git/commits/${sha}?path=${encodeURIComponent(path)}`
      ),

    /** 工作区终端：REST 管生命周期，ws 走 terminalWsUrl。 */
    terminalCreate: (id: number) =>
      request<TerminalInfo>(`/sessions/${id}/terminals`, { method: "POST" }),
    terminalList: (id: number) =>
      request<TerminalInfo[]>(`/sessions/${id}/terminals`),
    terminalRemove: (id: number, tid: string) =>
      request<null>(`/sessions/${id}/terminals/${tid}`, { method: "DELETE" }),
    terminalWsUrl: (id: number, tid: string) => {
      // 开发态直连后端：vite 的 ws 代理在 HMR/重启后会僵死（输入静默丢失），
      // 端口是项目固定约定（根 AGENTS.md §4.0），后端 ws 升级已放行本源。
      if (import.meta.env.DEV && !BASE.startsWith("http")) {
        return `ws://127.0.0.1:48080/api/sessions/${id}/terminals/${tid}/ws`
      }
      const proto = location.protocol === "https:" ? "wss" : "ws"
      const base = BASE.startsWith("http")
        ? BASE.replace(/^http/, "ws")
        : `${proto}://${location.host}${BASE}`
      return `${base}/sessions/${id}/terminals/${tid}/ws`
    },
  },

  skills: {
    list: () => request<Paged<Skill>>("/skills"),
    get: (name: string) => request<SkillDetail>(`/skills/${name}`),
    create: (input: SkillCreateInput) =>
      request<SkillDetail>("/skills", {
        method: "POST",
        body: JSON.stringify(input),
      }),
    update: (name: string, input: SkillUpdateInput) =>
      request<SkillDetail>(`/skills/${name}`, {
        method: "PUT",
        body: JSON.stringify(input),
      }),
    remove: (name: string) =>
      request<{ deleted: boolean }>(`/skills/${name}`, { method: "DELETE" }),

    files: (name: string) => request<Paged<SkillFile>>(`/skills/${name}/files`),
    file: (name: string, path: string) =>
      request<SkillFileContent>(`/skills/${name}/files/${path}`),
    putFile: (name: string, path: string, content: string) =>
      request<SkillFile>(`/skills/${name}/files/${path}`, {
        method: "PUT",
        body: JSON.stringify({ content }),
      }),
    removeFile: (name: string, path: string) =>
      request<{ deleted: boolean }>(`/skills/${name}/files/${path}`, {
        method: "DELETE",
      }),

    usage: () => request<Paged<SkillUsage>>("/skills/usage"),

    scripts: (name: string) =>
      request<Paged<SkillScript>>(`/skills/${name}/scripts`),
    runScript: (name: string, input: SkillScriptRunInput) =>
      request<SkillScriptRunResult>(`/skills/${name}/scripts/run`, {
        method: "POST",
        body: JSON.stringify(input),
      }),
  },

  roles: {
    list: () => request<Role[]>("/roles"),
    get: (id: number) => request<Role>(`/roles/${id}`),
    create: (input: RoleInput) =>
      request<Role>("/roles", { method: "POST", body: JSON.stringify(input) }),
    update: (id: number, input: RoleInput) =>
      request<Role>(`/roles/${id}`, {
        method: "PUT",
        body: JSON.stringify(input),
      }),
    remove: (id: number) => request<null>(`/roles/${id}`, { method: "DELETE" }),
  },

  orchestrator: {
    list: (params?: { page?: number; pageSize?: number }) => {
      const qs = new URLSearchParams()
      if (params?.page) qs.set("page", String(params.page))
      if (params?.pageSize) qs.set("pageSize", String(params.pageSize))
      const s = qs.toString()
      return request<Paged<OrchSession>>(
        `/orchestrator/sessions${s ? `?${s}` : ""}`
      )
    },
    get: (id: number) => request<OrchSession>(`/orchestrator/sessions/${id}`),
    create: (input: { agentId: number; cwd?: string; title?: string }) =>
      request<OrchSession>("/orchestrator/sessions", {
        method: "POST",
        body: JSON.stringify(input),
      }),
    remove: (id: number) =>
      request<null>(`/orchestrator/sessions/${id}`, { method: "DELETE" }),
    send: (id: number, content: string) =>
      request<Message>(`/orchestrator/sessions/${id}/send`, {
        method: "POST",
        body: JSON.stringify({ content }),
      }),
    cancel: (id: number) =>
      request<null>(`/orchestrator/sessions/${id}/cancel`, { method: "POST" }),
    /** 急停：中止主会话与全部在跑子任务。 */
    stop: (id: number) =>
      request<null>(`/orchestrator/sessions/${id}/stop`, { method: "POST" }),
    settings: (id: number) =>
      request<SessionSettings>(`/orchestrator/sessions/${id}/settings`),
    applySettings: (id: number, patch: SettingsPatch) =>
      request<SessionSettings>(`/orchestrator/sessions/${id}/settings`, {
        method: "PUT",
        body: JSON.stringify(patch),
      }),
    resolvePermission: (id: number, permissionId: string, optionId: string) =>
      request<null>(`/orchestrator/sessions/${id}/permission`, {
        method: "POST",
        body: JSON.stringify({ permissionId, optionId }),
      }),
    resolveElicitation: (
      id: number,
      elicitationId: string,
      action: "accept" | "decline" | "cancel",
      content?: Record<string, string>
    ) =>
      request<null>(`/orchestrator/sessions/${id}/elicitation`, {
        method: "POST",
        body: JSON.stringify({ elicitationId, action, content }),
      }),
    messages: (id: number, params?: { limit?: number; before?: number }) => {
      const qs = new URLSearchParams()
      if (params?.limit) qs.set("limit", String(params.limit))
      if (params?.before) qs.set("before", String(params.before))
      const s = qs.toString()
      return request<Paged<Message>>(
        `/orchestrator/sessions/${id}/messages${s ? `?${s}` : ""}`
      )
    },
    eventsUrl: (id: number) => `${BASE}/orchestrator/sessions/${id}/events`,

    tasks: (id: number) =>
      request<OrchTask[]>(`/orchestrator/sessions/${id}/tasks`),
    taskMessages: (
      tid: number,
      params?: { limit?: number; before?: number }
    ) => {
      const qs = new URLSearchParams()
      if (params?.limit) qs.set("limit", String(params.limit))
      if (params?.before) qs.set("before", String(params.before))
      const s = qs.toString()
      return request<Paged<Message>>(
        `/orchestrator/tasks/${tid}/messages${s ? `?${s}` : ""}`
      )
    },
    taskEventsUrl: (tid: number) => `${BASE}/orchestrator/tasks/${tid}/events`,
    taskCancel: (tid: number) =>
      request<null>(`/orchestrator/tasks/${tid}/cancel`, { method: "POST" }),
    taskResolvePermission: (
      tid: number,
      permissionId: string,
      optionId: string
    ) =>
      request<null>(`/orchestrator/tasks/${tid}/permission`, {
        method: "POST",
        body: JSON.stringify({ permissionId, optionId }),
      }),
    taskResolveElicitation: (
      tid: number,
      elicitationId: string,
      action: "accept" | "decline" | "cancel",
      content?: Record<string, string>
    ) =>
      request<null>(`/orchestrator/tasks/${tid}/elicitation`, {
        method: "POST",
        body: JSON.stringify({ elicitationId, action, content }),
      }),
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
    /** 在 path 下新建单层子目录，返回新目录条目（工作目录选择器就地建目录）。 */
    createDir: (path: string, name: string) =>
      request<DirEntry>("/fs/dirs", {
        method: "POST",
        body: JSON.stringify({ path, name }),
      }),
  },
}
