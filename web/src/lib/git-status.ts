import type { GitOverview } from "@/types/acp"

/** 文件在 git 眼里的状态，只留界面需要区分的三种。 */
export type FileChangeKind = "added" | "modified" | "deleted"

/** 状态 → 文字色 class。颜色只落在文件名上，不做底色（树本来就密）。 */
export const CHANGE_TONE: Record<FileChangeKind, string> = {
  added: "text-primary",
  modified: "text-warning",
  deleted: "text-destructive line-through decoration-1",
}

/**
 * 把 git 汇总铺成「绝对路径 → 状态」的表，供文件树给条目着色。
 *
 * status 报的路径相对**仓库根**，而文件树的条目是绝对路径——两者必须
 * 经 overview.root 对齐，否则会话开在仓库子目录时全都对不上。
 */
export function buildChangeMap(
  overview: GitOverview | null
): Map<string, FileChangeKind> {
  const map = new Map<string, FileChangeKind>()
  if (!overview?.isRepo || !overview.root) return map

  for (const file of overview.files) {
    const kind = kindOf(file.status)
    if (!kind) continue
    map.set(`${overview.root}/${file.path}`, kind)
  }
  return map
}

/**
 * 目录的状态：内部有任何变更就跟着标。取「最重」的一种——一个目录里
 * 既有新增又有删除时，删除更值得被看见。
 */
export function dirChangeKind(
  dir: string,
  changes: Map<string, FileChangeKind>
): FileChangeKind | undefined {
  const prefix = `${dir}/`
  let seen: FileChangeKind | undefined
  for (const [path, kind] of changes) {
    if (!path.startsWith(prefix)) continue
    if (kind === "deleted") return "deleted"
    // added 比 modified 更值得提示（新目录里全是新文件）。
    if (kind === "added" || seen === undefined) seen = kind
  }
  return seen
}

/** git 的 status 字母 → 界面区分的三种状态；重命名等归入「修改」。 */
function kindOf(status: string): FileChangeKind | undefined {
  const code = status.trim().toUpperCase()
  if (code === "") return undefined
  if (code.startsWith("?") || code.startsWith("A")) return "added"
  if (code.startsWith("D")) return "deleted"
  return "modified"
}
