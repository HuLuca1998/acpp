import { useState } from "react"
import { useTranslation } from "react-i18next"

import {
  isDbQueryCall,
  parseDbToolOutput,
  type ParsedDbResult,
} from "@/lib/db-result"
import { MarkdownContent } from "@/components/chat/markdown"
import { Marker, MarkerContent, MarkerIcon } from "@/components/ui/marker"
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
 * tool_call 消息 payload 的已知形状。rawOutput 有三种：codex 的内建工具
 * 是对象 {formatted_output, exit_code}，claude 的内建工具是纯字符串，
 * **MCP 工具**（含我们自己的 acpp-db）则是 MCP 的 content 数组
 * `[{type:"text", text}]`——三种都要认，漏一种那类工具就只剩黑箱。
 */
export interface ToolCallPayload {
  toolCallId?: string
  kind?: string
  status?: string
  rawInput?: { command?: string; cwd?: string } & Record<string, unknown>
  rawOutput?:
    | string
    | { type?: string; text?: string }[]
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
  /** 这次调用派出了子代理——它的产出是 markdown 报告，不是终端输出。 */
  isSubagent?: boolean
}

/** 归一化三种 rawOutput 形状，取正文与退出码。 */
function outputOf(payload: ToolCallPayload): {
  output?: string
  exitCode?: number
} {
  const raw = payload.rawOutput
  if (typeof raw === "string") return { output: raw }
  if (Array.isArray(raw)) {
    // MCP 的 content 数组：拼接全部文本块（我们的工具只产一块，
    // 但别的 MCP server 可能分多块返回）。
    const text = raw
      .filter((part) => typeof part?.text === "string")
      .map((part) => part.text)
      .join("\n")
    return { output: text || undefined }
  }
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
  source,
  parsed,
}: {
  sql: string
  /** 入参里的数据源 ref；项目只有一个数据源时 AI 可省略。 */
  source?: string
  parsed: ParsedDbResult | null
}) {
  const { t } = useTranslation()
  // 还没跑完（或结果解析不出来）时也要标出目标：跑错环境的代价太大，
  // 语句正对着 prod 跑的那一刻最该看见。入参没带 source 就如实标
  // 「默认数据源」，等结果头部到达后换成权威标识。
  if (!parsed) {
    return (
      <div className="flex flex-col gap-2">
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <DatabaseIcon className="size-3.5 shrink-0" />
          <span className="font-mono">{source || t("db.defaultSource")}</span>
        </div>
        <pre className="overflow-auto rounded-lg border border-border bg-background/50 px-2.5 py-1.5 font-mono text-xs leading-5 whitespace-pre-wrap">
          {sql}
        </pre>
      </div>
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

  // 子代理交回来的是 AI 写的 markdown 报告（标题、表格、代码块），塞进
  // 终端视图会把 ## 和 | 原样摆出来。反过来 Bash/Read 的输出是原始文本，
  // 用 markdown 渲染只会毁掉它（缩进变代码块、* 变列表），所以只认子代理。
  if (payload.isSubagent && output) {
    return (
      <div className="rounded-md border border-border bg-muted/30 px-3 py-2">
        <MarkdownContent className="text-sm">{output}</MarkdownContent>
      </div>
    )
  }

  if (isDbQueryCall(payload.rawInput)) {
    return (
      <DbQueryView
        sql={payload.rawInput.sql}
        source={payload.rawInput.source}
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
      <MarkerIcon>
        <Icon />
      </MarkerIcon>
      <MarkerContent className="truncate">
        {title || t("chat.toolCall")}
      </MarkerContent>
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
      <Marker>
        {/* 占位对齐可展开项的箭头槽，一列工具的图标才在同一条竖线上。 */}
        <span className="size-4 shrink-0" />
        {header}
      </Marker>
    )
  }

  return (
    <div className="flex flex-col gap-2">
      <Marker
        render={
          <button
            type="button"
            onClick={() => setOpen((o) => !o)}
            aria-expanded={open}
          />
        }
        className="rounded-md transition-colors outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
      >
        <ChevronRightIcon
          className={cn(
            "size-4 shrink-0 transition-transform",
            open && "rotate-90"
          )}
        />
        {header}
      </Marker>
      {open && payload ? (
        <div className="pl-6 transition-[opacity,translate] duration-200 ease-snappy starting:-translate-y-0.5 starting:opacity-0 motion-reduce:starting:translate-y-0">
          <ToolCallDetail payload={payload} />
        </div>
      ) : null}
    </div>
  )
}
