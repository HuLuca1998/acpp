// HTTP 层的地基：基址、错误类型、请求器与分页查询串。
//
// 单独成文件是因为端点定义已经不止一个文件（api.ts 与按域拆出去的
// api-connections.ts），它们都要用这几样东西——放在 api.ts 里再互相
// import 会绕成环。

/** 开发环境走 vite proxy，生产环境同源。可用 VITE_API_BASE 覆盖。 */
export const BASE = import.meta.env.VITE_API_BASE ?? "/api"

export class ApiError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = "ApiError"
    this.status = status
  }
}

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
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
 * 分页 + 排序的查询串。六个列表端点共用同一套协议（AGENTS.md §2），
 * 各写一遍必然会有漏掉排序参数的那一个。
 *
 * 空值一律不落进 URL：`0` 对 page/pageSize/agentId 都不是合法取值，
 * 当成「没给」处理。
 */
export function pageQuery(
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
