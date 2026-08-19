import { useTranslation } from "react-i18next"

import type { DirEntry, DirListing } from "@/types/acp"
import { cn } from "@/lib/utils"
import { formatBytes } from "@/lib/format"
import { ChevronDownIcon, ChevronUpIcon, FolderIcon } from "lucide-react"

import { fileIconOf, formatMtime, sortEntries, type DirSortKey } from "./shared"

const GRID = "grid grid-cols-[minmax(0,1fr)_6.5rem_4.5rem] items-center gap-2"

function HeaderCell({
  label,
  sortKey,
  active,
  asc,
  align,
  onSort,
}: {
  label: string
  sortKey: DirSortKey
  active: boolean
  asc: boolean
  align?: "right"
  onSort: (key: DirSortKey) => void
}) {
  const Chevron = asc ? ChevronUpIcon : ChevronDownIcon
  return (
    <button
      type="button"
      className={cn(
        "flex items-center gap-0.5 truncate py-1 text-xs text-muted-foreground transition-colors hover:text-foreground",
        align === "right" && "justify-end",
        active && "text-foreground"
      )}
      onClick={() => onSort(sortKey)}
    >
      {label}
      {active ? <Chevron className="size-3 shrink-0" /> : null}
    </button>
  )
}

/**
 * 目录/文件列表（访达列表视图的骨架）：名称、修改日期、大小三列，
 * 表头可排序。目录行点击进入，文件行点击选中。
 */
export function DirListView({
  listing,
  sortKey,
  sortAsc,
  locale,
  onSort,
  onOpenDir,
  onPickFile,
}: {
  listing: DirListing
  sortKey: DirSortKey
  sortAsc: boolean
  locale: string
  onSort: (key: DirSortKey) => void
  onOpenDir: (path: string) => void
  onPickFile: (path: string) => void
}) {
  const { t } = useTranslation()
  const dirs = sortEntries(listing.dirs, sortKey, sortAsc)
  const files = sortEntries(listing.files ?? [], sortKey, sortAsc)

  const row = (entry: DirEntry, isDir: boolean) => {
    const Icon = isDir ? FolderIcon : fileIconOf(entry.name)
    return (
      <li key={entry.path}>
        <button
          type="button"
          className={cn(
            GRID,
            "w-full rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-muted focus-visible:bg-muted focus-visible:outline-none"
          )}
          onClick={() => (isDir ? onOpenDir(entry.path) : onPickFile(entry.path))}
        >
          <span className="flex min-w-0 items-center gap-2">
            <Icon className="size-4 shrink-0 text-muted-foreground" />
            <span className="truncate">{entry.name}</span>
          </span>
          <span className="truncate text-right text-xs text-muted-foreground tabular-nums">
            {formatMtime(entry.modTime, locale)}
          </span>
          <span className="text-right text-xs text-muted-foreground tabular-nums">
            {isDir ? "—" : formatBytes(entry.size)}
          </span>
        </button>
      </li>
    )
  }

  return (
    <div className="flex flex-col">
      <div className={cn(GRID, "border-b border-border px-2")}>
        <HeaderCell
          label={t("dirPicker.colName")}
          sortKey="name"
          active={sortKey === "name"}
          asc={sortAsc}
          onSort={onSort}
        />
        <HeaderCell
          label={t("dirPicker.colModified")}
          sortKey="mtime"
          active={sortKey === "mtime"}
          asc={sortAsc}
          align="right"
          onSort={onSort}
        />
        <HeaderCell
          label={t("dirPicker.colSize")}
          sortKey="size"
          active={sortKey === "size"}
          asc={sortAsc}
          align="right"
          onSort={onSort}
        />
      </div>
      <ul className="p-1">
        {dirs.map((d) => row(d, true))}
        {files.map((f) => row(f, false))}
      </ul>
    </div>
  )
}
