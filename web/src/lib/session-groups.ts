import type { Session } from "@/types/acp"

/** 一个工作目录 + 它名下最近的若干条会话。 */
export interface SessionGroup {
  /** 工作目录绝对路径，同时是分组的标识。 */
  cwd: string
  /** 侧边栏显示的短名（同名目录会自动带上一层父目录区分）。 */
  label: string
  /** 组内最新会话的分支，没有就不显示。 */
  branch?: string
  sessions: Session[]
}

/** 侧边栏「最近会话」的展示上限：最多 5 个工作区 × 每个 5 条 = 25 条。 */
export const MAX_GROUPS = 5
export const MAX_SESSIONS_PER_GROUP = 5

/**
 * 把会话按**工作目录**分组。
 *
 * 刻意不依赖项目（git 仓库）扫描：会话可以开在任何目录上，一旦工作区根
 * 变了、或者目录压根不是仓库，靠项目匹配就会整片落空，侧边栏退回平铺。
 * cwd 是会话自带的事实，永远对得上。
 *
 * 组的顺序按「组内最新会话」排——最近干活的工作区排在最前，这比按目录
 * 名或创建时间更贴合「最近会话」这四个字。
 */
export function groupSessionsByCwd(
  sessions: Session[],
  limits?: { groups?: number; perGroup?: number }
): SessionGroup[] {
  const maxGroups = limits?.groups ?? MAX_GROUPS
  const perGroup = limits?.perGroup ?? MAX_SESSIONS_PER_GROUP

  const buckets = new Map<string, Session[]>()
  for (const session of sessions) {
    const cwd = session.cwd?.trim()
    if (!cwd) continue
    const bucket = buckets.get(cwd)
    if (bucket) bucket.push(session)
    else buckets.set(cwd, [session])
  }

  const groups: SessionGroup[] = []
  for (const [cwd, bucket] of buckets) {
    const sorted = [...bucket].sort(
      (a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt)
    )
    groups.push({
      cwd,
      label: "",
      branch: sorted.find((s) => s.gitBranch)?.gitBranch,
      sessions: sorted.slice(0, perGroup),
    })
  }

  groups.sort(
    (a, b) =>
      Date.parse(b.sessions[0].updatedAt) - Date.parse(a.sessions[0].updatedAt)
  )

  // 标签只用文件夹名，不写路径：侧边栏窄，路径一长就被截断，反而不如
  // 一个短名 + 悬停看完整路径来得清楚。
  return groups
    .slice(0, maxGroups)
    .map((group) => ({ ...group, label: baseName(group.cwd) }))
}

function baseName(path: string): string {
  const parts = path.split("/").filter(Boolean)
  return parts.at(-1) ?? path
}
