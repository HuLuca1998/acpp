import { useEffect, useRef } from "react"

import type { DirEntry, DirListing } from "@/types/acp"
import { cn } from "@/lib/utils"
import { ChevronRightIcon, FolderIcon } from "lucide-react"

import { fileIconOf, sortEntries, type DirSortKey } from "./shared"

/**
 * 访达分栏视图（Miller columns）：每一列是一层目录，点目录在右侧
 * 开新列逐级下钻，路径全程可见。文件行点击即选中（file 模式）。
 */
export function DirColumnsView({
  columns,
  sortKey,
  sortAsc,
  onDrill,
  onPickFile,
}: {
  /** 从起点到当前的每层 listing，最后一列是「当前目录」。 */
  columns: DirListing[]
  sortKey: DirSortKey
  sortAsc: boolean
  /** 在第 colIndex 列点开一个目录：截断右侧列并下钻。 */
  onDrill: (path: string, colIndex: number) => void
  onPickFile: (path: string) => void
}) {
  const scroller = useRef<HTMLDivElement>(null)

  // 新列出现时滚到最右，视线跟着下钻走。
  useEffect(() => {
    const el = scroller.current
    if (el) el.scrollLeft = el.scrollWidth
  }, [columns.length])

  const row = (entry: DirEntry, isDir: boolean, colIndex: number) => {
    // 高亮「通往右侧列的那一项」，一眼看清当前路径经过谁。
    const active = isDir && columns[colIndex + 1]?.path === entry.path
    const Icon = isDir ? FolderIcon : fileIconOf(entry.name)
    return (
      <li key={entry.path}>
        <button
          type="button"
          className={cn(
            "flex w-full items-center gap-1.5 rounded-md px-2 py-1 text-left text-sm transition-colors hover:bg-muted focus-visible:bg-muted focus-visible:outline-none",
            active && "bg-muted"
          )}
          onClick={() =>
            isDir ? onDrill(entry.path, colIndex) : onPickFile(entry.path)
          }
        >
          <Icon className="size-4 shrink-0 text-muted-foreground" />
          <span className="min-w-0 flex-1 truncate">{entry.name}</span>
          {isDir ? (
            <ChevronRightIcon className="size-3.5 shrink-0 text-muted-foreground/60" />
          ) : null}
        </button>
      </li>
    )
  }

  return (
    <div ref={scroller} className="flex h-full overflow-x-auto">
      {columns.map((listing, i) => (
        <ul
          key={listing.path}
          className="h-full w-52 shrink-0 overflow-y-auto border-r border-border p-1 last:border-r-0"
        >
          {sortEntries(listing.dirs, sortKey, sortAsc).map((d) =>
            row(d, true, i)
          )}
          {sortEntries(listing.files ?? [], sortKey, sortAsc).map((f) =>
            row(f, false, i)
          )}
        </ul>
      ))}
    </div>
  )
}
