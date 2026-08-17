/** 路径树的一个目录节点。 */
export interface PathTreeNode<T> {
  /** 显示名。单子目录链会被压缩成 `a/b/c` 一行。 */
  name: string
  /** 相对根的完整目录路径，作为折叠状态与 key。 */
  path: string
  dirs: PathTreeNode<T>[]
  files: { name: string; item: T }[]
}

/**
 * 把一组带路径的条目铺成目录树。
 *
 * 平铺完整路径在窄面板里几乎不可读——`web/src/components/workspace/...`
 * 一律被截断成 `web/src/components/wor…`，谁也看不出是哪个文件。树形让
 * 目录只写一次，文件名回到最显眼的位置。
 *
 * **单子目录链会被压缩**（`web/src/lib` 合成一行）：变更集里常有一长串
 * 只含一个子目录的层级，每层单独占一行只是在浪费高度——vscode 与 GoLand
 * 都这么做。
 */
export function buildPathTree<T>(
  items: T[],
  pathOf: (item: T) => string
): PathTreeNode<T> {
  const root: PathTreeNode<T> = { name: "", path: "", dirs: [], files: [] }

  for (const item of items) {
    const parts = pathOf(item).split("/").filter(Boolean)
    if (parts.length === 0) continue

    const fileName = parts.pop() as string
    let node = root
    for (const segment of parts) {
      const path = node.path ? `${node.path}/${segment}` : segment
      let next = node.dirs.find((dir) => dir.path === path)
      if (!next) {
        next = { name: segment, path, dirs: [], files: [] }
        node.dirs.push(next)
      }
      node = next
    }
    node.files.push({ name: fileName, item })
  }

  return compress(root)
}

/** 目录数（含自身之下的全部层级），用于「这个目录里有几个文件」的计数。 */
export function countFiles<T>(node: PathTreeNode<T>): number {
  return (
    node.files.length + node.dirs.reduce((sum, dir) => sum + countFiles(dir), 0)
  )
}

/** 把只有一个子目录、自身没有文件的节点与子节点合并。根节点不合并。 */
function compress<T>(node: PathTreeNode<T>, isRoot = true): PathTreeNode<T> {
  const dirs = node.dirs.map((dir) => compress(dir, false))

  if (!isRoot && dirs.length === 1 && node.files.length === 0) {
    const only = dirs[0]
    return {
      name: `${node.name}/${only.name}`,
      path: only.path,
      dirs: only.dirs,
      files: only.files,
    }
  }
  return { ...node, dirs }
}
