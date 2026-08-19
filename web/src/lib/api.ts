import type {
  Agent,
  CatalogInput,
  CloneTask,
  DataSource,
  DataSourceInput,
  DataSourceTest,
  DataSourceUri,
  DbDatabase,
  DbTable,
  DbTableDetail,
  DirEntry,
  DirListing,
  EnvInfo,
  EnvInstallResult,
  GitBranchView,
  GitCommitDetail,
  GitCompare,
  GitHistory,
  GitOpResult,
  GitDiffView,
  GitOverview,
  Identity,
  Message,
  OrchSession,
  OrchTask,
  OverviewStats,
  Paged,
  PageQuery,
  UploadedFile,
  Project,
  RemoteRepo,
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
  SqlExecResult,
  SystemInfo,
  TitleModelConfig,
  OllamaModel,
  Tenant,
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

/**
 * 工作区数据面的作用域 API：普通会话与编排主会话的端点形状完全一致，
 * 只差路径前缀。面板组件经 WorkspaceProvider 拿到对应作用域实例。
 */
export function workspaceScopeApi(prefix: string, draftCwd?: string) {
  /**
   * 一次请求的路径基底。会话态是 `/sessions/{id}`；草稿态没有会话，
   * 基底是 `/workspace`，目录改由 `?cwd=` 带过去——看文件和看 git 状态
   * 本来就只需要一个目录，不该等到会话建出来才允许。
   */
  const at = (id: number, path: string) => {
    if (draftCwd === undefined) return `${prefix}/${id}${path}`
    const [base, query] = path.split("?")
    const params = new URLSearchParams(query)
    params.set("cwd", draftCwd)
    return `/workspace${base}?${params.toString()}`
  }
  return {
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
      const res = await fetch(`${BASE}${prefix}/${id}/transcript`, {
        headers: offset > 0 ? { Range: `bytes=${offset}-` } : {},
        cache: "no-store",
      })
      // 204：偏移已到文件末尾，没有新内容——后端刻意不用 416，那会让
      // 浏览器按失败资源在控制台刷红字（尾随读每 2 秒一次）。416 仍然
      // 认，中间层可能自己就把越界的 Range 拦下来了。
      if (res.status === 204 || res.status === 416) return null
      if (!res.ok) throw new ApiError(res.status, res.statusText)
      const chunk = await res.text()
      if (chunk === "") return null
      // 偏移按字节推进（JSONL 里的中文是多字节，不能用字符数）。
      const bytes = new TextEncoder().encode(chunk).length
      // 防御：请求了 Range 却收到 200（中间层没执行 Range），这是全量。
      const reset = offset > 0 && res.status === 200
      return { chunk, nextOffset: reset ? bytes : offset + bytes, reset }
    },

    /**
     * 会话可见的数据源：只有当前工作目录所属项目的那几条（adr-008）。
     * 斜杠命令与 @ 数据库引用都读它——界面能看到的范围与 AI 能操作的
     * 范围必须是同一个。
     */
    datasources: (id: number) => request<DataSource[]>(at(id, "/datasources")),
    datasourceDatabases: (id: number, dsid: number) =>
      request<DbDatabase[]>(at(id, `/datasources/${dsid}/databases`)),
    datasourceTables: (id: number, dsid: number, database: string) =>
      request<DbTable[]>(
        at(
          id,
          `/datasources/${dsid}/tables?database=${encodeURIComponent(database)}`
        )
      ),

    /** 工作区文件树：path 为空从会话 cwd 开始，depth ≤ 2。 */
    workspaceTree: (id: number, params?: { path?: string; depth?: number }) => {
      const qs = new URLSearchParams()
      if (params?.path) qs.set("path", params.path)
      if (params?.depth) qs.set("depth", String(params.depth))
      const s = qs.toString()
      return request<TreeListing>(at(id, `/fs/entries${s ? `?${s}` : ""}`))
    },
    /** 工作区文件内容（预览用，路径限制在会话 cwd 内）。 */
    workspaceFile: (id: number, path: string) =>
      request<WorkspaceFile>(
        at(id, `/fs/file?path=${encodeURIComponent(path)}`)
      ),

    /**
     * 文件下载地址。返回 URL 而不是发请求：交给浏览器自己下，才能有
     * 原生的下载进度与「另存为」；同源请求 cookie 自动带上，鉴权照旧。
     */
    downloadUrl: (id: number, path: string, archive = false) =>
      `${BASE}${at(id, `/fs/download?path=${encodeURIComponent(path)}${archive ? "&archive=1" : ""}`)}`,
    /** git 汇总：分支/领先落后/变更文件/未推送 commit，diff 与 commit 面板共享。 */
    gitOverview: (id: number) => request<GitOverview>(at(id, `/git/overview`)),
    /** 工作区单文件 diff（HEAD 版对工作区版的两端全文）。 */
    gitDiff: (id: number, path: string) =>
      request<GitDiffView>(
        at(id, `/git/diff?path=${encodeURIComponent(path)}`)
      ),
    /** 分支面：当前分支、本地/远端分支、worktree 清单（会话底部控件用）。 */
    gitBranches: (id: number) =>
      request<GitBranchView>(at(id, `/git/branches`)),
    /** 切换分支（create=true 时先从当前 HEAD 新建）。脏工作区后端会拒。 */
    gitCheckout: (id: number, input: { branch: string; create?: boolean }) =>
      request<GitBranchView>(at(id, `/git/checkout`), {
        method: "POST",
        body: JSON.stringify(input),
      }),
    /** 提交链路的一页（ref 为空看 HEAD；offset 翻页，hasMore 指示还有没有）。 */
    gitHistory: (
      id: number,
      params?: { ref?: string; limit?: number; offset?: number }
    ) => {
      const qs = new URLSearchParams()
      if (params?.ref) qs.set("ref", params.ref)
      if (params?.limit) qs.set("limit", String(params.limit))
      if (params?.offset) qs.set("offset", String(params.offset))
      const s = qs.toString()
      return request<GitHistory>(at(id, `/git/history${s ? `?${s}` : ""}`))
    },
    /** 对比两个 ref：head 相对 base 多出的提交与文件变更。 */
    gitCompare: (id: number, base: string, head: string) =>
      request<GitCompare>(
        at(
          id,
          `/git/compare?base=${encodeURIComponent(base)}&head=${encodeURIComponent(head)}`
        )
      ),
    /** 提交工作区全部改动（含未跟踪文件）。 */
    gitCommitAll: (id: number, message: string) =>
      request<GitOpResult>(at(id, "/git/commit"), {
        method: "POST",
        body: JSON.stringify({ message }),
      }),
    /** 推送当前分支（没有 upstream 时顺手建立跟踪）。 */
    gitPush: (id: number) =>
      request<GitOpResult>(at(id, "/git/push"), { method: "POST" }),
    /** 拉取当前分支，只接受快进。 */
    gitPull: (id: number) =>
      request<GitOpResult>(at(id, "/git/pull"), { method: "POST" }),
    /** 把某个 ref 合并进当前分支。 */
    gitMerge: (id: number, ref: string) =>
      request<GitOpResult>(at(id, "/git/merge"), {
        method: "POST",
        body: JSON.stringify({ ref }),
      }),
    /** 新建分支（from 为空即当前 HEAD；checkout 表示顺带切过去）。 */
    gitCreateBranch: (
      id: number,
      input: { name: string; from?: string; checkout?: boolean }
    ) =>
      request<GitOpResult>(at(id, "/git/branches"), {
        method: "POST",
        body: JSON.stringify(input),
      }),
    /** 删本地分支；force 才用 -D（没合并的分支默认删不掉是 git 的保护）。 */
    gitDeleteBranch: (id: number, name: string, force = false) =>
      request<GitOpResult>(
        at(
          id,
          `/git/branches/${encodeURIComponent(name)}${force ? "?force=1" : ""}`
        ),
        { method: "DELETE" }
      ),
    /** 丢弃改动：paths 为空表示整个工作区。 */
    gitDiscard: (id: number, paths?: string[]) =>
      request<null>(at(id, "/git/discard"), {
        method: "POST",
        body: JSON.stringify({ paths: paths ?? [] }),
      }),
    /** 开隔离工作区：`<仓库>/worktrees/<名字>`，返回它的绝对路径。 */
    worktreeCreate: (id: number, input: { name: string; branch?: string }) =>
      request<{ path: string }>(at(id, `/git/worktrees`), {
        method: "POST",
        body: JSON.stringify(input),
      }),
    /** 拆掉一个 worktree（分支保留）。 */
    worktreeRemove: (id: number, path: string) =>
      request<null>(at(id, `/git/worktrees`), {
        method: "DELETE",
        body: JSON.stringify({ path }),
      }),
    /** 提交详情（文件清单）。 */
    gitCommit: (id: number, sha: string) =>
      request<GitCommitDetail>(at(id, `/git/commits/${sha}`)),
    /** 某文件在一条提交前后的全文。 */
    gitCommitFile: (id: number, sha: string, path: string) =>
      request<GitDiffView>(
        at(id, `/git/commits/${sha}?path=${encodeURIComponent(path)}`)
      ),

    /** 工作区终端：REST 管生命周期，ws 走 terminalWsUrl。 */
    terminalCreate: (id: number) =>
      request<TerminalInfo>(at(id, `/terminals`), { method: "POST" }),
    terminalList: (id: number) => request<TerminalInfo[]>(at(id, `/terminals`)),
    terminalRemove: (id: number, tid: string) =>
      request<null>(at(id, `/terminals/${tid}`), { method: "DELETE" }),
    terminalWsUrl: (id: number, tid: string) => {
      // 开发态直连后端：vite 的 ws 代理在 HMR/重启后会僵死（输入静默丢失），
      // 端口是项目固定约定（根 AGENTS.md §4.0），后端 ws 升级已放行本源。
      if (import.meta.env.DEV && !BASE.startsWith("http")) {
        return `ws://127.0.0.1:48080/api${prefix}/${id}/terminals/${tid}/ws`
      }
      const proto = location.protocol === "https:" ? "wss" : "ws"
      const base = BASE.startsWith("http")
        ? BASE.replace(/^http/, "ws")
        : `${proto}://${location.host}${BASE}`
      return `${base}${prefix}/${id}/terminals/${tid}/ws`
    },
  }
}

/** 工作区作用域 API 的类型（面板与 provider 消费）。 */
export type WorkspaceScopeApi = ReturnType<typeof workspaceScopeApi>

/**
 * 分页 + 排序的查询串。六个列表端点共用同一套协议（AGENTS.md §2），
 * 各写一遍必然会有漏掉排序参数的那一个。
 *
 * 空值一律不落进 URL：`0` 对 page/pageSize/agentId 都不是合法取值，
 * 当成「没给」处理。
 */
function pageQuery(
  params?: Record<string, string | number | undefined>
): string {
  const qs = new URLSearchParams()
  for (const [key, value] of Object.entries(params ?? {})) {
    if (value === undefined || value === "" || value === 0) continue
    qs.set(key, String(value))
  }
  const s = qs.toString()
  return s ? `?${s}` : ""
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
    /** 改工作区根（立刻生效：只影响之后新建的会话与访客）。 */
    setWorkspaceDir: (workspaceDir: string) =>
      request<SystemInfo>("/system/workspace-dir", {
        method: "PUT",
        body: JSON.stringify({ workspaceDir }),
      }),
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
    /** 读会话标题模型配置。 */
    titleModel: () => request<TitleModelConfig>("/system/title-model"),
    /** 存标题模型配置，立刻热更到运行中的服务（无需重启）。 */
    saveTitleModel: (input: TitleModelConfig) =>
      request<TitleModelConfig>("/system/title-model", {
        method: "PUT",
        body: JSON.stringify(input),
      }),
    /** 拉某个 ollama 端点上已装的模型；地址传参是为了在保存前先试拉。 */
    titleModels: (baseUrl: string) =>
      request<OllamaModel[]>(
        `/system/title-model/models?baseUrl=${encodeURIComponent(baseUrl)}`
      ),
    /** 用表单里的配置当场生成一个标题看效果，不落盘。 */
    testTitleModel: (input: TitleModelConfig) =>
      request<{ title: string }>("/system/title-model/test", {
        method: "POST",
        body: JSON.stringify(input),
      }),
    /** 版本检查（缓存结果；force 现查 GitHub Releases）。 */
    update: (force?: boolean) =>
      request<UpdateInfo>(`/system/update${force ? "?force=1" : ""}`),
    /** 一键更新：下载最新 release 替换 .app 并自动重启（仅桌面版）。 */
    updateApply: () =>
      request<{ message: string }>("/system/update/apply", { method: "POST" }),
  },

  /**
   * 本地文件上传。落在各自身份的家目录下（owner 是工作区根，租户是自己
   * 的 root），拿到 path 之后就是一个普通的 @ 文件引用。
   */
  uploads: {
    list: () => request<Paged<UploadedFile>>("/uploads"),
    create: async (file: File) => {
      const body = new FormData()
      body.append("file", file)
      // 不设 Content-Type：multipart 的 boundary 得让浏览器自己填。
      const res = await fetch(`${BASE}/uploads`, { method: "POST", body })
      const json = (await res.json()) as { data?: UploadedFile; error?: string }
      if (!res.ok) throw new ApiError(res.status, json.error ?? res.statusText)
      return json.data as UploadedFile
    },
    remove: (hash: string, name: string) =>
      request<{ deleted: boolean }>(
        `/uploads?hash=${encodeURIComponent(hash)}&name=${encodeURIComponent(name)}`,
        { method: "DELETE" }
      ),
  },

  sessions: {
    /** 概览统计（按天趋势 + agent/状态分布）。聚合在后端做——分页列表算不准。 */
    overview: (days = 14) =>
      request<OverviewStats>(`/sessions/overview?days=${days}`),

    list: (params?: Partial<PageQuery> & { agentId?: number }) =>
      request<Paged<Session>>(`/sessions${pageQuery(params)}`),
    get: (id: number) => request<Session>(`/sessions/${id}`),
    create: (input: {
      agentId: number
      title?: string
      cwd?: string
      /** 非空时先在 cwd 所在仓库下开 `worktrees/<worktree>`，会话开在那里。 */
      worktree?: string
      worktreeBranch?: string
    }) =>
      request<Session>("/sessions", {
        method: "POST",
        body: JSON.stringify(input),
      }),
    remove: (id: number) =>
      request<null>(`/sessions/${id}`, { method: "DELETE" }),
    /**
     * 子代理的最终产出。只有 codex 需要——它的子代理是独立 thread，产出不在
     * 协议里，得开一条一次性会话把转录 load 出来，所以是展开时才拉的懒加载。
     * claude 的产出随工具调用一起下发，不走这里。
     */
    subagentOutput: (id: number, threadId: string) =>
      request<{ output: string }>(
        `/sessions/${id}/subagents/${encodeURIComponent(threadId)}/output`
      ),
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

    /** 工作区数据面（文件树/预览/git/终端/转录）——见 workspaceScopeApi。 */
    ...workspaceScopeApi("/sessions"),
  },

  skills: {
    list: (params?: Partial<PageQuery>) =>
      request<Paged<Skill>>(`/skills${pageQuery(params)}`),
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
    list: (params?: Partial<PageQuery>) =>
      request<Paged<Role>>(`/roles${pageQuery(params)}`),
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
    list: (params?: Partial<PageQuery>) =>
      request<Paged<OrchSession>>(`/orchestrator/sessions${pageQuery(params)}`),
    get: (id: number) => request<OrchSession>(`/orchestrator/sessions/${id}`),
    create: (input: { agentId: number; cwd?: string; title?: string }) =>
      request<OrchSession>("/orchestrator/sessions", {
        method: "POST",
        body: JSON.stringify(input),
      }),
    remove: (id: number) =>
      request<null>(`/orchestrator/sessions/${id}`, { method: "DELETE" }),
    send: (id: number, input: SendInput) =>
      request<Message>(`/orchestrator/sessions/${id}/send`, {
        method: "POST",
        body: JSON.stringify(input),
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
    /** 编排主会话的工作区数据面（与普通会话同形状，见 workspaceScopeApi）。 */
    ...workspaceScopeApi("/orchestrator/sessions"),

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

  /** 身份（adr-007）。凭证是 HttpOnly cookie，请求不用手动带。 */
  auth: {
    /**
     * 未认证时同样 200（authenticated=false），前端据此渲染邀请页。
     * no-store：响应随 cookie 变，缓存住会让人兑换完还看见上一个身份。
     */
    me: () => request<Identity>("/auth/me", { cache: "no-store" }),
    /** 用邀请链接里的 token 换 cookie。 */
    redeem: (token: string) =>
      request<Identity>("/auth/redeem", {
        method: "POST",
        body: JSON.stringify({ token }),
      }),
    logout: () => request<Identity>("/auth/logout", { method: "POST" }),
  },

  /** 租户管理（owner 专属）。 */
  tenants: {
    list: (params?: Partial<PageQuery>) =>
      request<Paged<Tenant>>(`/tenants${pageQuery(params)}`),
    create: (name: string) =>
      request<Tenant>("/tenants", {
        method: "POST",
        body: JSON.stringify({ name }),
      }),
    /** 停用/启用，或改工作目录根。 */
    update: (id: number, patch: { disabled?: boolean; root?: string }) =>
      request<Tenant>(`/tenants/${id}`, {
        method: "PUT",
        body: JSON.stringify(patch),
      }),
    /** 重新生成分享链接：旧链接立刻作废，会话与目录不动。 */
    rotate: (id: number) =>
      request<Tenant>(`/tenants/${id}/rotate`, { method: "POST" }),
    remove: (id: number) =>
      request<null>(`/tenants/${id}`, { method: "DELETE" }),
  },

  /** 工作区项目：磁盘即事实源，克隆是后台任务。 */
  projects: {
    list: () => request<Paged<Project>>("/projects"),
    create: (name: string) =>
      request<Project>("/projects", {
        method: "POST",
        body: JSON.stringify({ name }),
      }),
    remove: (name: string) =>
      request<null>(`/projects/${name}`, { method: "DELETE" }),
    /** 起一个后台克隆，进度轮询 clones。 */
    clone: (input: { url: string; name?: string }) =>
      request<CloneTask>("/projects/clone", {
        method: "POST",
        body: JSON.stringify(input),
      }),
    clones: () => request<Paged<CloneTask>>("/projects/clones"),
    /** 克隆对话框的可选仓库（gh CLI，不含个人账号名下的仓库）。 */
    repos: () => request<Paged<RemoteRepo>>("/projects/repos"),
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

  /**
   * 数据库数据源（adr-008）。管理面是全量的（owner 专属）；会话面在
   * sessions.datasources 下，只给当前项目的那几条——两个入口刻意分开，
   * 免得在会话里误用别的项目的连接。
   */
  datasources: {
    list: (params?: Partial<PageQuery>) =>
      request<Paged<DataSource>>(`/datasources${pageQuery(params)}`),
    /** 配置页选库用：列出这组连接参数能看到的全部库（连接还没绑定库）。 */
    probeDatabases: (input: DataSourceInput & { id?: number }) =>
      request<DbDatabase[]>("/datasources/probe-databases", {
        method: "POST",
        body: JSON.stringify(input),
      }),
    get: (id: number) => request<DataSource>(`/datasources/${id}`),
    create: (input: DataSourceInput) =>
      request<DataSource>("/datasources", {
        method: "POST",
        body: JSON.stringify(input),
      }),
    update: (id: number, input: DataSourceInput) =>
      request<DataSource>(`/datasources/${id}`, {
        method: "PUT",
        body: JSON.stringify(input),
      }),
    remove: (id: number) =>
      request<null>(`/datasources/${id}`, { method: "DELETE" }),
    /** 拨一次真连接确认配置可用，失败不抛异常而是返回 ok:false + 原话。 */
    test: (id: number) =>
      request<DataSourceTest>(`/datasources/${id}/test`, { method: "POST" }),
    /** 导出连接 URI（Navicat 与通用两种写法，**含密码**）。 */
    uri: (id: number) => request<DataSourceUri>(`/datasources/${id}/uri`),
    databases: (id: number) =>
      request<DbDatabase[]>(`/datasources/${id}/databases`),
    tables: (id: number, database: string) =>
      request<DbTable[]>(
        `/datasources/${id}/tables?database=${encodeURIComponent(database)}`
      ),
    schema: (id: number, database: string, table: string) =>
      request<DbTableDetail>(
        `/datasources/${id}/schema?database=${encodeURIComponent(database)}&table=${encodeURIComponent(table)}`
      ),
    /** 执行一段 SQL，可含多条语句（按顺序执行、遇错即停）。 */
    query: (
      id: number,
      input: { database?: string; sql: string; maxRows?: number }
    ) =>
      request<SqlExecResult>(`/datasources/${id}/query`, {
        method: "POST",
        body: JSON.stringify(input),
      }),
  },
}
