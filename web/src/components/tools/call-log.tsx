import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { CallStats } from "@/components/tools/call-stats"
import { DataPagination } from "@/components/data-pagination"
import { ListPageStates } from "@/components/list-page-states"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { useAsyncData } from "@/hooks/use-async-data"
import { api } from "@/lib/api"
import { formatRelativeTime, formatDateTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { McpCall, McpToolStat } from "@/types/acp"
import {
  AlertTriangleIcon,
  BotIcon,
  ChevronRightIcon,
  HistoryIcon,
  Trash2Icon,
  UserRoundIcon,
} from "lucide-react"

/**
 * 调用记录：AI 到底调了哪些工具、传了什么、拿回什么。
 *
 * 上面一张按工具聚合的统计表，下面是逐条记录。两者回答的是不同的问题：
 * 统计回答「这个工具有没有被用起来」，记录回答「刚才那次到底发生了什么」。
 *
 * 记录里 args/result 是**截断过**的文本（后端落库时截的）——这里是排查
 * 线索，不是完整留档。
 */
export function CallLog({
  stats,
  onChanged,
}: {
  stats: McpToolStat[]
  onChanged: () => void
}) {
  const { t } = useTranslation()
  const [source, setSource] = useState("")
  const [errorsOnly, setErrorsOnly] = useState(false)
  const [clearing, setClearing] = useState(false)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  // 清空之后要重拉，但筛选条件没变——加一个版本号当作「重来一次」的信号。
  const [version, setVersion] = useState(0)

  // 不用 usePagedData：那个 hook 只按 page/pageSize/排序重拉，
  // 这里的筛选条件（来源、只看失败）变了同样得重拉。
  const { data, error } = useAsyncData(
    () =>
      api.tools.calls({
        page,
        pageSize,
        source: source || undefined,
        errorsOnly: errorsOnly ? "1" : undefined,
      }),
    [page, pageSize, source, errorsOnly, version]
  )
  const items = data?.items ?? []
  const total = data?.total ?? 0

  /** 换筛选条件回第一页：换了条件还停在第 3 页，那已经是另一批数据了。 */
  function refilter(next: () => void) {
    next()
    setPage(1)
  }

  async function clear() {
    try {
      await api.tools.clearCalls()
      setClearing(false)
      setPage(1)
      setVersion((v) => v + 1)
      onChanged()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  const sources = [
    { value: "", label: t("tools.calls.all") },
    { value: "agent", label: t("tools.calls.fromAgent") },
    { value: "manual", label: t("tools.calls.fromManual") },
  ]

  return (
    <div className="flex flex-col gap-4">
      {stats.length > 0 ? <CallStats stats={stats} /> : null}

      <div className="flex flex-wrap items-center gap-2">
        {sources.map((option) => (
          <Button
            key={option.value}
            variant={source === option.value ? "secondary" : "outline"}
            size="sm"
            onClick={() => refilter(() => setSource(option.value))}
          >
            {option.label}
          </Button>
        ))}
        <Button
          variant={errorsOnly ? "secondary" : "outline"}
          size="sm"
          onClick={() => refilter(() => setErrorsOnly((v) => !v))}
        >
          <AlertTriangleIcon data-icon="inline-start" />
          {t("tools.calls.errorsOnly")}
        </Button>
        <div className="flex-1" />
        <Button
          variant="ghost"
          size="sm"
          onClick={() => setClearing(true)}
          disabled={total === 0}
        >
          <Trash2Icon data-icon="inline-start" />
          {t("tools.calls.clear")}
        </Button>
      </div>

      {items.length === 0 ? (
        <ListPageStates
          icon={<HistoryIcon />}
          error={error}
          loading={data === null}
          emptyTitle={t("tools.calls.emptyTitle")}
          emptyHint={t("tools.calls.emptyHint")}
        />
      ) : (
        <div className="flex flex-col divide-y rounded-lg border">
          {items.map((call) => (
            <CallRow key={call.id} call={call} />
          ))}
        </div>
      )}

      <DataPagination
        total={total}
        page={page}
        pageSize={pageSize}
        onPage={setPage}
        onPageSize={setPageSize}
      />

      <AlertDialog open={clearing} onOpenChange={setClearing}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("tools.calls.clearTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("tools.calls.clearDesc")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={clear}>
              {t("tools.calls.clear")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

/** 一条调用：点开看参数与返回。 */
function CallRow({ call }: { call: McpCall }) {
  const { t, i18n } = useTranslation()
  const [open, setOpen] = useState(false)

  return (
    <div className="flex flex-col">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-2 px-3 py-2 text-left transition-colors hover:bg-accent/50"
      >
        <ChevronRightIcon
          className={cn(
            "size-3.5 shrink-0 text-muted-foreground transition-transform",
            open && "rotate-90"
          )}
        />
        {call.source === "agent" ? (
          <BotIcon className="size-3.5 shrink-0 text-muted-foreground" />
        ) : (
          <UserRoundIcon className="size-3.5 shrink-0 text-muted-foreground" />
        )}
        <span className="shrink-0 font-mono text-xs">{call.tool}</span>
        {call.isError ? (
          <Badge
            variant="outline"
            className="shrink-0 border-warning/40 text-[11px] text-warning"
          >
            {t("tools.calls.failed")}
          </Badge>
        ) : null}
        <span className="min-w-0 flex-1 truncate font-mono text-xs text-muted-foreground">
          {call.args}
        </span>
        <span className="shrink-0 text-xs text-muted-foreground tabular-nums">
          {call.durationMs}ms
        </span>
        <span
          className="shrink-0 text-xs text-muted-foreground tabular-nums"
          title={formatDateTime(call.createdAt, i18n.language)}
        >
          {formatRelativeTime(call.createdAt, i18n.language)}
        </span>
      </button>

      {open ? (
        <div className="flex flex-col gap-2 border-t bg-muted/20 px-3 py-2">
          {call.sessionId > 0 ? (
            <p className="text-xs text-muted-foreground">
              {t("tools.calls.fromSession", { id: call.sessionId })}
            </p>
          ) : null}
          <Field label={t("tools.calls.args")} text={call.args} />
          <Separator />
          <Field label={t("tools.calls.result")} text={call.result} />
        </div>
      ) : null}
    </div>
  )
}

function Field({ label, text }: { label: string; text: string }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-xs font-medium text-muted-foreground">{label}</span>
      <pre className="max-h-60 overflow-auto rounded-md border bg-background p-2 font-mono text-xs leading-5 whitespace-pre-wrap">
        {text || "—"}
      </pre>
    </div>
  )
}
