import { useState, type ReactNode } from "react"
import { useTranslation } from "react-i18next"
import {
  ChevronDownIcon,
  ChevronRightIcon,
  FileIcon,
  FolderIcon,
} from "lucide-react"

import {
  ChangeStat,
  StatusLetter,
} from "@/components/workspace/panels/git-parts"
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu"
import { countFiles, type PathTreeNode } from "@/lib/path-tree"
import { cn } from "@/lib/utils"
import type { GitFileChange } from "@/types/acp"

/** 工作区视图里的一组变更（已跟踪 / 未跟踪），组头可折叠。 */
export function ChangeGroup({
  title,
  count,
  children,
}: {
  title: string
  count: number
  children: ReactNode
}) {
  const [collapsed, setCollapsed] = useState(false)
  return (
    <div>
      <button
        type="button"
        className="flex w-full items-center gap-1.5 px-2 py-1 text-left text-[11px] font-medium text-muted-foreground transition-colors duration-150 hover:text-foreground"
        onClick={() => setCollapsed((prev) => !prev)}
      >
        {collapsed ? (
          <ChevronRightIcon className="size-3.5 shrink-0" />
        ) : (
          <ChevronDownIcon className="size-3.5 shrink-0" />
        )}
        <span className="min-w-0 flex-1 truncate">{title}</span>
        <span className="shrink-0 text-muted-foreground/60 tabular-nums">
          {count}
        </span>
      </button>
      {collapsed ? null : children}
    </div>
  )
}

/**
 * 变更树的一层：目录可折叠（默认展开——变更集通常不大，一进来就该看见
 * 全部文件），文件行点开进查看器的 diff 模式。
 */
export function ChangeTree({
  node,
  depth,
  onOpen,
  onAsk,
  onReference,
  onPreview,
  onDownload,
  onCopy,
  onDiscard,
  onAskDir,
}: {
  node: PathTreeNode<GitFileChange>
  depth: number
  onOpen: (path: string) => void
  onAsk: (path: string) => void
  onReference: (path: string) => void
  onPreview: (path: string) => void
  onDownload: (path: string) => void
  onCopy: (value: string) => void
  /** 只有看工作区时才给：提交里的改动没什么可丢的。 */
  onDiscard?: (path: string) => void
  onAskDir: (dir: string) => void
}) {
  const { t } = useTranslation()
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})

  return (
    <div>
      {node.dirs.map((dir) => {
        const isCollapsed = collapsed[dir.path]
        return (
          <div key={dir.path}>
            <button
              type="button"
              title={dir.path}
              className="flex w-full items-center gap-1.5 py-1 pr-2.5 text-left text-xs text-muted-foreground transition-colors duration-150 hover:bg-accent hover:text-foreground"
              style={{ paddingLeft: `${depth * 12 + 8}px` }}
              onClick={() =>
                setCollapsed((prev) => ({
                  ...prev,
                  [dir.path]: !prev[dir.path],
                }))
              }
            >
              {isCollapsed ? (
                <ChevronRightIcon className="size-3.5 shrink-0" />
              ) : (
                <ChevronDownIcon className="size-3.5 shrink-0" />
              )}
              <FolderIcon className="size-3.5 shrink-0" />
              <span className="min-w-0 flex-1 truncate font-mono">
                {dir.name}
              </span>
              <span className="shrink-0 text-muted-foreground/60 tabular-nums">
                {countFiles(dir)}
              </span>
            </button>
            {isCollapsed ? null : (
              <ChangeTree
                node={dir}
                depth={depth + 1}
                onOpen={onOpen}
                onAsk={onAsk}
                onReference={onReference}
                onPreview={onPreview}
                onDownload={onDownload}
                onCopy={onCopy}
                onDiscard={onDiscard}
                onAskDir={onAskDir}
              />
            )}
          </div>
        )
      })}

      {node.files.map(({ name, item }) => (
        <ContextMenu key={item.path}>
          <ContextMenuTrigger
            render={
              <button
                type="button"
                title={item.path}
                className={cn(
                  "flex w-full items-center gap-2 py-1 pr-2.5 text-left text-xs transition-colors duration-150 hover:bg-accent"
                )}
                style={{ paddingLeft: `${depth * 12 + 8}px` }}
                onClick={() => onOpen(item.path)}
              />
            }
          >
            <StatusLetter status={item.untracked ? "U" : item.status} />
            <FileIcon className="size-3.5 shrink-0 text-muted-foreground/70" />
            <span className="min-w-0 flex-1 truncate font-mono">{name}</span>
            <ChangeStat added={item.added} deleted={item.deleted} />
          </ContextMenuTrigger>
          <ContextMenuContent className="w-56">
            <ContextMenuItem onClick={() => onAsk(item.path)}>
              {t("workspace.git.askFile")}
            </ContextMenuItem>
            <ContextMenuSeparator />
            <ContextMenuItem onClick={() => onPreview(item.path)}>
              {t("workspace.git.openCurrent")}
            </ContextMenuItem>
            <ContextMenuItem onClick={() => onReference(item.path)}>
              {t("workspace.refMenu.addReference")}
            </ContextMenuItem>
            <ContextMenuItem onClick={() => onDownload(item.path)}>
              {t("workspace.refMenu.download")}
            </ContextMenuItem>
            <ContextMenuItem onClick={() => onCopy(item.path)}>
              {t("workspace.git.copyPath")}
            </ContextMenuItem>
            {onDiscard ? (
              <>
                <ContextMenuSeparator />
                <ContextMenuItem
                  variant="destructive"
                  onClick={() => onDiscard(item.path)}
                >
                  {t("workspace.git.discardFile")}
                </ContextMenuItem>
              </>
            ) : null}
          </ContextMenuContent>
        </ContextMenu>
      ))}
    </div>
  )
}
