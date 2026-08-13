import { memo, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"

import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import { PanelEmptyState } from "@/components/workspace/file-tree-panel"
import { useWorkspace } from "@/components/workspace/workspace-context"

/** 渲染上限：logs 是调试窗口，只保留最近这么多帧，太早的丢弃。 */
const MAX_LINES = 1000
/** 轮询间隔：转录 append-only，Range 续读增量成本极低。 */
const POLL_MS = 2000

interface WireLine {
  /** 转录行号（丢弃早期行后仍然稳定递增）。 */
  seq: number
  time: string
  dir: "send" | "recv"
  /** 一眼可辨的标签：method / result / error。 */
  label: string
  /** 原始帧 JSON（展开时 pretty 显示）。 */
  raw: string
}

let nextSeq = 1

function parseLine(line: string): WireLine | null {
  try {
    const entry = JSON.parse(line) as {
      ts?: string
      dir?: string
      msg?: Record<string, unknown>
    }
    const msg = entry.msg ?? {}
    const label =
      (typeof msg.method === "string" && msg.method) ||
      ("result" in msg ? "result" : "error" in msg ? "error" : "?")
    return {
      seq: nextSeq++,
      time: entry.ts ? new Date(entry.ts).toLocaleTimeString() : "",
      dir: entry.dir === "send" ? "send" : "recv",
      label,
      raw: JSON.stringify(msg),
    }
  } catch {
    return null
  }
}

/**
 * logs 面板：实时跟随会话的线级转录（JSONL，每行一帧 JSON-RPC）。
 * 轮询 + Range 字节续读增量；贴底跟随，用户上翻自动暂停。
 */
export const LogsPanel = memo(function LogsPanel() {
  const { t } = useTranslation()
  const ws = useWorkspace()
  const [lines, setLines] = useState<WireLine[]>([])
  const [expanded, setExpanded] = useState<number | null>(null)
  const offsetRef = useRef(0)
  const bufferRef = useRef("")
  const viewportRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!ws.sessionId) return
    let cancelled = false
    // 防并发重入：首次全量拉取慢于轮询间隔时，interval 会用同一个 offset
    // 再发一次，两个响应各自 append 同一段内容——日志整段翻倍。
    let pulling = false

    async function pull() {
      if (pulling) return
      pulling = true
      try {
        const res = await api.sessions.transcriptChunk(
          ws.sessionId,
          offsetRef.current
        )
        if (cancelled || !res) return
        offsetRef.current = res.nextOffset
        // 末尾可能是正在写入的半行，攒到下一轮补全再解析。
        const text = bufferRef.current + res.chunk
        const parts = text.split("\n")
        bufferRef.current = parts.pop() ?? ""
        const parsed = parts
          .filter((l) => l.trim() !== "")
          .map(parseLine)
          .filter((l): l is WireLine => l !== null)
        if (parsed.length === 0) return
        setLines((prev) => {
          const merged = [...prev, ...parsed]
          return merged.length > MAX_LINES
            ? merged.slice(merged.length - MAX_LINES)
            : merged
        })
      } catch {
        // 拉不到就等下一轮；logs 是旁路调试窗口，不打扰主流程。
      } finally {
        pulling = false
      }
    }

    void pull()
    const timer = window.setInterval(() => void pull(), POLL_MS)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [ws.sessionId])

  // 贴底跟随：内容渲染完成后再滚（在 pull 里滚会拿到旧的 scrollHeight）。
  // 用户上翻查看历史即暂停，滚回底部自动恢复。
  const followRef = useRef(true)
  useEffect(() => {
    if (!followRef.current) return
    const el = viewportRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [lines])

  // dockview 面板挂载瞬间尺寸是 0，那时滚底会被 clamp 回顶部——
  // 尺寸就位/变化时若仍在跟随，重新贴底。
  useEffect(() => {
    const el = viewportRef.current
    if (!el) return
    const ro = new ResizeObserver(() => {
      if (followRef.current) el.scrollTop = el.scrollHeight
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  if (!ws.sessionId) {
    return (
      <PanelEmptyState
        title={t("workspace.tree.draftTitle")}
        description={t("workspace.tree.draftHint")}
      />
    )
  }

  if (lines.length === 0) {
    return (
      <PanelEmptyState
        title={t("workspace.logs.emptyTitle")}
        description={t("workspace.logs.emptyHint")}
      />
    )
  }

  return (
    <div
      ref={viewportRef}
      onScroll={() => {
        const el = viewportRef.current
        // 尺寸未就位（面板刚挂载）时的 scroll 事件不算用户意图。
        if (!el || el.clientHeight === 0) return
        followRef.current =
          el.scrollHeight - el.scrollTop - el.clientHeight < 48
      }}
      className="h-full overflow-y-auto scrollbar-thin bg-background p-2 font-mono text-xs leading-5"
    >
      {lines.map((line) => (
        <div key={line.seq}>
          <button
            type="button"
            className="flex w-full items-baseline gap-2 rounded-sm px-1 text-left hover:bg-muted"
            onClick={() =>
              setExpanded((cur) => (cur === line.seq ? null : line.seq))
            }
          >
            <span className="shrink-0 text-muted-foreground/60 tabular-nums">
              {line.time}
            </span>
            <span
              className={cn(
                "shrink-0",
                line.dir === "send" ? "text-primary" : "text-success"
              )}
            >
              {line.dir === "send" ? "→" : "←"}
            </span>
            <span className="shrink-0 text-foreground">{line.label}</span>
            <span className="min-w-0 truncate text-muted-foreground">
              {line.raw}
            </span>
          </button>
          {expanded === line.seq ? (
            <pre className="my-1 max-h-72 overflow-auto rounded-md border border-border bg-muted/30 p-2 whitespace-pre-wrap">
              {JSON.stringify(JSON.parse(line.raw), null, 2)}
            </pre>
          ) : null}
        </div>
      ))}
    </div>
  )
})
