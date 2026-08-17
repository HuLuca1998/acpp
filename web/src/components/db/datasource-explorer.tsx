import { useState } from "react"
import { useTranslation } from "react-i18next"

import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import { useAsyncData } from "@/hooks/use-async-data"
import type { DataSource, DbTable, SqlExecResult } from "@/types/acp"
import { SqlResultView } from "@/components/db/sql-result-view"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { PlayIcon, TableIcon } from "lucide-react"

/**
 * 一个数据源的浏览与查询面：左边库/表导航，右边表结构或 SQL 控制台。
 *
 * 这里跑 SQL 的结果与对话里 AI 查询的结果用同一个 SqlResultView——
 * 同一份数据在两处长得不一样是最容易让人读错的那种不一致。
 *
 * 切库、切表靠 key 驱动子组件重挂来归零状态，不在 effect 里逐个清空：
 * 前者是 React 自己的机制，后者会引发级联渲染，还容易漏清一个字段，
 * 把上一个库的表名留在界面上。
 */
export function DataSourceExplorer({ source }: { source: DataSource }) {
  const { t } = useTranslation()
  const { data: databases, error } = useAsyncData(
    () => api.datasources.databases(source.id),
    [source.id]
  )
  const [picked, setPicked] = useState("")

  // 用户没选时的落点：数据源配的默认库 > 第一个业务库 > 第一个库。
  // 省掉「打开就得先选一次」那一步。
  const database =
    picked ||
    source.database ||
    databases?.find((d) => !d.system)?.name ||
    databases?.[0]?.name ||
    ""

  if (error) {
    return <p className="p-4 text-sm text-destructive">{error}</p>
  }
  if (!databases) {
    return <Skeleton className="h-40 w-full" />
  }

  return (
    <div className="flex flex-col gap-3">
      <Select value={database} onValueChange={(v) => setPicked(v ?? "")}>
        <SelectTrigger className="w-full sm:w-72">
          <SelectValue placeholder={t("db.databases")} />
        </SelectTrigger>
        <SelectContent>
          {databases.map((d) => (
            <SelectItem key={d.name} value={d.name}>
              <span className={cn(d.system && "text-muted-foreground")}>
                {d.name}
              </span>
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <DatabasePane key={database} source={source} database={database} />
    </div>
  )
}

function DatabasePane({
  source,
  database,
}: {
  source: DataSource
  database: string
}) {
  const { t } = useTranslation()
  const { data: tables } = useAsyncData(
    () =>
      database
        ? api.datasources.tables(source.id, database)
        : Promise.resolve([] as DbTable[]),
    [source.id, database]
  )
  const [table, setTable] = useState<string | null>(null)

  return (
    <div className="grid gap-3 md:grid-cols-[16rem_1fr]">
      <TableList tables={tables} selected={table} onSelect={setTable} />
      <Tabs defaultValue="schema" className="min-w-0">
        <TabsList>
          <TabsTrigger value="schema">{t("db.columns")}</TabsTrigger>
          <TabsTrigger value="sql">{t("db.console")}</TabsTrigger>
        </TabsList>
        <TabsContent value="schema">
          {table ? (
            <SchemaView
              key={table}
              source={source}
              database={database}
              table={table}
            />
          ) : (
            <p className="rounded-lg border border-border p-4 text-xs text-muted-foreground">
              {t("db.pickTable")}
            </p>
          )}
        </TabsContent>
        <TabsContent value="sql">
          <SqlConsole source={source} database={database} />
        </TabsContent>
      </Tabs>
    </div>
  )
}

function TableList({
  tables,
  selected,
  onSelect,
}: {
  tables: DbTable[] | null
  selected: string | null
  onSelect: (name: string) => void
}) {
  const { t } = useTranslation()

  if (!tables) {
    return (
      <div className="flex flex-col gap-1.5 rounded-lg border border-border p-2">
        {Array.from({ length: 6 }, (_, i) => (
          <Skeleton key={i} className="h-6 w-full" />
        ))}
      </div>
    )
  }
  if (tables.length === 0) {
    return (
      <div className="rounded-lg border border-border p-3 text-xs text-muted-foreground">
        {t("db.tableCount", { count: 0 })}
      </div>
    )
  }

  return (
    <div className="max-h-96 overflow-y-auto rounded-lg border border-border p-1">
      {tables.map((tb) => (
        <button
          key={tb.name}
          type="button"
          onClick={() => onSelect(tb.name)}
          title={tb.comment}
          className={cn(
            "flex w-full items-center gap-1.5 rounded-md px-2 py-1 text-left text-xs",
            selected === tb.name
              ? "bg-accent text-accent-foreground"
              : "hover:bg-muted"
          )}
        >
          <TableIcon className="size-3.5 shrink-0 text-muted-foreground" />
          <span className="min-w-0 flex-1 truncate font-mono">{tb.name}</span>
          <span className="shrink-0 text-muted-foreground tabular-nums">
            {tb.rows}
          </span>
        </button>
      ))}
    </div>
  )
}

function SchemaView({
  source,
  database,
  table,
}: {
  source: DataSource
  database: string
  table: string
}) {
  const { t } = useTranslation()
  const { data: detail } = useAsyncData(
    () => api.datasources.schema(source.id, database, table),
    [source.id, database, table]
  )

  if (!detail) {
    return <Skeleton className="h-40 w-full" />
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="overflow-auto rounded-lg border border-border">
        <table className="w-full border-collapse text-xs">
          <thead className="bg-muted">
            <tr>
              {[
                t("db.columns"),
                t("db.colType"),
                t("db.colNullable"),
                t("db.colKey"),
                t("db.colComment"),
              ].map((h) => (
                <th
                  key={h}
                  scope="col"
                  className="border-b border-border px-2.5 py-1.5 text-left font-medium whitespace-nowrap"
                >
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody className="font-mono">
            {detail.columns.map((c) => (
              <tr
                key={c.name}
                className="border-b border-border/50 last:border-0"
              >
                <td className="px-2.5 py-1 whitespace-nowrap">{c.name}</td>
                <td className="px-2.5 py-1 whitespace-nowrap text-muted-foreground">
                  {c.type}
                </td>
                <td className="px-2.5 py-1 whitespace-nowrap text-muted-foreground">
                  {c.nullable ? "NULL" : "NOT NULL"}
                </td>
                <td className="px-2.5 py-1 whitespace-nowrap text-muted-foreground">
                  {c.key}
                </td>
                <td className="max-w-60 truncate px-2.5 py-1" title={c.comment}>
                  {c.comment}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {detail.indexes.length > 0 ? (
        <div className="flex flex-col gap-1 text-xs">
          <span className="text-muted-foreground">{t("db.indexes")}</span>
          {detail.indexes.map((idx) => (
            <code key={idx.name} className="font-mono text-muted-foreground">
              {idx.unique ? "UNIQUE " : ""}
              {idx.name} ({idx.columns.join(", ")})
            </code>
          ))}
        </div>
      ) : null}

      {detail.ddl ? (
        <details className="text-xs">
          <summary className="cursor-pointer text-muted-foreground">
            {t("db.ddl")}
          </summary>
          <pre className="mt-1.5 max-h-64 overflow-auto rounded-lg border border-border bg-muted/40 p-2.5 font-mono leading-5 whitespace-pre">
            {detail.ddl}
          </pre>
        </details>
      ) : null}
    </div>
  )
}

function SqlConsole({
  source,
  database,
}: {
  source: DataSource
  database: string
}) {
  const { t } = useTranslation()
  const [sql, setSql] = useState("")
  const [running, setRunning] = useState(false)
  const [result, setResult] = useState<SqlExecResult | null>(null)
  const [error, setError] = useState<string | null>(null)

  // 换库时本组件会随 DatabasePane 的 key 一起重挂，上一条结果自然消失
  // ——结果卡片上没写库名，留着会被当成新库的。

  async function run() {
    if (!sql.trim()) return
    setRunning(true)
    setError(null)
    try {
      setResult(await api.datasources.query(source.id, { database, sql }))
    } catch (err) {
      setError((err as Error).message)
      setResult(null)
    } finally {
      setRunning(false)
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <Textarea
        value={sql}
        rows={5}
        spellCheck={false}
        placeholder={t("db.sqlPlaceholder")}
        className="font-mono text-xs"
        onChange={(e) => setSql(e.target.value)}
        onKeyDown={(e) => {
          // ⌘/Ctrl + Enter 执行：SQL 里换行是常态，回车不能当提交。
          if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
            e.preventDefault()
            void run()
          }
        }}
      />
      <div className="flex items-center gap-2">
        <Button size="sm" disabled={running || !sql.trim()} onClick={run}>
          {running ? <Spinner /> : <PlayIcon data-icon="inline-start" />}
          {running ? t("db.running") : t("db.run")}
        </Button>
        <span className="font-mono text-xs text-muted-foreground">⌘↵</span>
      </div>
      {error ? <p className="text-xs text-destructive">{error}</p> : null}
      {result ? <SqlResultView results={result.results} /> : null}
    </div>
  )
}
