import { useState } from "react"
import { useTranslation } from "react-i18next"

import type { WorkspaceScopeApi } from "@/lib/api"
import { cn } from "@/lib/utils"
import { useAsyncData } from "@/hooks/use-async-data"
import type { DataSource } from "@/types/acp"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { StatusDot } from "@/components/status-dot"
import { ChevronRightIcon, DatabaseIcon, TableIcon } from "lucide-react"

/**
 * 选一个数据库引用交给 AI，与 @ 文件引用是同一个动作。
 *
 * 两级：数据源（它那个库的表清单）与表（列 / 索引 / 建表语句）。中间没有
 * 「选库」这一层——一条连接只对应一个库。两级都能直接引用，不必非得钻到
 * 表：「看看这个库有什么」和「按这张表写 SQL」是两种真实需求。
 *
 * 能选的范围与 AI 能操作的范围是同一个（都走会话的项目过滤）。
 */
export function DbRefPicker({
  open,
  sessionId,
  scope,
  onOpenChange,
  onSelect,
}: {
  open: boolean
  /** 0 表示草稿态：会话还没建，取不到项目，只提示。 */
  sessionId: number
  /** 作用域 API：普通会话与编排主会话形状一致，只差路径前缀。 */
  scope: WorkspaceScopeApi
  onOpenChange: (open: boolean) => void
  /** 选中的引用串：`<项目>/<环境>[/<库>[/<表>]]`。 */
  onSelect: (ref: string) => void
}) {
  const { t } = useTranslation()
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[80vh] gap-3 overflow-hidden sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("db.refTitle")}</DialogTitle>
          <DialogDescription>{t("db.refHint")}</DialogDescription>
        </DialogHeader>
        {open ? (
          <Picker
            sessionId={sessionId}
            scope={scope}
            onPick={(ref) => {
              onSelect(ref)
              onOpenChange(false)
            }}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

function Picker({
  sessionId,
  scope,
  onPick,
}: {
  sessionId: number
  scope: WorkspaceScopeApi
  onPick: (ref: string) => void
}) {
  const { t } = useTranslation()
  const { data: sources, error } = useAsyncData(
    () => (sessionId ? scope.datasources(sessionId) : Promise.resolve([])),
    [sessionId]
  )
  // 钻取路径只有一步：选中数据源 → 看它那个库的表。
  const [source, setSource] = useState<DataSource | null>(null)

  if (!sessionId) {
    return <Empty text={t("db.refNeedsSession")} />
  }
  if (error) {
    return <p className="p-4 text-sm text-destructive">{error}</p>
  }
  if (!sources) {
    return <Skeleton className="h-40 w-full" />
  }
  if (sources.length === 0) {
    return <Empty text={t("db.noSourceForProject")} />
  }

  return (
    <div className="flex min-h-0 flex-col gap-2">
      <Breadcrumb source={source} onReset={() => setSource(null)} />
      <ScrollArea className="min-h-0 flex-1">
        {!source ? (
          <SourceLevel sources={sources} onOpen={setSource} onPick={onPick} />
        ) : (
          <TableLevel
            sessionId={sessionId}
            scope={scope}
            source={source}
            database={source.database}
            onPick={onPick}
          />
        )}
      </ScrollArea>
    </div>
  )
}

function Breadcrumb({
  source,
  onReset,
}: {
  source: DataSource | null
  onReset: () => void
}) {
  const { t } = useTranslation()
  if (!source) return null
  return (
    <div className="flex items-center gap-1 text-xs text-muted-foreground">
      <button
        type="button"
        className="rounded px-1 py-0.5 hover:text-foreground"
        onClick={onReset}
      >
        {t("db.title")}
      </button>
      <ChevronRightIcon className="size-3 shrink-0" />
      <span className="px-1 font-mono">
        {source.ref} / {source.database}
      </span>
    </div>
  )
}

/** 一行：左边点进下一层，右边「引用这一层」。 */
function Row({
  icon,
  name,
  meta,
  refValue,
  onOpen,
  onPick,
}: {
  icon: React.ReactNode
  name: React.ReactNode
  meta?: React.ReactNode
  refValue: string
  onOpen?: () => void
  onPick: (ref: string) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="group flex items-center gap-2 rounded-md px-1.5 py-1 text-sm hover:bg-muted">
      <button
        type="button"
        disabled={!onOpen}
        onClick={onOpen}
        className={cn(
          "flex min-w-0 flex-1 items-center gap-2 text-left",
          onOpen ? "cursor-pointer" : "cursor-default"
        )}
      >
        {icon}
        <span className="min-w-0 truncate font-mono">{name}</span>
        {meta ? (
          <span className="ml-auto shrink-0 pr-1 text-xs text-muted-foreground tabular-nums">
            {meta}
          </span>
        ) : null}
      </button>
      <Button
        size="sm"
        variant="ghost"
        className="shrink-0 opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
        onClick={() => onPick(refValue)}
      >
        {t("db.refPick")}
      </Button>
    </div>
  )
}

function SourceLevel({
  sources,
  onOpen,
  onPick,
}: {
  sources: DataSource[]
  onOpen: (source: DataSource) => void
  onPick: (ref: string) => void
}) {
  return (
    <div className="flex flex-col">
      {sources.map((s) => (
        <Row
          key={s.id}
          icon={<StatusDot tone={s.disabled ? "muted" : "success"} />}
          name={s.ref}
          meta={s.database}
          refValue={s.ref}
          onOpen={() => onOpen(s)}
          onPick={onPick}
        />
      ))}
    </div>
  )
}

function TableLevel({
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
    [sessionId, source.id, database]
  )

  if (error) return <p className="p-4 text-sm text-destructive">{error}</p>
  if (!data) return <Skeleton className="h-32 w-full" />
  if (data.length === 0) {
    return <Empty text={t("db.tableCount", { count: 0 })} />
  }

  return (
    <div className="flex flex-col">
      {data.map((tb) => (
        <Row
          key={tb.name}
          icon={
            <TableIcon className="size-3.5 shrink-0 text-muted-foreground" />
          }
          name={tb.name}
          meta={tb.comment || String(tb.rows)}
          refValue={`${source.ref}/${database}/${tb.name}`}
          onPick={onPick}
        />
      ))}
    </div>
  )
}

function Empty({ text }: { text: string }) {
  return (
    <div className="flex flex-col items-center gap-2 p-6 text-center text-sm text-muted-foreground">
      <DatabaseIcon className="size-5" />
      {text}
    </div>
  )
}
