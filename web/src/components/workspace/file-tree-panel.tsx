import { memo, useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import {
  ChevronRightIcon,
  FileIcon,
  FolderIcon,
  FolderTreeIcon,
  RotateCwIcon,
} from "lucide-react"

import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { TreeEntry } from "@/types/acp"
import { useWorkspace } from "@/components/workspace/workspace-context"
import { Button } from "@/components/ui/button"
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@/components/ui/context-menu"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Spinner } from "@/components/ui/spinner"

/**
 * 文件树面板：首屏一次拉两层（后端 depth=2），更深展开时按目录懒加载。
 * 懒加载的子目录内容放 childrenByPath，不回写树结构，保持更新扁平。
 */
export const FileTreePanel = memo(function FileTreePanel() {
  const { t } = useTranslation()
  const ws = useWorkspace()
  const [entries, setEntries] = useState<TreeEntry[] | null>(null)
  const [truncated, setTruncated] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [childrenByPath, setChildrenByPath] = useState<
    Map<string, TreeEntry[]>
  >(new Map())

  const load = useCallback(() => {
    if (!ws.sessionId) return
    api.sessions
      .workspaceTree(ws.sessionId, { depth: 2 })
      .then((listing) => {
        setError(null)
        setEntries(listing.entries)
        setTruncated(listing.truncated ?? false)
        // 首屏默认展开第一层目录（即「默认展开 2 层」——根内容 + 一级目录内容全部可见）。
        setExpanded(
          new Set(
            listing.entries.filter((e) => e.kind === "dir").map((e) => e.path)
          )
        )
        setChildrenByPath(new Map())
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : String(err))
      })
  }, [ws.sessionId])

  useEffect(() => {
    load()
  }, [load])

  // turn 结束后的工作区广播：树跟着 git 数据一起重拉。
  useEffect(() => ws.onWorkspaceRefresh(load), [ws, load])

  const toggleDir = useCallback(
    (entry: TreeEntry) => {
      setExpanded((prev) => {
        const next = new Set(prev)
        if (next.has(entry.path)) {
          next.delete(entry.path)
        } else {
          next.add(entry.path)
        }
        return next
      })
      // 未展开过的深层目录：补拉一层。失败不打断树，行内静默（下次点击重试）。
      if (!entry.listed && !childrenByPath.has(entry.path)) {
        void api.sessions
          .workspaceTree(ws.sessionId, { path: entry.path, depth: 1 })
          .then((listing) => {
            setChildrenByPath((prev) =>
              new Map(prev).set(entry.path, listing.entries)
            )
          })
          .catch(() => {})
      }
    },
    [ws.sessionId, childrenByPath]
  )

  if (!ws.sessionId) {
    return (
      <PanelEmptyState
        title={t("workspace.tree.draftTitle")}
        description={t("workspace.tree.draftHint")}
      />
    )
  }

  if (error) {
    return (
      <PanelEmptyState
        title={t("workspace.tree.loadFailed")}
        description={error}
        action={
          <Button size="sm" variant="outline" onClick={load}>
            {t("common.retry")}
          </Button>
        }
      />
    )
  }

  if (entries === null) {
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner className="size-4 text-muted-foreground" />
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col [contain:strict]">
      <div className="flex h-8 shrink-0 items-center justify-end px-2">
        <button
          type="button"
          aria-label={t("workspace.tree.refresh")}
          className="flex size-6 items-center justify-center rounded-md text-muted-foreground transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97]"
          onClick={load}
        >
          <RotateCwIcon className="size-3.5" />
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-auto px-1 pb-2">
        {entries.length === 0 ? (
          <div className="px-2 py-4 text-xs text-muted-foreground">
            {t("workspace.tree.empty")}
          </div>
        ) : (
          entries.map((entry) => (
            <TreeNode
              key={entry.path}
              entry={entry}
              depth={0}
              expanded={expanded}
              childrenByPath={childrenByPath}
              onToggle={toggleDir}
              onOpenFile={ws.openPreview}
              onAddReference={ws.addReference}
            />
          ))
        )}
        {truncated ? (
          <div className="px-2 py-1 text-xs text-muted-foreground">
            {t("workspace.tree.truncated")}
          </div>
        ) : null}
      </div>
    </div>
  )
})

/** 单个树节点：固定行高，展开箭头只动 transform；右键出引用菜单。 */
function TreeNode({
  entry,
  depth,
  expanded,
  childrenByPath,
  onToggle,
  onOpenFile,
  onAddReference,
}: {
  entry: TreeEntry
  depth: number
  expanded: Set<string>
  childrenByPath: Map<string, TreeEntry[]>
  onToggle: (entry: TreeEntry) => void
  onOpenFile: (path: string) => void
  onAddReference: (path: string) => void
}) {
  const { t } = useTranslation()
  const isDir = entry.kind === "dir"
  const isOpen = isDir && expanded.has(entry.path)
  const children = entry.children ?? childrenByPath.get(entry.path)

  return (
    <>
      <ContextMenu>
        <ContextMenuTrigger
          render={
            <button
              type="button"
              title={entry.path}
              className="flex h-6 w-full items-center gap-1 rounded-md px-1.5 text-xs text-foreground/90 transition-colors duration-150 ease-snappy hover:bg-muted"
              style={{ paddingLeft: `${depth * 14 + 6}px` }}
              onClick={() => (isDir ? onToggle(entry) : onOpenFile(entry.path))}
            />
          }
        >
          {isDir ? (
            <ChevronRightIcon
              className={cn(
                "size-3.5 shrink-0 text-muted-foreground transition-transform duration-150 ease-snappy",
                isOpen && "rotate-90"
              )}
            />
          ) : (
            <span className="size-3.5 shrink-0" />
          )}
          {isDir ? (
            <FolderIcon className="size-3.5 shrink-0 text-muted-foreground" />
          ) : (
            <FileIcon className="size-3.5 shrink-0 text-muted-foreground" />
          )}
          <span className="truncate">{entry.name}</span>
        </ContextMenuTrigger>
        <ContextMenuContent>
          <ContextMenuItem onClick={() => onAddReference(entry.path)}>
            {t("workspace.refMenu.addReference")}
          </ContextMenuItem>
          {!isDir ? (
            <ContextMenuItem onClick={() => onOpenFile(entry.path)}>
              {t("workspace.refMenu.openPreview")}
            </ContextMenuItem>
          ) : null}
          <ContextMenuItem
            onClick={() => void navigator.clipboard.writeText(entry.path)}
          >
            {t("workspace.refMenu.copyPath")}
          </ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>
      {isOpen && children
        ? children.map((child) => (
            <TreeNode
              key={child.path}
              entry={child}
              depth={depth + 1}
              expanded={expanded}
              childrenByPath={childrenByPath}
              onToggle={onToggle}
              onOpenFile={onOpenFile}
              onAddReference={onAddReference}
            />
          ))
        : null}
      {isOpen && !children ? (
        <div
          className="flex h-6 items-center px-1.5"
          style={{ paddingLeft: `${(depth + 1) * 14 + 6}px` }}
        >
          <Spinner className="size-3 text-muted-foreground" />
        </div>
      ) : null}
    </>
  )
}

/** 面板内空态：树/预览共用的轻量壳。 */
export function PanelEmptyState({
  title,
  description,
  action,
}: {
  title: string
  description?: string
  action?: React.ReactNode
}) {
  return (
    <Empty className="h-full justify-center">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <FolderTreeIcon />
        </EmptyMedia>
        <EmptyTitle className="text-sm">{title}</EmptyTitle>
        {description ? (
          <EmptyDescription className="text-xs">{description}</EmptyDescription>
        ) : null}
      </EmptyHeader>
      {action}
    </Empty>
  )
}
