import { useTranslation } from "react-i18next"

import type { WorkspaceScopeApi } from "@/lib/api"
import { useAsyncData } from "@/hooks/use-async-data"
import type { DataSource } from "@/types/acp"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { StatusDot } from "@/components/status-dot"
import { DatabaseIcon, TableIcon, XIcon } from "lucide-react"

/**
 * `/db` 的结果卡：浮在输入框上方，只给用户看。
 *
 * 两级——`/db` 列本项目的数据源、`/db <环境>` 列那条连接的表。中间没有
 * 「选库」这一层：一条连接只对应一个库。
 *
 * 刻意不进对话流：查一眼有哪些库是「顺手看看」，不该变成一条消息占着
 * 上下文，也不该等 agent 响应。关掉即走，刷新不留痕。
 *
 * 每行右侧有「引用」——看到想问的那张表，就在这里带走，不必关掉面板再去
 * @ 菜单把同一条路重走一遍。
 *
 * 能看到什么与 AI 能操作什么是同一个范围——都只有当前工作目录所属项目的
 * 数据源（会话态走 /sessions/{id}/datasources，草稿态走 /workspace?cwd=，
 * 两条路后端都按项目过滤）。
 */
export function DbSlashPanel({
  sessionId,
  scope,
  /** 命令参数：空 = 列数据源，`<env>` = 列那条连接的表。 */
  args,
  onPick,
  onClose,
}: {
  sessionId: number
  /** 数据面作用域：会话态查会话的项目，草稿态经 `/workspace?cwd=` 查目录的。 */
  scope: WorkspaceScopeApi
  args: string
  /** 把这条引用带进输入框的附件区（与 @ 菜单同一个入口）。 */
  onPick: (ref: string) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [envArg] = args.split(/\s+/).filter(Boolean)
  const { data: sources, error } = useAsyncData(
    () => scope.datasources(sessionId),
    [sessionId, scope]
  )

  const picked = envArg ? pickSource(sources ?? [], envArg) : null

  return (
    <div className="pointer-events-auto mb-2 overflow-hidden rounded-xl border border-border bg-card/95 backdrop-blur-xl transition-[opacity,translate] duration-200 ease-snappy starting:translate-y-1 starting:opacity-0 motion-reduce:starting:translate-y-0">
      <div className="flex items-center gap-1.5 border-b border-border px-3 py-1.5 text-xs">
        <DatabaseIcon className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="font-medium">{t("db.title")}</span>
        {picked ? (
          <span className="font-mono text-muted-foreground">{picked.ref}</span>
        ) : null}
        <Button
          variant="ghost"
          size="icon-sm"
          className="ml-auto"
          aria-label={t("common.cancel")}
          onClick={onClose}
        >
          <XIcon />
        </Button>
      </div>

      <ScrollArea className="max-h-64 p-2 text-xs">
        {error ? (
          <p className="px-1 py-2 text-destructive">{error}</p>
        ) : !sources ? (
          <Skeleton className="h-16 w-full" />
        ) : sources.length === 0 ? (
          <p className="px-1 py-2 text-muted-foreground">
            {t("db.noSourceForProject")}
          </p>
        ) : !envArg ? (
          <SourceList sources={sources} onPick={onPick} />
        ) : !picked ? (
          <p className="px-1 py-2 text-muted-foreground">
            {t("db.pickSource")}：{sources.map((s) => s.env).join(" / ")}
          </p>
        ) : (
          <TableList
            sessionId={sessionId}
            scope={scope}
            source={picked}
            database={picked.database}
            onPick={onPick}
          />
        )}
      </ScrollArea>
    </div>
  )
}

function SourceList({
  sources,
  onPick,
}: {
  sources: DataSource[]
  onPick: (ref: string) => void
}) {
  const { t } = useTranslation()
  return (
    <ul className="flex flex-col gap-0.5">
      {sources.map((s) => (
        <Row key={s.id} refValue={s.ref} onPick={onPick}>
          <StatusDot tone={s.disabled ? "muted" : "success"} />
          <span className="shrink-0 font-mono font-medium">{s.env}</span>
          <span className="min-w-0 truncate font-mono text-muted-foreground">
            {s.host}:{s.port}
            {s.database ? `/${s.database}` : ""}
          </span>
          {s.disabled ? (
            <span className="shrink-0 text-muted-foreground">
              {t("db.disabled")}
            </span>
          ) : null}
        </Row>
      ))}
    </ul>
  )
}

/**
 * 一行：内容照旧，右侧跟一个默认隐身的「引用」按钮。
 *
 * 按钮**占位**（只改 opacity，不用 absolute）：行右侧本来就排着注释与
 * 行数，绝对定位会直接压在上面；而且占位后 hover 不会让文字重排。
 * 与 @ 菜单里那行是同一套做法。
 */
function Row({
  refValue,
  onPick,
  children,
}: {
  refValue: string
  onPick: (ref: string) => void
  children: React.ReactNode
}) {
  const { t } = useTranslation()
  return (
    <li className="group flex items-center gap-2 rounded-md px-1 py-1 hover:bg-muted/60">
      {children}
      <Button
        size="xs"
        variant="ghost"
        className="shrink-0 transition-colors"
        onClick={() => onPick(refValue)}
      >
        {t("db.refPick")}
      </Button>
    </li>
  )
}

function TableList({
  sessionId,
  scope,
  source,
  database,
  onPick,
}: {
  sessionId: number
  scope: WorkspaceScopeApi
  source: DataSource
  database: string
  onPick: (ref: string) => void
}) {
  const { t } = useTranslation()
  const { data, error } = useAsyncData(
    () => scope.datasourceTables(sessionId, source.id, database),
    [sessionId, scope, source.id, database]
  )

  if (error) return <p className="px-1 py-2 text-destructive">{error}</p>
  if (!data) return <Skeleton className="h-16 w-full" />
  if (data.length === 0) {
    return (
      <p className="px-1 py-2 text-muted-foreground">
        {t("db.tableCount", { count: 0 })}
      </p>
    )
  }

  return (
    <ul className="flex flex-col gap-0.5">
      {data.map((tb) => (
        // 引用值与 @ 菜单里那条完全一致：`<项目>/<环境>/<库>/<表>`。
        <Row
          key={tb.name}
          refValue={`${source.ref}/${database}/${tb.name}`}
          onPick={onPick}
        >
          <TableIcon className="size-3.5 shrink-0 text-muted-foreground" />
          <span className="min-w-0 flex-1 truncate font-mono">{tb.name}</span>
          {tb.comment ? (
            <span className="max-w-40 min-w-0 truncate text-muted-foreground">
              {tb.comment}
            </span>
          ) : null}
          <span className="shrink-0 text-muted-foreground tabular-nums">
            {tb.rows}
          </span>
        </Row>
      ))}
    </ul>
  )
}

/** 按环境名或完整 `<项目>/<环境>` 找数据源，与后端 Resolve 同一套写法。 */
function pickSource(sources: DataSource[], ref: string): DataSource | null {
  const lower = ref.toLowerCase()
  return (
    sources.find(
      (s) => s.env.toLowerCase() === lower || s.ref.toLowerCase() === lower
    ) ?? null
  )
}
