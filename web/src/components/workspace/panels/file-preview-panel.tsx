import {
  memo,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react"
import { useTranslation } from "react-i18next"
import type { IDockviewPanelProps } from "dockview-react"
import {
  AtSignIcon,
  CodeIcon,
  ExternalLinkIcon,
  EyeIcon,
  FileTextIcon,
  LocateFixedIcon,
} from "lucide-react"

import { cn } from "@/lib/utils"
import type { GitDiffView, TableView, WorkspaceFile } from "@/types/acp"
import { DiffView } from "@/components/diff-view"
import { Hint } from "@/components/hint"
import { MarkdownContent } from "@/components/chat/markdown"
import { ChatPanelContext } from "@/components/workspace/chat-panel-context"
import {
  hasRichView,
  hasSourceView,
  isHtmlFile,
  isMarkdownFile,
  isTableFile,
  previewKind,
} from "@/components/workspace/panels/file-preview-kind"
import { MediaPreview } from "@/components/workspace/panels/file-preview-media"
import { PanelRefreshButton } from "@/components/workspace/panels/panel-refresh"
import { TablePreview } from "@/components/workspace/panels/file-preview-table"
import {
  usePreviewTarget,
  useWorkspace,
} from "@/components/workspace/workspace-context"
import { useVisibleLoad } from "@/hooks/use-panel-visible"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Spinner } from "@/components/ui/spinner"

/** 一次预览请求的结果，key 是它对应的目标（见下面的 requestKey）。 */
type PreviewResult = {
  key: string
  file?: WorkspaceFile
  diff?: GitDiffView
  table?: TableView
  error?: string
}

/** 预览渲染的行数上限：更大的文件截断展示，虚拟滚动是 M4 的事。 */
const MAX_RENDER_LINES = 5000

/** diff 渲染的行数上限（同样是虚拟滚动前的兜底）。 */
const DIFF_MAX_LINES = 2000

/**
 * 刷新拿回同样的内容时，保留旧结果对象。
 *
 * 工作区刷新一轮能来好几次，而一个文件多数时候根本没变。正文是几千个
 * DOM 节点，换一个新对象就等于整篇重渲一遍——内容一模一样，用户只会
 * 看到滚动位置与选中文字被白白抹掉。
 *
 * 表格不比：几万个格子逐个比对比重渲还贵，照常换。
 */
function keepIfSame(next: PreviewResult) {
  return (prev: PreviewResult | null): PreviewResult => {
    if (!prev || prev.key !== next.key || prev.error !== next.error) return next
    if (next.file && prev.file) {
      return prev.file.content === next.file.content &&
        prev.file.binary === next.file.binary &&
        prev.file.truncated === next.file.truncated
        ? prev
        : next
    }
    if (next.diff && prev.diff) {
      return prev.diff.oldText === next.diff.oldText &&
        prev.diff.newText === next.diff.newText &&
        prev.diff.binary === next.diff.binary
        ? prev
        : next
    }
    return next
  }
}

/**
 * 文件查看器面板：只读、等宽、行号，两种形态——
 * **file** 看文件当前内容，**diff** 看它改了什么（工作区改动或某条提交）。
 *
 * 合成一个面板是刻意的：这两件事是同一个阅读动作的两面，各占一个 tab
 * 只会让人在「现在什么样」和「改了什么」之间来回找。形态由命令总线的
 * 预览目标决定（文件树点文件 → file，变更面板点文件 → diff）。
 */
export const FilePreviewPanel = memo(function FilePreviewPanel(
  props: IDockviewPanelProps
) {
  const { t } = useTranslation()
  const ws = useWorkspace()
  const target = usePreviewTarget()
  const path = target?.path ?? null
  const mode = target?.mode ?? "file"
  const sha = target?.sha
  // 跟随定位的行号：只在 file 模式有意义。
  const targetLine = mode === "file" ? target?.line : undefined
  // 一次预览请求的落地结果，**带着它对应的目标**。
  //
  // 不拆成 file/diff/table/loading/error 五个 state，是为了不在 effect 里
  // 先同步清一遍：那样每换一个文件就白多一次渲染（React 会先用清空后的
  // 状态渲一帧），大文件与大表格上看得出来。目标由 requestKey 表达，
  // 它一变，上一份结果自动失效，不必谁去清。
  const [loaded, setLoaded] = useState<PreviewResult | null>(null)
  // markdown 默认看渲染后的样子——打开一个 README 是为了读它，不是读它的
  // 语法；要看源码点一下切过去。
  const [raw, setRaw] = useState(false)
  // 手动刷新的世代号。图片/音视频/PDF/html 不经我们的 fetch，由浏览器
  // 按 URL 自己取——URL 不变它就不会再请求，所以刷新时把这个号拼进
  // 地址，逼它重新拿一份。
  const [reloadKey, setReloadKey] = useState(0)
  const bodyRef = useRef<HTMLDivElement>(null)
  // 请求世代：切文件比请求回来快是常事，只认最后发出的那一次。
  const seqRef = useRef(0)

  // 图片/音视频/PDF 走浏览器原生渲染，不拉正文——把一个 mp4 读成字符串
  // 只会得到一堆乱码和一次白拉的流量。
  const media = mode === "diff" ? null : previewKind(path)
  // 「渲染形态 vs 源码形态」：markdown 看排版、html 看页面、csv/xlsx 看表格。
  // diff 永远是源码——那一栏比的是文本改动。
  const rich = mode !== "diff" && !raw && hasRichView(path)
  const showTable = rich && isTableFile(path)
  const showHtml = rich && isHtmlFile(path)

  // 这次要看的东西的身份。跟随定位时同一文件只变行号，key 不变，不重拉。
  const requestKey = `${mode}|${sha ?? ""}|${showTable ? "table" : ""}|${path ?? ""}`
  const current = loaded?.key === requestKey ? loaded : null
  const file = current?.file ?? null
  const diff = current?.diff ?? null
  const table = current?.table ?? null
  const error = current?.error ?? null
  // 媒体与 html 渲染形态不拉正文，也就无所谓「加载中」。
  const needsFetch = Boolean(path && ws.sessionId && !media && !showHtml)
  const loading = needsFetch && current === null

  const load = useCallback(() => {
    // 这几种都不需要正文：媒体与 html 渲染形态由浏览器按 URL 自己取。
    if (!path || !ws.sessionId || media || showHtml) return
    const key = requestKey
    const seq = ++seqRef.current

    const request = showTable
      ? ws.scope
          .workspaceTable(ws.sessionId, path)
          .then((view) => ({ table: view }))
      : mode === "diff"
        ? sha
          ? ws.scope
              .gitCommitFile(ws.sessionId, sha, path)
              .then((view) => ({ diff: view }))
          : ws.scope
              .gitDiff(ws.sessionId, path)
              .then((view) => ({ diff: view }))
        : ws.scope
            .workspaceFile(ws.sessionId, path)
            .then((view) => ({ file: view }))

    request
      .then((result) => {
        if (seqRef.current === seq) setLoaded(keepIfSame({ key, ...result }))
      })
      .catch((err) => {
        if (seqRef.current === seq) {
          setLoaded(
            keepIfSame({
              key,
              error: err instanceof Error ? err.message : String(err),
            })
          )
        }
      })
  }, [
    path,
    mode,
    sha,
    media,
    showHtml,
    showTable,
    requestKey,
    ws.sessionId,
    ws.scope,
  ])

  // 打开即加载 + 跟着工作区刷新走（agent 每干完一件事、每轮结束都会广播）。
  // 藏在 tab 后面时不白拉，切回来那一刻补一次——查看器的正文是全篇文本，
  // 看不见的时候每隔一会儿拉一遍是纯浪费。
  //
  // load 的引用跟着 requestKey 变，所以换文件/换形态同样由它驱动，
  // 不必再留一个只为首次加载的 effect。
  useVisibleLoad(props.api, ws.onWorkspaceRefresh, load)

  // 手动刷新：正文重拉，交给浏览器渲染的那几种换个地址逼它重取。
  const refresh = useCallback(() => {
    setReloadKey((n) => n + 1)
    load()
  }, [load])

  // 媒体与 html 的地址：带上世代号，手动刷新才能绕过浏览器缓存。
  const inlineUrl = path
    ? `${ws.scope.previewUrl(ws.sessionId, path)}${reloadKey ? `&v=${reloadKey}` : ""}`
    : ""

  // 定位到行：行高固定 leading-5（20px），content-visibility 的估算尺寸
  // 与之一致，按行数换算滚动位置即可，顶部留三行上下文。
  useEffect(() => {
    if (!targetLine || !file || file.binary) return
    if (rich) return
    const el = bodyRef.current
    if (el) el.scrollTop = Math.max(0, (targetLine - 1) * 20 - 60)
  }, [targetLine, file, rich])

  const { lines, clipped } = useMemo(() => {
    if (!file || file.binary) return { lines: [], clipped: false }
    const all = file.content.split("\n")
    if (all.length > MAX_RENDER_LINES) {
      return { lines: all.slice(0, MAX_RENDER_LINES), clipped: true }
    }
    return { lines: all, clipped: false }
  }, [file])

  if (!path) {
    return (
      <Empty className="h-full justify-center">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <FileTextIcon />
          </EmptyMedia>
          <EmptyTitle className="text-sm">
            {t("workspace.preview.emptyTitle")}
          </EmptyTitle>
          <EmptyDescription className="text-xs">
            {t("workspace.preview.emptyHint")}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className="flex h-full flex-col [contain:strict]">
      <div className="flex h-8 shrink-0 items-center gap-1.5 border-b border-border px-3">
        {loading ? <Spinner className="size-3" /> : null}
        <span
          className="min-w-0 flex-1 truncate font-mono text-xs text-muted-foreground [direction:rtl] [unicode-bidi:plaintext]"
          title={path}
        >
          {path}
        </span>
        <PanelRefreshButton
          label={t("workspace.preview.refresh")}
          desc={t("workspace.preview.refreshDesc")}
          align="center"
          onRefresh={refresh}
        />
        <FollowToggle />
        {hasRichView(path) && hasSourceView(path) && mode !== "diff" ? (
          <Hint
            label={t(
              raw ? "workspace.preview.rendered" : "workspace.preview.source"
            )}
            desc={t("workspace.preview.sourceDesc")}
          >
            <button
              type="button"
              aria-label={t(
                raw ? "workspace.preview.rendered" : "workspace.preview.source"
              )}
              className="flex size-6 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97]"
              onClick={() => setRaw((prev) => !prev)}
            >
              {raw ? (
                <EyeIcon className="size-3.5" />
              ) : (
                <CodeIcon className="size-3.5" />
              )}
            </button>
          </Hint>
        ) : null}
        {target?.mode === "diff" ? (
          <span className="shrink-0 font-mono text-[10px] text-muted-foreground/70">
            {target.sha
              ? target.sha.slice(0, 7)
              : t("workspace.git.workingTree")}
          </span>
        ) : null}
        {mode !== "diff" ? (
          <Hint
            label={t("workspace.preview.openExternal")}
            desc={t("workspace.preview.openExternalDesc")}
          >
            <button
              type="button"
              aria-label={t("workspace.preview.openExternal")}
              className="flex size-6 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97]"
              onClick={() =>
                window.open(
                  ws.scope.previewUrl(ws.sessionId, path),
                  "_blank",
                  "noopener,noreferrer"
                )
              }
            >
              <ExternalLinkIcon className="size-3.5" />
            </button>
          </Hint>
        ) : null}
        <Hint
          label={t("workspace.refMenu.addReference")}
          desc={t("workspace.refMenu.addReferenceDesc")}
          align="end"
        >
          <button
            type="button"
            aria-label={t("workspace.refMenu.addReference")}
            className="flex size-6 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97]"
            onClick={() => ws.addReference(path)}
          >
            <AtSignIcon className="size-3.5" />
          </button>
        </Hint>
      </div>
      <div ref={bodyRef} className="min-h-0 flex-1 overflow-auto">
        {showHtml ? (
          <iframe
            src={inlineUrl}
            title={path}
            // 服务端给 html 的 inline 响应打了 CSP sandbox，这里的 sandbox
            // 属性是第二道：页面里的脚本一律不跑，它与我们同源。
            sandbox=""
            className="size-full border-0 bg-background"
          />
        ) : media ? (
          <MediaPreview
            kind={media}
            src={inlineUrl}
            name={path}
          />
        ) : error ? (
          // 预览失败（多半是文件太大或格式解析不了）不该是条死路：说清
          // 原因，同时把「交给浏览器」这条出路摆在旁边。
          <div className="flex flex-col items-start gap-2 p-3 text-xs">
            <span className="text-destructive">{error}</span>
            <button
              type="button"
              className="rounded-md text-primary transition-colors duration-150 hover:underline"
              onClick={() =>
                window.open(
                  ws.scope.previewUrl(ws.sessionId, path),
                  "_blank",
                  "noopener,noreferrer"
                )
              }
            >
              {t("workspace.preview.openExternal")}
            </button>
          </div>
        ) : diff ? (
          diff.binary ? (
            <div className="p-3 text-xs text-muted-foreground">
              {t("workspace.git.binary")}
            </div>
          ) : (
            <div className="p-2">
              <DiffView
                oldText={diff.oldText}
                newText={diff.newText}
                maxLines={DIFF_MAX_LINES}
              />
            </div>
          )
        ) : file?.binary ? (
          <div className="flex flex-col items-start gap-2 p-3 text-xs text-muted-foreground">
            <span>{t("workspace.preview.binary")}</span>
            {/* 认不出的格式也给条出路：浏览器打得开就打开，打不开它会
                退成下载，两种结果都比一句「二进制文件」有用。 */}
            <button
              type="button"
              className="rounded-md text-primary transition-colors duration-150 hover:underline"
              onClick={() =>
                window.open(
                  ws.scope.previewUrl(ws.sessionId, path),
                  "_blank",
                  "noopener,noreferrer"
                )
              }
            >
              {t("workspace.preview.openExternal")}
            </button>
          </div>
        ) : table ? (
          <TablePreview view={table} />
        ) : file && isMarkdownFile(path) && !raw ? (
          <div className="px-4 py-3">
            <MarkdownContent>{file.content}</MarkdownContent>
          </div>
        ) : file ? (
          <div className="w-max min-w-full py-2 font-mono text-xs leading-5">
            {lines.map((line, i) => (
              <div
                key={i}
                // contain-intrinsic-size 必须带 auto：固定值让浏览器用估算
                // 高度堆叠所有行，面板尺寸一变（拖动分栏、切布局）累积误差
                // 就会把后半段留成空白；auto 让它记住实测高度再复用。
                className={cn(
                  "flex [contain-intrinsic-block-size:auto_1.25rem] [content-visibility:auto]",
                  targetLine === i + 1 && "bg-primary/10"
                )}
              >
                <span className="w-12 shrink-0 pr-3 text-right text-muted-foreground/60 tabular-nums select-none">
                  {i + 1}
                </span>
                <span className="pr-4 whitespace-pre">{line}</span>
              </div>
            ))}
            {file.truncated || clipped ? (
              <div className="px-12 py-2 text-muted-foreground">
                {t("workspace.preview.truncated")}
              </div>
            ) : null}
          </div>
        ) : null}
      </div>
    </div>
  )
})

/**
 * 跟随视图开关：agent 每触碰一个新文件（ACP locations），查看器自动切过去。
 * 默认关闭——自动抢焦点必须是用户主动选的。
 *
 * 单独成组件而不是写在面板里，是为了**把聊天上下文的订阅关在这一小块**。
 * 面板本体一旦消费 ChatPanelContext，正文分片每 80ms 换一次状态就会把整个
 * 查看器（可能正渲染着几千行代码或一张大表）跟着重渲一遍——哪怕这个面板
 * 此刻藏在别的 tab 后面。这里只渲染一个 24px 的按钮，重渲多少次都无所谓。
 */
function FollowToggle() {
  const { t } = useTranslation()
  const ws = useWorkspace()
  const chatPanel = useContext(ChatPanelContext)
  const [follow, setFollow] = useState(false)
  const head =
    follow && chatPanel?.chat.busy ? chatPanel.chat.touched[0] : undefined
  useEffect(() => {
    if (!head) return
    ws.openPreview(head.path, head.line)
  }, [head, ws])

  if (!chatPanel) return null
  return (
    <Hint
      label={t(
        follow ? "workspace.preview.followOff" : "workspace.preview.followOn"
      )}
      desc={t("workspace.preview.followDesc")}
    >
      <button
        type="button"
        aria-pressed={follow}
        aria-label={t(
          follow ? "workspace.preview.followOff" : "workspace.preview.followOn"
        )}
        className={cn(
          "flex size-6 shrink-0 items-center justify-center rounded-md transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted active:scale-[0.97]",
          follow
            ? "text-primary"
            : "text-muted-foreground hover:text-foreground"
        )}
        onClick={() => setFollow((prev) => !prev)}
      >
        <LocateFixedIcon className="size-3.5" />
      </button>
    </Hint>
  )
}
