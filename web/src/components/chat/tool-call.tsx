import { useState } from "react"
import { useTranslation } from "react-i18next"

import {
  isDbQueryCall,
  parseDbToolOutput,
  type ParsedDbResult,
} from "@/lib/db-result"
import { cn } from "@/lib/utils"
import { SqlResultView } from "@/components/db/sql-result-view"
import { DiffView } from "@/components/diff-view"
import { Badge } from "@/components/ui/badge"
import { Spinner } from "@/components/ui/spinner"
import {
  BrainIcon,
  ChevronRightIcon,
  DatabaseIcon,
  FileDiffIcon,
  FileTextIcon,
  GlobeIcon,
  SearchIcon,
  TerminalIcon,
  Trash2Icon,
  WrenchIcon,
  type LucideIcon,
} from "lucide-react"

/** ACP 工具类型 → 图标：一眼分辨读文件/改代码/跑命令，不用点开细看。 */
const KIND_ICONS: Record<string, LucideIcon> = {
  read: FileTextIcon,
  edit: FileDiffIcon,
  delete: Trash2Icon,
  search: SearchIcon,
  execute: TerminalIcon,
  think: BrainIcon,
  fetch: GlobeIcon,
}

/**
 * tool_call 消息 payload 的已知形状。rawOutput 随 runtime 变：
 * codex 是对象 {formatted_output, exit_code}，claude 是纯字符串。
 */
export interface ToolCallPayload {
  toolCallId?: string
  kind?: string
  status?: string
  rawInput?: { command?: string; cwd?: string } & Record<string, unknown>
  rawOutput?:
    | string
    | ({
        formatted_output?: string
        exit_code?: number
      } & Record<string, unknown>)
  content?: {
    type: string
    path?: string
    oldText?: string | null
    newText?: string
  }[]
}

/** 归一化两种 rawOutput 形状，取正文与退出码。 */
function outputOf(payload: ToolCallPayload): {
  output?: string
  exitCode?: number
} {
  const raw = payload.rawOutput
  if (typeof raw === "string") return { output: raw }
  return { output: raw?.formatted_output, exitCode: raw?.exit_code }
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
  const { t } = useTranslation()
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
              {t("chat.exitCode", { code: exitCode })}
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

/**
 * 数据库查询：数据源标识 + 语句 + 字段 + 可滚动数据，与配置页的 SQL
 * 控制台共用 SqlResultView。
 */
function DbQueryView({
  sql,
  parsed,
}: {
  sql: string
  parsed: ParsedDbResult | null
}) {
  // 还没跑完（或结果解析不出来）时先把 SQL 亮出来——等待期间最该看见的
  // 就是「它到底要跑什么」。
  if (!parsed) {
    return (
      <pre className="overflow-auto rounded-lg border border-border bg-background/50 px-2.5 py-1.5 font-mono text-xs leading-5 whitespace-pre-wrap">
        {sql}
      </pre>
    )
  }

  return (
    <div className="flex flex-col gap-2">
      {/* 哪个数据源、哪个库——跑错环境的代价太大，不能只靠记忆。 */}
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <DatabaseIcon className="size-3.5 shrink-0" />
        <span className="font-mono">{parsed.source}</span>
        {parsed.database ? (
          <>
            <span aria-hidden>/</span>
            <span className="font-mono">{parsed.database}</span>
          </>
        ) : null}
        <span className="ml-auto tabular-nums">{parsed.elapsedMs}ms</span>
      </div>
      <SqlResultView results={parsed.results} />
    </div>
  )
}

/** 按工具类型渲染详情：数据库 → 结果表格，edit → diff，execute/read → 终端，其余 → 原始 JSON。 */
function ToolCallDetail({ payload }: { payload: ToolCallPayload }) {
  const diffs = (payload.content ?? []).filter(
    (c) => c.type === "diff" && typeof c.newText === "string"
  )
  const command = payload.rawInput?.command
  const { output, exitCode } = outputOf(payload)

  if (isDbQueryCall(payload.rawInput)) {
    return (
      <DbQueryView
        sql={payload.rawInput.sql}
        parsed={output ? parseDbToolOutput(output) : null}
      />
    )
  }

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
  const Icon = isDbQueryCall(payload?.rawInput)
    ? DatabaseIcon
    : (KIND_ICONS[payload?.kind ?? ""] ?? WrenchIcon)

  const header = (
    <>
      <Icon className="size-4 shrink-0" />
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
