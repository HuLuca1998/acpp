import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useTranslation } from "react-i18next"

import {
  ChangeStat,
  GitPanelHeader,
  StatusLetter,
} from "@/components/workspace/panels/git-parts"
import { PanelEmptyState } from "@/components/workspace/panels/panel-empty-state"
import {
  useGitOverview,
  useGitSelection,
  useWorkspace,
} from "@/components/workspace/workspace-context"
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu"
import { Spinner } from "@/components/ui/spinner"
import { buildPathTree, countFiles, type PathTreeNode } from "@/lib/path-tree"
import { cn } from "@/lib/utils"
import type { GitFileChange } from "@/types/acp"
import {
  ChevronDownIcon,
  ChevronRightIcon,
  FileIcon,
  FolderIcon,
} from "lucide-react"

/**
 * 变更面板（右上）。取代了原来的 diff 面板——它们是同一个东西的两种
 * 数据源：文件清单 + 点开看改动。这里的清单**跟随选择态**：
 *
 * - 没选提交 → 工作区里尚未提交的改动
 * - 选了提交 → 那条提交动过的文件
 * - 选了两个 ref → 两者对比涉及的文件
 *
 * 点文件不在本面板里展开，而是送进文件查看器（preview）以 diff 模式打开：
 * 一个窄面板里既列清单又铺全文，两件事都做不好。
 */
export const ChangesPanel = memo(function ChangesPanel() {
  const { t } = useTranslation()
  const ws = useWorkspace()
  const selection = useGitSelection()
  const git = useGitOverview()

  const [files, setFiles] = useState<GitFileChange[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const tokenRef = useRef(0)
  const staleRef = useRef(0)

  const comparing = selection.refs.length === 2
  const [base, head] = selection.refs

  // 同 history：首屏加载态由 files === null 表达，spinner 留给显式刷新。
  //
  // token 是 stale 守卫：选择切得比请求快时，旧结果必须被丢掉——否则
  // 面板显示的是上一次点的那条提交的文件。
  const load = useCallback(() => {
    if (!ws.sessionId) return
    const token = ++tokenRef.current
    staleRef.current = token

    const request = comparing
      ? ws.scope
          .gitCompare(ws.sessionId, base, head)
          .then((compare) => compare.files)
      : selection.sha
        ? ws.scope
            .gitCommit(ws.sessionId, selection.sha)
            .then((detail) => detail.files)
        : Promise.resolve(null)

    request
      .then((data) => {
        if (staleRef.current !== token) return
        // null 表示「看工作区」——那份数据在共享的 gitStore 里，不重复拉。
        if (data === null) {
          setFiles(null)
          ws.refreshGit()
          setError(null)
          return
        }
        setFiles(data)
        setError(null)
      })
      .catch((err: Error) => {
        if (staleRef.current === token) setError(err.message)
      })
      .finally(() => {
        if (staleRef.current === token) setLoading(false)
      })
  }, [ws, comparing, base, head, selection.sha])

  useEffect(() => {
    load()
    return ws.onWorkspaceRefresh(load)
  }, [load, ws])

  // 工作区模式下用共享的 gitStore，避免同一份数据两处拉。
  const list = comparing || selection.sha ? files : (git.data?.files ?? null)
  // 树只在文件清单变化时重建：大变更集（几百个文件）每次渲染重建一遍纯属
  // 浪费，而这个面板会被选择态与 git 刷新频繁带着重渲染。
  const tree = useMemo(
    () => (list ? buildPathTree(list, (file) => file.path) : null),
    [list]
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
      <PanelEmptyState title={t("common.loadFailed")} description={error} />
    )
  }

  const title = comparing
    ? `${base} → ${head}`
    : selection.sha
      ? selection.sha.slice(0, 7)
      : t("workspace.git.workingTree")

  const openDiff = (path: string) => {
    // 提交/对比模式下看的是「那时的改动」，工作区模式看的是当前改动。
    ws.openDiff(path, selection.sha ?? undefined)
  }

  const askAboutFile = (path: string) => {
    ws.askAI(
      comparing
        ? t("workspace.git.promptFileCompare", { path, base, head })
        : t("workspace.git.promptFile", {
            path,
            where: selection.sha
              ? selection.sha.slice(0, 7)
              : t("workspace.git.workingTree"),
          })
    )
  }

  return (
    <div className="flex h-full flex-col">
      <GitPanelHeader
        title={title}
        hint={
          list
            ? t("workspace.git.fileCount", { count: list.length })
            : undefined
        }
        loading={loading || git.loading}
        onRefresh={() => {
          setLoading(true)
          load()
        }}
      />
      <div className="min-h-0 flex-1 overflow-y-auto py-1">
        {list === null || tree === null ? (
          <div className="flex items-center justify-center py-6">
            <Spinner className="size-4 text-muted-foreground" />
          </div>
        ) : list.length === 0 ? (
          <PanelEmptyState title={t("workspace.git.noChanges")} />
        ) : (
          <ChangeTree
            node={tree}
            depth={0}
            onOpen={openDiff}
            onAsk={askAboutFile}
            onReference={ws.addReference}
          />
        )}
      </div>
    </div>
  )
})

/**
 * 变更树的一层：目录可折叠（默认展开——变更集通常不大，一进来就该看见
 * 全部文件），文件行点开进查看器的 diff 模式。
 */
function ChangeTree({
  node,
  depth,
  onOpen,
  onAsk,
  onReference,
}: {
  node: PathTreeNode<GitFileChange>
  depth: number
  onOpen: (path: string) => void
  onAsk: (path: string) => void
  onReference: (path: string) => void
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
            <StatusLetter status={item.status} />
            <FileIcon className="size-3.5 shrink-0 text-muted-foreground/70" />
            <span className="min-w-0 flex-1 truncate font-mono">{name}</span>
            <ChangeStat added={item.added} deleted={item.deleted} />
          </ContextMenuTrigger>
          <ContextMenuContent className="w-52">
            <ContextMenuItem onClick={() => onAsk(item.path)}>
              {t("workspace.git.askFile")}
            </ContextMenuItem>
            <ContextMenuSeparator />
            <ContextMenuItem onClick={() => onReference(item.path)}>
              {t("workspace.refMenu.addReference")}
            </ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
      ))}
    </div>
  )
}
