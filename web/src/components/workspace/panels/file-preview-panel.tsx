import { memo, useContext, useEffect, useMemo, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import {
  AtSignIcon,
  CodeIcon,
  ExternalLinkIcon,
  EyeIcon,
  FileTextIcon,
  LocateFixedIcon,
} from "lucide-react"

import { cn } from "@/lib/utils"
import type { GitDiffView, WorkspaceFile } from "@/types/acp"
import { DiffView } from "@/components/diff-view"
import { Hint } from "@/components/hint"
import { MarkdownContent } from "@/components/chat/markdown"
import { ChatPanelContext } from "@/components/workspace/chat-panel-context"
import { previewKind } from "@/components/workspace/panels/file-preview-kind"
import { MediaPreview } from "@/components/workspace/panels/file-preview-media"
import {
  usePreviewTarget,
  useWorkspace,
} from "@/components/workspace/workspace-context"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Spinner } from "@/components/ui/spinner"

/** 预览渲染的行数上限：更大的文件截断展示，虚拟滚动是 M4 的事。 */
const MAX_RENDER_LINES = 5000

/** diff 渲染的行数上限（同样是虚拟滚动前的兜底）。 */
const DIFF_MAX_LINES = 2000

/**
 * 文件查看器面板：只读、等宽、行号，两种形态——
 * **file** 看文件当前内容，**diff** 看它改了什么（工作区改动或某条提交）。
 *
 * 合成一个面板是刻意的：这两件事是同一个阅读动作的两面，各占一个 tab
 * 只会让人在「现在什么样」和「改了什么」之间来回找。形态由命令总线的
 * 预览目标决定（文件树点文件 → file，变更面板点文件 → diff）。
 */
export const FilePreviewPanel = memo(function FilePreviewPanel() {
  const { t } = useTranslation()
  const ws = useWorkspace()
  const target = usePreviewTarget()
  const path = target?.path ?? null
  const mode = target?.mode ?? "file"
  const sha = target?.sha
  // 跟随定位的行号：只在 file 模式有意义。
  const targetLine = mode === "file" ? target?.line : undefined
  const [file, setFile] = useState<WorkspaceFile | null>(null)
  const [diff, setDiff] = useState<GitDiffView | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // markdown 默认看渲染后的样子——打开一个 README 是为了读它，不是读它的
  // 语法；要看源码点一下切过去。
  const [raw, setRaw] = useState(false)
  const bodyRef = useRef<HTMLDivElement>(null)

  // 跟随模式：agent 每触碰一个新文件（locations），查看器自动切过去。
  // 只在会话页有聊天上下文时提供；默认关闭——自动抢焦点必须是用户主动选的。
  const chatPanel = useContext(ChatPanelContext)
  const [follow, setFollow] = useState(false)
  const head =
    follow && chatPanel?.chat.busy ? chatPanel.chat.touched[0] : undefined
  useEffect(() => {
    if (!head) return
    ws.openPreview(head.path, head.line)
  }, [head, ws])

  // 图片/音视频/PDF 走浏览器原生渲染，不拉正文——把一个 mp4 读成字符串
  // 只会得到一堆乱码和一次白拉的流量。
  const media = mode === "diff" ? null : previewKind(path)

  // 依赖收窄到字段级：跟随时同一文件只变行号，不该整个重拉一遍内容。
  useEffect(() => {
    if (!path || !ws.sessionId || media) return
    let stale = false
    setLoading(true)
    setError(null)
    setFile(null)
    setDiff(null)

    const request =
      mode === "diff"
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
        if (stale) return
        if ("diff" in result) setDiff(result.diff)
        else setFile(result.file)
      })
      .catch((err) => {
        if (!stale) setError(err instanceof Error ? err.message : String(err))
      })
      .finally(() => {
        if (!stale) setLoading(false)
      })
    return () => {
      stale = true
    }
  }, [path, mode, sha, media, ws.sessionId, ws.scope])

  // 定位到行：行高固定 leading-5（20px），content-visibility 的估算尺寸
  // 与之一致，按行数换算滚动位置即可，顶部留三行上下文。
  useEffect(() => {
    if (!targetLine || !file || file.binary) return
    if (isMarkdown(path) && !raw) return
    const el = bodyRef.current
    if (el) el.scrollTop = Math.max(0, (targetLine - 1) * 20 - 60)
  }, [targetLine, file, path, raw])

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
        {chatPanel ? (
          <Hint
            label={t(
              follow
                ? "workspace.preview.followOff"
                : "workspace.preview.followOn"
            )}
            desc={t("workspace.preview.followDesc")}
          >
            <button
              type="button"
              aria-pressed={follow}
              aria-label={t(
                follow
                  ? "workspace.preview.followOff"
                  : "workspace.preview.followOn"
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
        ) : null}
        {isMarkdown(path) && target?.mode !== "diff" ? (
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
        {media ? (
          <MediaPreview
            kind={media}
            src={ws.scope.previewUrl(ws.sessionId, path)}
            name={path}
          />
        ) : error ? (
          <div className="p-3 text-xs text-destructive">{error}</div>
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
        ) : file && isMarkdown(path) && !raw ? (
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

/** 只按扩展名判断：内容嗅探对 markdown 不可靠，也没必要。 */
function isMarkdown(path: string | null): boolean {
  if (!path) return false
  const lower = path.toLowerCase()
  return lower.endsWith(".md") || lower.endsWith(".markdown")
}
