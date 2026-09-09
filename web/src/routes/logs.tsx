import { useState } from "react"
import { useTranslation } from "react-i18next"
import type { ColumnDef } from "@tanstack/react-table"
import { toast } from "sonner"

import { ListPageHeader } from "@/components/list-page-header"
import { ListPageStates } from "@/components/list-page-states"
import { usePagedData } from "@/hooks/use-paged-data"
import { useSearchDraft } from "@/hooks/use-search-draft"
import { DataTable } from "@/components/data-table/data-table"
import {
  SearchBar,
  SearchSelect,
  SearchText,
} from "@/components/data-table/data-table-search"
import { DataTableHeader } from "@/components/data-table/data-table-header"
import type { dataTableFeatures } from "@/components/data-table/data-table-features"
import { LogDetail } from "@/components/logs/log-detail"
import { StatusDot } from "@/components/status-dot"
import { api } from "@/lib/api"
import { formatBytes, formatDateTime } from "@/lib/format"
import type { ApiLog } from "@/types/apilog"
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
import { Button } from "@/components/ui/button"
import { ScrollTextIcon, Trash2Icon } from "lucide-react"

type LogColumn = ColumnDef<typeof dataTableFeatures, ApiLog, unknown>

const METHODS = ["GET", "POST", "PUT", "DELETE", "PATCH"]

/**
 * 请求日志页：中间件记下的每条 /api 请求。
 * 与其他列表页同一副四区骨架；行点开看请求 / 回复的头与正文。
 */
export function Logs() {
  const { t, i18n } = useTranslation()
  const search = useSearchDraft({ q: "", method: "", status: "" })
  const { values } = search
  const {
    items: logs,
    total,
    error,
    fetching,
    page,
    pageSize,
    sorting,
    setPage,
    setPageSize,
    setSorting,
    reload,
    setData,
  } = usePagedData(
    (params) =>
      api.logs.list({
        ...params,
        q: values.q,
        method: values.method,
        status: values.status,
      }),
    { sort: [{ id: "id", desc: true }], deps: [values] }
  )
  const submitSearch = () => {
    search.commit()
    setPage(1)
  }
  const resetSearch = () => {
    search.reset()
    setPage(1)
  }
  const [opened, setOpened] = useState<number | null>(null)
  const [clearing, setClearing] = useState(false)

  async function clear() {
    try {
      await api.logs.clear()
      setClearing(false)
      setData({ items: [], total: 0, page: 1, pageSize })
      setPage(1)
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  // 列 id 就是数据库列名：原样进 ?sort=，由后端白名单校验。
  const columns: LogColumn[] = [
    {
      id: "id",
      accessorFn: (log: ApiLog) => log.id,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("logs.columnId")} />
      ),
      meta: {
        label: t("logs.columnId"),
        className: "w-16 tabular-nums text-muted-foreground",
        pin: "left",
      },
      cell: ({ row }) => row.original.id,
    },
    {
      id: "created_at",
      accessorFn: (log: ApiLog) => log.createdAt,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("logs.time")} />
      ),
      meta: {
        label: t("logs.time"),
        className: "text-muted-foreground tabular-nums whitespace-nowrap",
      },
      cell: ({ row }) => formatDateTime(row.original.createdAt, i18n.language),
    },
    {
      id: "method",
      accessorFn: (log: ApiLog) => log.method,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("logs.method")} />
      ),
      meta: { label: t("logs.method"), className: "w-20 font-mono text-xs" },
      cell: ({ row }) => row.original.method,
    },
    {
      id: "path",
      accessorFn: (log: ApiLog) => log.path,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("logs.path")} />
      ),
      meta: { label: t("logs.path"), className: "max-w-80" },
      cell: ({ row }) => (
        <span
          className="block truncate font-mono text-xs"
          title={
            row.original.query
              ? `${row.original.path}?${row.original.query}`
              : row.original.path
          }
        >
          {row.original.path}
          {row.original.query ? (
            <span className="text-muted-foreground">?{row.original.query}</span>
          ) : null}
        </span>
      ),
    },
    {
      id: "status",
      accessorFn: (log: ApiLog) => log.status,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("logs.status")} />
      ),
      meta: { label: t("logs.status"), className: "w-24 tabular-nums" },
      cell: ({ row }) => (
        <StatusDot
          tone={
            row.original.status >= 500
              ? "destructive"
              : row.original.status >= 400
                ? "warning"
                : "success"
          }
          label={String(row.original.status)}
        />
      ),
    },
    {
      id: "duration_ms",
      accessorFn: (log: ApiLog) => log.durationMs,
      header: ({ column }) => (
        <DataTableHeader
          column={column}
          title={t("logs.duration")}
          className="ml-auto"
        />
      ),
      meta: {
        label: t("logs.duration"),
        className: "w-24 text-right tabular-nums text-muted-foreground",
      },
      cell: ({ row }) => `${row.original.durationMs} ms`,
    },
    {
      id: "size",
      enableSorting: false,
      header: () => t("logs.size"),
      meta: {
        label: t("logs.size"),
        className:
          "w-28 text-right tabular-nums text-muted-foreground whitespace-nowrap",
      },
      cell: ({ row }) =>
        `${formatBytes(row.original.requestSize)} / ${formatBytes(row.original.responseSize)}`,
    },
    {
      id: "identity",
      accessorFn: (log: ApiLog) => log.identity,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("logs.identity")} />
      ),
      meta: { label: t("logs.identity"), className: "text-muted-foreground" },
      cell: ({ row }) => row.original.identity,
    },
    {
      id: "remote_addr",
      accessorFn: (log: ApiLog) => log.remoteAddr,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("logs.remote")} />
      ),
      meta: {
        label: t("logs.remote"),
        className: "font-mono text-xs text-muted-foreground",
      },
      cell: ({ row }) => row.original.remoteAddr,
    },
    {
      id: "origin",
      enableSorting: false,
      header: () => t("logs.origin"),
      meta: { label: t("logs.origin"), className: "max-w-56" },
      cell: ({ row }) =>
        row.original.origin ? (
          <span
            className="block truncate font-mono text-xs text-muted-foreground"
            title={row.original.origin}
          >
            {row.original.origin}
          </span>
        ) : (
          <span className="text-muted-foreground/50">{t("common.none")}</span>
        ),
    },
  ]

  return (
    <div className="flex flex-col gap-4 p-4 lg:p-6">
      <ListPageHeader
        title={t("logs.title")}
        total={logs ? total : undefined}
      />
      <DataTable
        columns={columns}
        data={error ? null : logs}
        total={total}
        page={page}
        pageSize={pageSize}
        sorting={sorting}
        search={
          <SearchBar onSearch={submitSearch} onReset={resetSearch}>
            <SearchText
              value={search.draft.q}
              onChange={(v) => search.set("q", v)}
              placeholder={t("logs.searchPath")}
            />
            <SearchSelect
              label={t("logs.method")}
              value={search.draft.method}
              onChange={(v) => search.set("method", v)}
              options={METHODS.map((m) => ({ value: m, label: m }))}
              width="sm"
            />
            <SearchSelect
              label={t("logs.status")}
              value={search.draft.status}
              onChange={(v) => search.set("status", v)}
              options={["2", "3", "4", "5"].map((n) => ({
                value: n,
                label: t("logs.statusClass", { n }),
              }))}
              width="sm"
            />
          </SearchBar>
        }
        fetching={fetching}
        actions={
          <Button
            variant="outline"
            size="sm"
            disabled={total === 0}
            onClick={() => setClearing(true)}
          >
            <Trash2Icon data-icon="inline-start" />
            {t("logs.clear")}
          </Button>
        }
        onReload={reload}
        onPage={setPage}
        onPageSize={setPageSize}
        onSorting={setSorting}
        onRowClick={(log) => setOpened(log.id)}
        empty={
          <ListPageStates
            icon={<ScrollTextIcon />}
            error={error}
            loading={logs === null}
            emptyTitle={t("logs.empty")}
            emptyHint={t("logs.emptyHint")}
          />
        }
      />

      <LogDetail id={opened} onClose={() => setOpened(null)} />

      <AlertDialog open={clearing} onOpenChange={setClearing}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("logs.clearTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("logs.clearConfirm")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={clear}>
              {t("logs.clear")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
