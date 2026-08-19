import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"
import { toast } from "sonner"

import { ListPageStates } from "@/components/list-page-states"
import { StatusDot } from "@/components/status-dot"
import { usePagedData } from "@/hooks/use-paged-data"
import { DataTable } from "@/components/data-table/data-table"
import { DataTableHeader } from "@/components/data-table/data-table-header"
import type { dataTableFeatures } from "@/components/data-table/data-table-features"
import type { ColumnDef } from "@tanstack/react-table"
import { api } from "@/lib/api"
import { formatDateTime, formatRelativeTime } from "@/lib/format"
import { SESSION_STATE_TONE } from "@/lib/status-tone"
import type { OrchSession } from "@/types/acp"

type OrchColumn = ColumnDef<typeof dataTableFeatures, OrchSession, unknown>
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
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { NetworkIcon, PlusIcon, Trash2Icon } from "lucide-react"

/** 编排会话列表（adr-006）。新建走 draft-first：直接进空白编排页。 */
export function Orchestrator() {
  const { t, i18n } = useTranslation()
  const {
    items: sessions,
    total,
    error,
    page,
    pageSize,
    sorting,
    setPage,
    setPageSize,
    setSorting,
    remove: dropRow,
  } = usePagedData((params) => api.orchestrator.list(params))
  const [deleting, setDeleting] = useState<OrchSession | null>(null)

  async function remove() {
    if (!deleting) return
    try {
      await api.orchestrator.remove(deleting.id)
      dropRow(deleting.id)
      toast.success(t("orch.deleted"))
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setDeleting(null)
    }
  }

  // 列 id 就是数据库列名，原样进 `?sort=`（后端白名单把关）。
  const columns: OrchColumn[] = [
    {
      id: "title",
      accessorFn: (session: OrchSession) => session.title,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("orch.colTitle")} />
      ),
      meta: { label: t("orch.colTitle"), className: "max-w-72 font-medium" },
      cell: ({ row }) => (
        // flex 而不是 inline-flex：inline 级的盒子按内容撑宽，父单元格的
        // max-w 管不住它，长标题会溢出去盖住右边的列。
        <span className="flex min-w-0 items-center gap-2">
          <StatusDot
            tone={SESSION_STATE_TONE[row.original.state]}
            pulse={row.original.state === "active"}
          />
          <Link
            to={`/orchestrator/${row.original.id}`}
            className="truncate after:absolute after:inset-0"
          >
            {row.original.title || `${t("common.unnamed")} #${row.original.id}`}
          </Link>
        </span>
      ),
    },
    {
      id: "cwd",
      enableSorting: false,
      header: t("orch.colCwd"),
      meta: {
        label: t("orch.colCwd"),
        className: "max-w-72 truncate font-mono text-xs text-muted-foreground",
      },
      cell: ({ row }) => row.original.cwd || t("common.none"),
    },
    {
      id: "tokens_used",
      accessorFn: (session: OrchSession) => session.tokensUsed,
      header: ({ column }) => (
        <DataTableHeader
          column={column}
          title={t("orch.colTokens")}
          className="ml-auto"
        />
      ),
      meta: {
        label: t("orch.colTokens"),
        className: "text-right text-muted-foreground tabular-nums",
      },
      cell: ({ row }) =>
        row.original.tokensUsed > 0
          ? row.original.tokensUsed.toLocaleString()
          : t("common.none"),
    },
    {
      id: "updated_at",
      accessorFn: (session: OrchSession) => session.updatedAt,
      header: ({ column }) => (
        <DataTableHeader
          column={column}
          title={t("orch.colUpdated")}
          className="ml-auto"
        />
      ),
      meta: {
        label: t("orch.colUpdated"),
        className: "text-right text-muted-foreground tabular-nums",
      },
      cell: ({ row }) => (
        <span title={formatDateTime(row.original.updatedAt, i18n.language)}>
          {formatRelativeTime(row.original.updatedAt, i18n.language)}
        </span>
      ),
    },
    {
      id: "actions",
      enableSorting: false,
      enableHiding: false,
      meta: { className: "w-10 text-right" },
      cell: ({ row }) => (
        <Button
          size="icon-sm"
          variant="ghost"
          className="relative text-muted-foreground transition-colors hover:text-destructive"
          aria-label={t("common.delete")}
          onClick={() => setDeleting(row.original)}
        >
          <Trash2Icon />
        </Button>
      ),
    },
  ]

  return (
    <div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6">
      <div className="px-4 lg:px-6">
        <Card>
          <CardHeader>
            <CardTitle>{t("orch.title")}</CardTitle>
            <CardDescription>{t("orch.description")}</CardDescription>
            <CardAction>
              <Button size="sm" render={<Link to="/orchestrator/new" />}>
                <PlusIcon data-icon="inline-start" />
                {t("orch.add")}
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent>
            <DataTable
              columns={columns}
              data={error ? null : sessions}
              total={total}
              page={page}
              pageSize={pageSize}
              sorting={sorting}
              onPage={setPage}
              onPageSize={setPageSize}
              onSorting={setSorting}
              empty={
                <ListPageStates
                  icon={<NetworkIcon />}
                  error={error}
                  loading={sessions === null}
                  emptyTitle={t("orch.empty")}
                  emptyHint={t("orch.emptyListHint")}
                  emptyAction={
                    <Button size="sm" render={<Link to="/orchestrator/new" />}>
                      <PlusIcon data-icon="inline-start" />
                      {t("orch.add")}
                    </Button>
                  }
                />
              }
            />
          </CardContent>
        </Card>
      </div>

      <AlertDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("orch.deleteTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("orch.deleteBody", {
                name: deleting?.title || `#${deleting?.id ?? ""}`,
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={remove}>
              {t("common.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
