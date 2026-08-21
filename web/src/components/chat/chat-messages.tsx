import { memo, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"

import type { Message, PlanEntry, TurnUsage } from "@/types/acp"
import type { LiveToolCall } from "@/hooks/use-chat"
import { cn } from "@/lib/utils"
import { formatDateTime, formatTokens } from "@/lib/format"
import { CopyButton } from "@/components/chat/copy-button"
import { ElicitationAnsweredCard } from "@/components/chat/cards/elicitation-card"
import { MarkdownContent } from "@/components/chat/markdown"
import { PlanHistoryCard } from "@/components/chat/plan-card"
import { UserAvatar } from "@/components/chat/message-shell"
import { ThoughtBlock } from "@/components/chat/thought-block"
import {
  ToolCallBlock,
  type ToolCallPayload,
} from "@/components/chat/tool-call"
import { Badge } from "@/components/ui/badge"
import { Bubble, BubbleContent } from "@/components/ui/bubble"
import { Marker, MarkerContent, MarkerIcon } from "@/components/ui/marker"
import {
  Message as MessageRow,
  MessageAvatar,
  MessageContent,
} from "@/components/ui/message"
import { Spinner } from "@/components/ui/spinner"
import {
  ArrowDownIcon,
  ArrowUpIcon,
  ChevronRightIcon,
  DatabaseIcon,
  FileIcon,
  LinkIcon,
  ShieldCheckIcon,
  WrenchIcon,
} from "lucide-react"

/**
 * 顶部哨兵：滚进视野即自动加载更早的消息——无按钮无等待感，
 * prepend 时视口位置由 preserveScrollOnPrepend 保持不跳。
 */
export function EarlierSentinel({ onVisible }: { onVisible: () => void }) {
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const el = ref.current
    if (!el) return
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) onVisible()
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [onVisible])

  return (
    <div ref={ref} className="flex justify-center py-2">
      <Spinner className="size-4 text-muted-foreground" />
    </div>
  )
}

/** 可折叠的过程块：默认收起，标题行显示条数；busy 时带 spinner。 */
export function ActivitySection({
  count,
  busy = false,
  activeLabel,
  children,
}: {
  count: number
  busy?: boolean
  /** 进行中的那件事（当前工具名等）：busy 时显示在折叠头，收起也能看见系统在干什么。 */
  activeLabel?: string
  children: React.ReactNode
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  return (
    <div className="flex flex-col gap-2">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className="flex w-fit items-center gap-2 rounded-md text-sm text-muted-foreground transition-colors outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
      >
        <ChevronRightIcon
          className={cn("size-4 transition-transform", open && "rotate-90")}
        />
        {busy ? (
          <Spinner className="size-4" />
        ) : (
          <WrenchIcon className="size-4" />
        )}
        <span>{t("chat.activity")}</span>
        <Badge variant="secondary">{count}</Badge>
        {busy && activeLabel ? (
          <span className="max-w-64 text-shimmer truncate font-mono text-xs">
            {activeLabel}
          </span>
        ) : null}
      </button>
      {open ? (
        <div className="ml-2 flex flex-col gap-2 border-l border-border pl-4 transition-[opacity,translate] duration-200 ease-snappy starting:-translate-y-0.5 starting:opacity-0 motion-reduce:starting:translate-y-0">
          {children}
        </div>
      ) : null}
    </div>
  )
}

export function LiveToolMarker({ tool }: { tool: LiveToolCall }) {
  return (
    <ToolCallBlock
      title={tool.title}
      status={tool.status}
      payload={{
        kind: tool.kind,
        rawInput: tool.rawInput as ToolCallPayload["rawInput"],
        rawOutput: tool.rawOutput as ToolCallPayload["rawOutput"],
        content: tool.content as ToolCallPayload["content"],
      }}
    />
  )
}

/** activity 块内的单条过程消息。memo 理由同 ChatMessage。 */
export const ActivityMessage = memo(function ActivityMessage({
  message,
}: {
  message: Message
}) {
  const { t } = useTranslation()

  if (message.kind === "thought") {
    return <ThoughtBlock content={message.content} />
  }

  if (message.kind === "permission_request") {
    // 转录重建的裁决记录：谁请求 + 用户选了什么（取消也如实说）。
    const payload = message.payload as {
      toolKind?: string
      choice?: string
      outcome?: string
    } | null
    const title =
      message.content || payload?.toolKind || t("chat.permission.title")
    const choice =
      payload?.choice ||
      (payload?.outcome === "cancelled" ? t("chat.permission.cancelled") : "")
    return (
      <Marker>
        <MarkerIcon>
          <ShieldCheckIcon />
        </MarkerIcon>
        <MarkerContent>
          {choice ? t("chat.permission.resolved", { title, choice }) : title}
        </MarkerContent>
      </Marker>
    )
  }

  const payload = (message.payload ?? null) as ToolCallPayload | null
  return (
    <ToolCallBlock
      title={message.content}
      status={payload?.status}
      payload={payload}
    />
  )
})

/** 正文消息：用户消息用主色气泡靠右并带头像，agent 输出渲染 markdown
 *  （头像与左侧对齐槽由外层 AgentRow 给，因为工具卡、计划卡要缩进同样多）。
 *  memo 是必须的——流式期间每个 chunk 都触发页面重渲染，
 *  没有它整份历史的 markdown 会一遍遍重新解析。 */
export const ChatMessage = memo(function ChatMessage({
  message,
  userName,
}: {
  message: Message
  /** 会话创建者的名字，人这侧的头像用它取首字母。 */
  userName?: string
}) {
  const { t, i18n } = useTranslation()
  const timestamp = formatDateTime(message.createdAt, i18n.language)

  if (message.kind === "elicitation") {
    return <ElicitationAnsweredCard message={message} />
  }

  if (message.kind === "plan") {
    // 计划的轮末快照：默认折叠成一行进度。
    const entries = (message.payload?.entries ?? null) as PlanEntry[] | null
    if (!entries?.length) return null
    return <PlanHistoryCard entries={entries} />
  }

  if (message.role === "user") {
    // 附件（图片 / @ 引用文件 / @ 引用数据库）由发送入参或转录重建带回
    // payload。linkedFiles 是大文件的按需引用子集（未内嵌，agent 自行读取）；
    // datasources 是 mysql:// 形状的数据库引用 URI。
    const payload = message.payload as {
      images?: { data: string; mimeType: string }[]
      files?: string[]
      linkedFiles?: string[]
      datasources?: string[]
    } | null
    const linked = new Set(payload?.linkedFiles ?? [])
    return (
      <MessageRow align="end">
        {/* align=end 是 flex-row-reverse，头像作为第一个子元素落在右侧。 */}
        <MessageAvatar className="self-start bg-transparent">
          <UserAvatar name={userName} />
        </MessageAvatar>
        <MessageContent>
          <div
            className="group/msg flex items-center justify-end gap-1.5"
            title={timestamp}
          >
            <CopyButton
              text={message.content}
              className="opacity-0 transition-opacity duration-150 group-hover/msg:opacity-100 focus-visible:opacity-100"
            />
            {/* 80% 行宽上限放在这层（参照全宽消息行）；Bubble 自带的
                max-w-[80%] 参照的是本列（内容宽），会把短消息也挤折行。 */}
            <div className="flex max-w-[80%] min-w-0 flex-col items-end gap-1.5">
              {payload?.images?.length ? (
                <div className="flex flex-wrap justify-end gap-1.5">
                  {payload.images.map((img, index) => (
                    <img
                      key={index}
                      src={`data:${img.mimeType};base64,${img.data}`}
                      alt=""
                      className="max-h-40 max-w-60 rounded-lg border border-border object-contain"
                    />
                  ))}
                </div>
              ) : null}
              {payload?.datasources?.length ? (
                <div className="flex flex-wrap justify-end gap-1.5">
                  {payload.datasources.map((uri) => (
                    <span
                      key={uri}
                      title={uri}
                      className="flex h-6 items-center gap-1 rounded-full border border-border px-2 text-xs text-muted-foreground"
                    >
                      <DatabaseIcon className="size-3" />
                      <span className="max-w-40 truncate font-mono">
                        {uri.replace(/^mysql:\/\//, "")}
                      </span>
                    </span>
                  ))}
                </div>
              ) : null}
              {payload?.files?.length ? (
                <div className="flex flex-wrap justify-end gap-1.5">
                  {payload.files.map((path) => (
                    <span
                      key={path}
                      title={
                        linked.has(path)
                          ? `${path} · ${t("chat.attachments.linkedHint")}`
                          : path
                      }
                      className="flex h-6 items-center gap-1 rounded-full border border-border px-2 text-xs text-muted-foreground"
                    >
                      {linked.has(path) ? (
                        <LinkIcon className="size-3" />
                      ) : (
                        <FileIcon className="size-3" />
                      )}
                      <span className="max-w-40 truncate font-mono">
                        {path.split("/").pop()}
                      </span>
                    </span>
                  ))}
                </div>
              ) : null}
              {message.content ? (
                <Bubble variant="default" align="end" className="max-w-full">
                  <BubbleContent className="whitespace-pre-wrap">
                    {message.content}
                  </BubbleContent>
                </Bubble>
              ) : null}
            </div>
          </div>
        </MessageContent>
      </MessageRow>
    )
  }

  const turnUsage = (message.payload as { turnUsage?: TurnUsage } | null)
    ?.turnUsage
  return (
    <div className="group/msg" title={timestamp}>
      <MarkdownContent>{message.content}</MarkdownContent>
      {/* 操作行占位固定高度，浮现时不推挤下方内容。 */}
      <div className="mt-1 flex h-6 items-center gap-1 opacity-0 transition-opacity duration-150 group-hover/msg:opacity-100 focus-within:opacity-100">
        <CopyButton text={message.content} />
        {turnUsage ? (
          <span
            className="flex items-center gap-1 text-xs text-muted-foreground/70 tabular-nums"
            title={t("chat.status.turnTokensHint", {
              input: turnUsage.inputTokens.toLocaleString(),
              output: turnUsage.outputTokens.toLocaleString(),
              cached: turnUsage.cachedReadTokens.toLocaleString(),
              total: turnUsage.totalTokens.toLocaleString(),
            })}
          >
            <ArrowUpIcon className="size-3" />
            {formatTokens(turnUsage.inputTokens)}
            <ArrowDownIcon className="size-3" />
            {formatTokens(turnUsage.outputTokens)}
          </span>
        ) : null}
      </div>
    </div>
  )
})
