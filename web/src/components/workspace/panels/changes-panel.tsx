import { memo, useCallback, useMemo, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import type { IDockviewPanelProps } from "dockview-react"
import { toast } from "sonner"

import { useVisibleLoad } from "@/hooks/use-panel-visible"

import { GitPanelHeader } from "@/components/workspace/panels/git-parts"
import {
  ChangeGroup,
  ChangeTree,
} from "@/components/workspace/panels/change-tree"
import { PanelEmptyState } from "@/components/workspace/panels/panel-empty-state"
import {
  GitConfirmDialog,
  type GitConfirm,
} from "@/components/workspace/panels/git-dialogs"
import { copyText } from "@/lib/clipboard"
import {
  useGitOverview,
  useGitSelection,
  useWorkspace,
} from "@/components/workspace/workspace-context"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Spinner } from "@/components/ui/spinner"
import { buildPathTree } from "@/lib/path-tree"
import type { GitFileChange } from "@/types/acp"

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
export const ChangesPanel = memo(function ChangesPanel(
  props: IDockviewPanelProps
) {
  const { t } = useTranslation()
  const ws = useWorkspace()
  const selection = useGitSelection()
  const git = useGitOverview()

  const [files, setFiles] = useState<GitFileChange[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const tokenRef = useRef(0)
  const staleRef = useRef(0)
  const [confirm, setConfirm] = useState<GitConfirm | null>(null)

  const comparing = selection.refs.length === 2
  const [base, head] = selection.refs

  // 同 history：首屏加载态由 files === null 表达，spinner 留给显式刷新。
  //
  // token 是 stale 守卫：选择切得比请求快时，旧结果必须被丢掉——否则
  // 面板显示的是上一次点的那条提交的文件。
  const load = useCallback(() => {
    if (!ws.ready) return
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

  // 藏在 tab 后面时不响应刷新广播，切回来再补一次。
  useVisibleLoad(props.api, ws.onWorkspaceRefresh, load)

  // 工作区模式下用共享的 gitStore，避免同一份数据两处拉。
  const workingTree = !comparing && !selection.sha
  const list = workingTree ? (git.data?.files ?? null) : files
  // 树只在文件清单变化时重建：大变更集（几百个文件）每次渲染重建一遍纯属
  // 浪费，而这个面板会被选择态与 git 刷新频繁带着重渲染。
  //
  // 看工作区时分两组：已跟踪文件的改动与 git 还没跟踪的新文件是两类事——
  // 前者是「改了什么」，后者常是忘了 ignore 的产物，混在一棵树里互相淹没。
  // 提交/对比里不存在未跟踪文件，只有一组、不画组头。
  const groups = useMemo(() => {
    if (!list) return null
    const build = (items: GitFileChange[]) =>
      buildPathTree(items, (file) => file.path)
    if (!workingTree) return [{ key: "all", files: list, tree: build(list) }]
    const tracked = list.filter((file) => !file.untracked)
    const untracked = list.filter((file) => file.untracked)
    return [
      { key: "tracked", files: tracked, tree: build(tracked) },
      { key: "untracked", files: untracked, tree: build(untracked) },
    ].filter((group) => group.files.length > 0)
  }, [list, workingTree])

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

  /** 丢弃单个文件的改动：只在看工作区时成立（提交里的改动没什么可丢的）。 */
  const discardFile = (path: string) =>
    setConfirm({
      title: t("workspace.git.discardFile"),
      description: t("workspace.git.discardFileDesc", { path }),
      confirmLabel: t("workspace.git.discard"),
      onConfirm: async () => {
        try {
          await ws.scope.gitDiscard(ws.sessionId, [path])
          ws.refreshWorkspace()
          toast.success(t("workspace.git.discarded"))
        } catch (err) {
          toast.error((err as Error).message)
        }
      },
    })

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
      <ScrollArea className="min-h-0 flex-1 py-1">
        {list === null || groups === null ? (
          <div className="flex items-center justify-center py-6">
            <Spinner className="size-4 text-muted-foreground" />
          </div>
        ) : list.length === 0 ? (
          <PanelEmptyState title={t("workspace.git.noChanges")} />
        ) : (
          groups.map((group) => {
            const tree = (
              <ChangeTree
                node={group.tree}
                depth={0}
                onOpen={openDiff}
                onAsk={askAboutFile}
                onReference={ws.addReference}
                onPreview={ws.openPreview}
                onDownload={ws.downloadFile}
                onCopy={(value) => void copyText(value)}
                onDiscard={workingTree ? discardFile : undefined}
                onAskDir={(dir) =>
                  ws.askAI(t("workspace.git.promptDir", { dir }))
                }
              />
            )
            if (!workingTree) return <div key={group.key}>{tree}</div>
            return (
              <ChangeGroup
                key={group.key}
                title={
                  group.key === "untracked"
                    ? t("workspace.git.untrackedFiles")
                    : t("workspace.git.trackedChanges")
                }
                count={group.files.length}
              >
                {tree}
              </ChangeGroup>
            )
          })
        )}
      </ScrollArea>

      <GitConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </div>
  )
})
