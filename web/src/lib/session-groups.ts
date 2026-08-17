import type { Project, Session } from "@/types/acp"

/** 一个项目 + 它名下最近的若干条会话。 */
export interface SessionGroup {
  project: Project
  sessions: Session[]
}

/** 侧边栏「最近会话」的展示上限：最多 5 个项目 × 每个 5 条 = 25 条。 */
export const MAX_GROUPS = 5
export const MAX_SESSIONS_PER_GROUP = 5

/**
 * 把会话按项目分组（adr-007）。
 *
 * 归属靠 cwd 前缀：会话的工作目录落在哪个项目目录下就属于哪个项目，
 * worktree（`<仓库>/worktrees/<名字>`）自然归进它的主仓库。项目路径较长的
 * 优先匹配，免得嵌套仓库被外层抢走。
 *
 * 分组顺序按「组内最新会话」排——最近干活的项目排在最前，这比按项目
 * 目录时间排更贴合「最近会话」这四个字。不属于任何项目的会话归入
 * `ungrouped`，由调用方决定怎么展示。
 */
export function groupSessionsByProject(
  sessions: Session[],
  projects: Project[],
  limits?: { groups?: number; perGroup?: number }
): { groups: SessionGroup[]; ungrouped: Session[] } {
  const maxGroups = limits?.groups ?? MAX_GROUPS
  const perGroup = limits?.perGroup ?? MAX_SESSIONS_PER_GROUP

  // 长路径在前：`/a/b` 里嵌着 `/a/b/c` 时，会话该归给 `/a/b/c`。
  const ordered = [...projects].sort((a, b) => b.path.length - a.path.length)
  const buckets = new Map<string, Session[]>()
  const ungrouped: Session[] = []

  for (const session of sessions) {
    const owner = ordered.find((project) => isUnder(project.path, session.cwd))
    if (!owner) {
      ungrouped.push(session)
      continue
    }
    const bucket = buckets.get(owner.name)
    if (bucket) bucket.push(session)
    else buckets.set(owner.name, [session])
  }

  const groups: SessionGroup[] = []
  for (const project of projects) {
    const bucket = buckets.get(project.name)
    if (!bucket?.length) continue
    const sorted = [...bucket].sort(
      (a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt)
    )
    groups.push({ project, sessions: sorted.slice(0, perGroup) })
  }

  groups.sort(
    (a, b) =>
      Date.parse(b.sessions[0].updatedAt) - Date.parse(a.sessions[0].updatedAt)
  )
  return { groups: groups.slice(0, maxGroups), ungrouped }
}

/** path 是否等于 base 或在 base 之下（纯字符串比较，两边都是后端给的绝对路径）。 */
function isUnder(base: string, path: string): boolean {
  if (!base || !path) return false
  if (path === base) return true
  return path.startsWith(base.endsWith("/") ? base : `${base}/`)
}
