import { useState } from "react"
import { useTranslation } from "react-i18next"

import { cn } from "@/lib/utils"
import { Badge } from "@/components/ui/badge"
import { Spinner } from "@/components/ui/spinner"
import { ChevronRightIcon, FileDiffIcon, TerminalIcon, WrenchIcon } from "lucide-react"

/** tool_call 消息 payload 的已知形状（codex-acp 实测）。 */
export interface ToolCallPayload {
  toolCallId?: string
  kind?: string
  status?: string
  rawInput?: { command?: string; cwd?: string } & Record<string, unknown>
  rawOutput?: {
    formatted_output?: string
    exit_code?: number
  } & Record<string, unknown>
  content?: {
    type: string
    path?: string
    oldText?: string | null
    newText?: string
  }[]
}

type DiffLine = { type: "same" | "del" | "add"; text: string }

/** 行级 diff：LCS 对齐；超大文件退化为整删整增，避免 O(n·m) 爆内存。 */
function lineDiff(oldText: string, newText: string): DiffLine[] {
  const a = oldText === "" ? [] : oldText.replace(/\n$/, "").split("\n")
  const b = newText === "" ? [] : newText.replace(/\n$/, "").split("\n")

  if (a.length * b.length > 250_000) {
    return [
      ...a.map((text) => ({ type: "del" as const, text })),
      ...b.map((text) => ({ type: "add" as const, text })),
    ]
  }

  // LCS 动态规划表
  const dp: number[][] = Array.from({ length: a.length + 1 }, () =>
    new Array<number>(b.length + 1).fill(0)
  )
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      dp[i][j] =
        a[i] === b[j]
          ? dp[i + 1][j + 1] + 1
          : Math.max(dp[i + 1][j], dp[i][j + 1])
    }
  }

  const lines: DiffLine[] = []
  let i = 0
  let j = 0
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) {
      lines.push({ type: "same", text: a[i] })
      i++
      j++
    } else if (dp[i + 1][j] >= dp[i][j + 1]) {
      lines.push({ type: "del", text: a[i] })
      i++
    } else {
      lines.push({ type: "add", text: b[j] })
      j++
    }
  }
  while (i < a.length) lines.push({ type: "del", text: a[i++] })
  while (j < b.length) lines.push({ type: "add", text: b[j++] })
  return lines
}

function DiffView({
  path,
  oldText,
  newText,
}: {
  path?: string
  oldText: string
  newText: string
}) {
  const lines = lineDiff(oldText, newText)
  return (
    <div className="overflow-hidden rounded-lg border border-border">
      {path ? (
        <div
          className="flex items-center gap-1.5 border-b border-border bg-muted/50 px-2.5 py-1.5 font-mono text-xs text-muted-foreground"
          title={path}
        >
          <FileDiffIcon className="size-3.5 shrink-0" />
          <span className="truncate [direction:rtl] [unicode-bidi:plaintext]">
            {path}
          </span>
        </div>
      ) : null}
      <pre className="max-h-72 overflow-auto bg-background/50 py-1 font-mono text-xs leading-5">
        {lines.map((line, idx) => (
          <div
            key={idx}
            className={cn(
              "px-2.5 whitespace-pre-wrap",
              line.type === "del" &&
                "bg-destructive/10 text-destructive dark:bg-destructive/15",
              line.type === "add" &&
                "bg-primary/10 text-primary dark:bg-primary/15",
              line.type === "same" && "text-muted-foreground"
            )}
          >
            {line.type === "del" ? "- " : line.type === "add" ? "+ " : "  "}
            {line.text}
          </div>
        ))}
      </pre>
    </div>
  )
}

function TerminalView({
  command,
  cwd,
  output,
  exitCode,
}: {
  command?: string
  cwd?: string
  output?: string
  exitCode?: number
}) {
  return (
    <div className="overflow-hidden rounded-lg border border-border">
      {command ? (
        <div
          className="flex items-start gap-1.5 border-b border-border bg-muted/50 px-2.5 py-1.5 font-mono text-xs"
          title={cwd}
        >
          <TerminalIcon className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
          <span className="min-w-0 break-all whitespace-pre-wrap">
            {command}
          </span>
          {exitCode !== undefined && exitCode !== 0 ? (
            <Badge variant="destructive" className="ml-auto shrink-0">
              exit {exitCode}
            </Badge>
          ) : null}
        </div>
      ) : null}
      {output ? (
        <pre className="max-h-72 overflow-auto bg-background/50 px-2.5 py-1.5 font-mono text-xs leading-5 whitespace-pre-wrap text-muted-foreground">
          {output}
        </pre>
      ) : null}
    </div>
  )
}

/** 按工具类型渲染详情：edit → diff，execute/read → 终端，其余 → 原始 JSON。 */
function ToolCallDetail({ payload }: { payload: ToolCallPayload }) {
  const diffs = (payload.content ?? []).filter(
    (c) => c.type === "diff" && typeof c.newText === "string"
  )
  const command = payload.rawInput?.command
  const output = payload.rawOutput?.formatted_output
  const exitCode = payload.rawOutput?.exit_code

  if (diffs.length > 0 || command || output) {
    return (
      <div className="flex flex-col gap-2">
        {diffs.map((diff, idx) => (
          <DiffView
            key={idx}
            path={diff.path}
            oldText={diff.oldText ?? ""}
            newText={diff.newText ?? ""}
          />
        ))}
        {command || output ? (
          <TerminalView
            command={command}
            cwd={payload.rawInput?.cwd}
            output={output}
            exitCode={exitCode}
          />
        ) : null}
      </div>
    )
  }

  // 未知类型：把原始入出参摆出来，总比黑箱强。
  const raw = {
    ...(payload.rawInput ? { input: payload.rawInput } : {}),
    ...(payload.rawOutput ? { output: payload.rawOutput } : {}),
  }
  if (Object.keys(raw).length === 0) return null
  return (
    <pre className="max-h-72 overflow-auto rounded-lg border border-border bg-background/50 px-2.5 py-1.5 font-mono text-xs leading-5 whitespace-pre-wrap text-muted-foreground">
      {JSON.stringify(raw, null, 2)}
    </pre>
  )
}

function hasDetail(payload: ToolCallPayload | null | undefined): boolean {
  if (!payload) return false
  return Boolean(
    (payload.content ?? []).some((c) => c.type === "diff") ||
      payload.rawInput ||
      payload.rawOutput
  )
}

/**
 * 一条工具调用：标题行 + 状态，有详情时可展开（diff / 终端输出 / 原始 JSON）。
 */
export function ToolCallBlock({
  title,
  status,
  payload,
}: {
  title: string
  status?: string
  payload?: ToolCallPayload | null
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const expandable = hasDetail(payload)

  const header = (
    <>
      <WrenchIcon className="size-4 shrink-0" />
      <span className="min-w-0 truncate">{title || t("chat.toolCall")}</span>
      {status ? (
        <Badge
          variant={status === "failed" ? "destructive" : "secondary"}
          className="shrink-0"
        >
          {status === "in_progress" ? <Spinner className="size-3" /> : null}
          {t(`chat.toolStatus.${status}` as never, { defaultValue: status })}
        </Badge>
      ) : null}
    </>
  )

  if (!expandable) {
    return (
      <div className="flex min-h-4 items-center gap-2 text-sm text-muted-foreground">
        <span className="size-4 shrink-0" />
        {header}
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-2">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className="flex min-h-4 w-full items-center gap-2 rounded-md text-left text-sm text-muted-foreground transition-colors outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
      >
        <ChevronRightIcon
          className={cn(
            "size-4 shrink-0 transition-transform",
            open && "rotate-90"
          )}
        />
        {header}
      </button>
      {open && payload ? (
        <div className="pl-6 transition-[opacity,translate] duration-200 ease-snappy starting:-translate-y-0.5 starting:opacity-0 motion-reduce:starting:translate-y-0">
          <ToolCallDetail payload={payload} />
        </div>
      ) : null}
    </div>
  )
}
