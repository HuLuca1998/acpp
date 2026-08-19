import type { SqlExecResult, SqlStatementResult } from "@/types/acp"

/**
 * 把数据库 MCP 工具的输出文本解析回结构化结果。
 *
 * 为什么要解析：AI 调 db_query 时，结果经 MCP 到达前端只剩一段纯文本
 * （ACP 的 tool_call 不透传结构化内容）。但对话里该看到的是真正的表格
 * ——语句、字段、可滚动的数据——而不是一坨等宽文本。
 *
 * 靠得住的前提是格式两端共同约定：后端 server/internal/datasource/render.go
 * 用制表符分隔输出，单元格里的制表符与换行在那边已经清掉，所以分隔符
 * 不会被数据撞上。改任何一侧都要同步改另一侧。
 *
 * 解析失败一律返回 null——让调用方退回原始文本展示，宁可朴素也不要
 * 编造一张错的表。
 */

/** 首行：`<数据源> · <库> · <n> 条语句 · <ms>ms` */
const HEADER = /^(.+?)\s+·\s+(.+?)\s+·\s+(\d+)\s+条语句\s+·\s+(\d+)ms$/
/** 语句块起始：`[1] SELECT ...` */
const STATEMENT = /^\[(\d+)\]\s+(.*)$/
/** 查询结果元信息：`3 行 · 4ms`（0 行时没有耗时段） */
const QUERY_META = /^(\d+)\s+行(?:\s+·\s+(\d+)ms)?$/
/** 写入结果元信息：`影响 1 行 · 0ms · 自增 id 4` */
const EXEC_META = /^影响\s+(\d+)\s+行\s+·\s+(\d+)ms(?:\s+·\s+自增 id\s+(\d+))?$/
const ERROR_META = /^失败：(.*)$/
const TRUNCATED = /^（只显示前/

/** 解析结果附带数据源标识——卡片要显眼地标出这是哪个环境的库。 */
export interface ParsedDbResult extends SqlExecResult {
  source: string
}

export function parseDbToolOutput(text: string): ParsedDbResult | null {
  if (typeof text !== "string") return null
  const lines = text.split("\n")
  const head = HEADER.exec(lines[0]?.trim() ?? "")
  if (!head) return null

  const parsed: ParsedDbResult = {
    source: head[1],
    database: head[2] === "-" ? "" : head[2],
    results: [],
    elapsedMs: Number(head[4]),
  }

  let i = 1
  while (i < lines.length) {
    const start = STATEMENT.exec(lines[i].trim())
    if (!start) {
      i++
      continue
    }
    const result: SqlStatementResult = {
      statement: start[2],
      kind: "exec",
      rowCount: 0,
      elapsedMs: 0,
    }
    i++
    i = readBody(lines, i, result)
    parsed.results.push(result)
  }

  return parsed.results.length > 0 ? parsed : null
}

/** 读一个语句块的元信息与数据行，返回下一个块的起始行号。 */
function readBody(
  lines: string[],
  i: number,
  result: SqlStatementResult
): number {
  const meta = lines[i]?.trim() ?? ""

  const failed = ERROR_META.exec(meta)
  if (failed) {
    result.error = failed[1]
    return i + 1
  }

  const exec = EXEC_META.exec(meta)
  if (exec) {
    result.affected = Number(exec[1])
    result.elapsedMs = Number(exec[2])
    if (exec[3]) result.lastInsertId = Number(exec[3])
    return i + 1
  }

  const query = QUERY_META.exec(meta)
  if (!query) return i + 1

  result.kind = "query"
  result.rowCount = Number(query[1])
  result.elapsedMs = Number(query[2] ?? 0)
  i++

  // 表头 + 数据行，直到空行或下一个语句块。
  if (result.rowCount > 0 && lines[i] !== undefined && lines[i] !== "") {
    result.columns = lines[i].split("\t")
    i++
    const rows: string[][] = []
    while (i < lines.length && lines[i] !== "" && !STATEMENT.test(lines[i])) {
      if (TRUNCATED.test(lines[i].trim())) {
        result.truncated = true
        i++
        continue
      }
      rows.push(lines[i].split("\t"))
      i++
    }
    result.rows = rows
  }
  return i
}

/**
 * 判断一次工具调用是不是数据库查询。
 * 认参数不认工具名：名字随 runtime 加前缀（claude 是 mcp__acpp-db__db_query，
 * codex 又不一样），而 sql 参数是我们自己定的，稳定得多。
 */
export function isDbQueryCall(
  rawInput: unknown
): rawInput is { sql: string; source?: string } {
  return (
    typeof rawInput === "object" &&
    rawInput !== null &&
    typeof (rawInput as { sql?: unknown }).sql === "string"
  )
}
