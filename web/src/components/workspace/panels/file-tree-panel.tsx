import { memo, useCallback, useContext, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import type { IDockviewPanelProps } from "dockview-react"

import { useVisibleLoad } from "@/hooks/use-panel-visible"
import {
  ChevronRightIcon,
  FileIcon,
  FolderIcon,
  RotateCwIcon,
} from "lucide-react"

import { displayPath } from "@/lib/format"
import {
  buildChangeMap,
  CHANGE_TONE,
  dirChangeKind,
  type FileChangeKind,
} from "@/lib/git-status"
import { cn } from "@/lib/utils"
import type { TreeEntry } from "@/types/acp"
import { useIdentity } from "@/hooks/identity-context"
import { PanelEmptyState } from "@/components/workspace/panels/panel-empty-state"
import { ChatPanelContext } from "@/components/workspace/chat-panel-context"
import {
  useGitOverview,
  useWorkspace,
} from "@/components/workspace/workspace-context"
import { Hint } from "@/components/hint"
import { StatusDot } from "@/components/status-dot"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Button } from "@/components/ui/button"
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@/components/ui/context-menu"
import { Spinner } from "@/components/ui/spinner"

/**
 * 空目录的 children 占位。用同一个常量而不是每次 `[]`：引用稳定，
 * TreeNode 的 memo 才拦得住无谓重渲染。
 */
const NO_CHILDREN: TreeEntry[] = []

/**
 * 树根的短标签：路径长就只留最后两段。
 *
 * 不用 CSS 的 `direction: rtl` 截断——那会把开头的 `/` 甩到末尾，
 * `/Users/luca/acpp` 显示成 `Users/luca/acpp/`，看着像另一个路径。
 * 悬停仍给完整路径。
 */
function rootLabel(path: string): string {
  const segs = path.split("/").filter(Boolean)
  if (segs.length <= 2) return path
  return `…/${segs.slice(-2).join("/")}`
}

/**
 * 文件树面板：首屏一次拉两层（后端 depth=2），更深展开时按目录懒加载。
 * 懒加载的子目录内容放 childrenByPath，不回写树结构，保持更新扁平。
 */
export const FileTreePanel = memo(function FileTreePanel(
  props: IDockviewPanelProps
) {
  const { t } = useTranslation()
  const ws = useWorkspace()
  const [entries, setEntries] = useState<TreeEntry[] | null>(null)
  // 树根的绝对路径：顶栏要显示「这棵树是哪个目录」——会话的工作目录和
  // 工作区根长得像，不写出来根本分不清自己在看哪一个。
  const [root, setRoot] = useState("")
  const [truncated, setTruncated] = useState(false)
  const identity = useIdentity().identity
  const git = useGitOverview()
  // 变更着色跟着共享的 git 汇总走：turn 结束与手动刷新都会重取，
  // 文件树不必自己再问一遍。
  const changes = useMemo(() => buildChangeMap(git.data), [git.data])
  // agent 本轮触碰的文件（ACP locations）：亮一个呼吸状态点。上下文不一定
  // 存在；用路径拼接的 key 记忆，聊天流的高频 chunk 不会换 Set 引用、
  // 也就不会把整棵树拖着重渲染。
  const chatPanel = useContext(ChatPanelContext)
  const touchedKey =
    chatPanel?.chat.busy && chatPanel.chat.touched.length > 0
      ? chatPanel.chat.touched.map((l) => l.path).join("\n")
      : ""
  const touched = useMemo(
    () => new Set(touchedKey ? touchedKey.split("\n") : []),
    [touchedKey]
  )
  const [error, setError] = useState<string | null>(null)
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [childrenByPath, setChildrenByPath] = useState<
    Map<string, TreeEntry[]>
  >(new Map())
  // 懒加载失败的目录：没有它，失败等于「children 永远是 undefined」，
  // 那个位置的 spinner 就会一直转下去，看着像卡死而不是出错。
  const [failed, setFailed] = useState<Set<string>>(new Set())

  const load = useCallback(() => {
    if (!ws.ready) return
    ws.scope
      .workspaceTree(ws.sessionId, { depth: 2 })
      .then((listing) => {
        setError(null)
        setRoot(listing.root)
        setEntries(listing.entries)
        setTruncated(listing.truncated ?? false)
        // 首屏默认展开第一层目录（即「默认展开 2 层」——根内容 + 一级目录内容全部可见）。
        setExpanded(
          new Set(
            listing.entries.filter((e) => e.kind === "dir").map((e) => e.path)
          )
        )
        setChildrenByPath(new Map())
        setFailed(new Set())
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : String(err))
      })
  }, [ws.ready, ws.sessionId, ws.scope])

  // turn 结束后的工作区广播：树跟着 git 数据一起重拉。
  // 藏在 tab 后面时不白拉，切回来再补。
  useVisibleLoad(props.api, ws.onWorkspaceRefresh, load)

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
      // 未展开过的深层目录：补拉一层。**必须走 ws.scope**——草稿态没有会话，
      // 请求得打 /workspace?cwd=，写死 api.sessions 会打到 /sessions/0 上，
      // 失败又被静默吞掉，表现就是展开一个目录后永远转圈。
      if (!entry.listed && !childrenByPath.has(entry.path)) {
        setFailed((prev) => {
          if (!prev.has(entry.path)) return prev
          const next = new Set(prev)
          next.delete(entry.path)
          return next
        })
        void ws.scope
          .workspaceTree(ws.sessionId, { path: entry.path, depth: 1 })
          .then((listing) => {
            setChildrenByPath((prev) =>
              new Map(prev).set(entry.path, listing.entries)
            )
          })
          // 失败不打断整棵树，只在这一行标出来（收起再展开即重试）。
          .catch(() => setFailed((prev) => new Set(prev).add(entry.path)))
      }
    },
    [ws.scope, ws.sessionId, childrenByPath]
  )

  if (!ws.ready) {
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
      <div className="flex h-8 shrink-0 items-center gap-1 px-2">
        {/* 树根：路径长就从头部省略——尾巴（当前目录名）才是要认的那截。 */}
        <Hint label={root || t("workspace.panels.files")} align="start">
          <span className="min-w-0 flex-1 truncate text-left text-xs text-muted-foreground">
            {rootLabel(displayPath(root, identity?.root))}
          </span>
        </Hint>
        <Hint
          label={t("workspace.tree.refresh")}
          desc={t("workspace.tree.refreshDesc")}
          align="end"
        >
          <button
            type="button"
            aria-label={t("workspace.tree.refresh")}
            className="flex size-6 items-center justify-center rounded-md text-muted-foreground transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97]"
            onClick={load}
          >
            <RotateCwIcon className="size-3.5" />
          </button>
        </Hint>
      </div>
      <ScrollArea className="min-h-0 flex-1 px-1 pb-2">
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
              failed={failed}
              onToggle={toggleDir}
              onOpenFile={ws.openPreview}
              onAddReference={ws.addReference}
              onDownload={ws.downloadFile}
              changes={changes}
              touched={touched}
            />
          ))
        )}
        {truncated ? (
          <div className="px-2 py-1 text-xs text-muted-foreground">
            {t("workspace.tree.truncated")}
          </div>
        ) : null}
      </ScrollArea>
    </div>
  )
})

/** 单个树节点：固定行高，展开箭头只动 transform；右键出引用菜单。
 *  memo：树挂在聊天上下文下，流式 chunk 会带着父组件高频重渲染，
 *  props 不变的节点不该跟着跑。 */
const TreeNode = memo(function TreeNode({
  entry,
  depth,
  expanded,
  childrenByPath,
  failed,
  onToggle,
  onOpenFile,
  onAddReference,
  onDownload,
  changes,
  touched,
}: {
  entry: TreeEntry
  depth: number
  expanded: Set<string>
  childrenByPath: Map<string, TreeEntry[]>
  failed: Set<string>
  onToggle: (entry: TreeEntry) => void
  onOpenFile: (path: string) => void
  onAddReference: (path: string) => void
  onDownload: (path: string, archive?: boolean) => void
  /** 绝对路径 → git 状态，用来给条目着色。 */
  changes: Map<string, FileChangeKind>
  /** agent 本轮触碰过的绝对路径：条目尾部亮呼吸点，轮结束即灭。 */
  touched: Set<string>
}) {
  const { t } = useTranslation()
  const isDir = entry.kind === "dir"
  // 目录跟着内部的变更走：不展开也能看出哪一支动过。
  const tone = isDir
    ? dirChangeKind(entry.path, changes)
    : changes.get(entry.path)
  const isOpen = isDir && expanded.has(entry.path)
  // `listed` = 后端已经列过这个目录：此时没有 children 就是**空目录**，
  // 不是「还没加载」。少了这一层判断，首屏展开的空目录会挂着一个永远
  // 转不完的 spinner（后端对空目录回的是 children: null）。
  const children =
    entry.children ??
    (entry.listed ? NO_CHILDREN : childrenByPath.get(entry.path))
  // 目录冒泡：收起时也能看出 agent 正在哪一支里干活。
  const isTouched = isDir
    ? [...touched].some((p) => p.startsWith(`${entry.path}/`))
    : touched.has(entry.path)

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
          {/* 变更着色只落在名字上：树本来就密，底色会让人看不清层级。 */}
          <span className={cn("truncate", tone && CHANGE_TONE[tone])}>
            {entry.name}
          </span>
          {isTouched ? (
            <span title={t("workspace.tree.touched")} className="ml-auto pr-1">
              <StatusDot tone="success" pulse />
            </span>
          ) : null}
        </ContextMenuTrigger>
        <ContextMenuContent>
          <ContextMenuItem onClick={() => onAddReference(entry.path)}>
            {t("workspace.refMenu.addReference")}
          </ContextMenuItem>
          {isDir ? (
            <ContextMenuItem onClick={() => onDownload(entry.path, true)}>
              {t("workspace.refMenu.downloadZip")}
            </ContextMenuItem>
          ) : (
            <>
              <ContextMenuItem onClick={() => onOpenFile(entry.path)}>
                {t("workspace.refMenu.openPreview")}
              </ContextMenuItem>
              <ContextMenuItem onClick={() => onDownload(entry.path)}>
                {t("workspace.refMenu.download")}
              </ContextMenuItem>
            </>
          )}
          <ContextMenuItem
            onClick={() => void navigator.clipboard.writeText(entry.path)}
          >
            {t("workspace.refMenu.copyPath")}
          </ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>
      {isOpen && children && children.length === 0 ? (
        <div
          className="flex h-6 items-center px-1.5 text-xs text-muted-foreground/70"
          style={{ paddingLeft: `${(depth + 1) * 14 + 6}px` }}
        >
          {t("workspace.tree.empty")}
        </div>
      ) : null}
      {isOpen && children
        ? children.map((child) => (
            <TreeNode
              key={child.path}
              entry={child}
              depth={depth + 1}
              expanded={expanded}
              childrenByPath={childrenByPath}
              failed={failed}
              onToggle={onToggle}
              onOpenFile={onOpenFile}
              onAddReference={onAddReference}
              onDownload={onDownload}
              changes={changes}
              touched={touched}
            />
          ))
        : null}
      {isOpen && !children ? (
        <div
          className="flex h-6 items-center px-1.5 text-xs text-muted-foreground"
          style={{ paddingLeft: `${(depth + 1) * 14 + 6}px` }}
        >
          {/* 加载失败要说出来：一个永远转的圈看着像卡死，用户只会一直等。
              收起再展开即重试。 */}
          {failed.has(entry.path) ? (
            <span className="text-destructive/80">
              {t("workspace.tree.loadFailed")}
            </span>
          ) : (
            <Spinner className="size-3" />
          )}
        </div>
      ) : null}
    </>
  )
})
