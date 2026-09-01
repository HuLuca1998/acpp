import { useState } from "react"
import { useTranslation } from "react-i18next"
import type { TFunction } from "i18next"
import { toast } from "sonner"
import type { ColumnDef } from "@tanstack/react-table"
import { HardDriveIcon, PencilIcon, PlusIcon, Trash2Icon } from "lucide-react"

import { api } from "@/lib/api"
import type { Server } from "@/types/acp"
import { Hint } from "@/components/hint"
import { ListPageStates } from "@/components/list-page-states"
import { authLabelKey } from "@/components/servers/auth-label"
import { ServerDialog } from "@/components/servers/server-dialog"
import { StatusDot } from "@/components/status-dot"
import { usePagedData } from "@/hooks/use-paged-data"
import { DataTable } from "@/components/data-table/data-table"
import { DataTableHeader } from "@/components/data-table/data-table-header"
import type { dataTableFeatures } from "@/components/data-table/data-table-features"
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

type ServerColumn = ColumnDef<typeof dataTableFeatures, Server, unknown>

/**
 * 服务器页：管理 SSH 连接（adr-019）。
 *
 * 一行一台机器。这些记录同时供两处使用——AI 的只读观察工具面，与数据源
 * 的拨号跳板，所以这里改一次，两边都跟着变。
 */
export function Servers() {
  const { t } = useTranslation()
  const {
    items: servers,
    total,
    error,
    page,
    pageSize,
    sorting,
    setPage,
    setPageSize,
    setSorting,
    replace,
    remove: dropRow,
  } = usePagedData((params) => api.servers.list(params))

  const [editing, setEditing] = useState<Server | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [deleting, setDeleting] = useState<Server | null>(null)

  function openEdit(server: Server | null) {
    setEditing(server)
    setDialogOpen(true)
  }

  async function confirmDelete() {
    if (!deleting) return
    try {
      await api.servers.remove(deleting.id)
      dropRow(deleting.id)
      toast.success(t("server.deleted"))
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setDeleting(null)
    }
  }

  return (
    <div className="flex flex-col gap-4 p-4 lg:p-6">
      <Card>
        <CardHeader>
          <CardTitle>{t("server.title")}</CardTitle>
          <CardDescription>{t("server.description")}</CardDescription>
          <CardAction>
            <Button size="sm" onClick={() => openEdit(null)}>
              <PlusIcon data-icon="inline-start" />
              {t("server.add")}
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent>
          <DataTable
            columns={serverColumns(t, openEdit, setDeleting)}
            data={error ? null : servers}
            total={total}
            page={page}
            pageSize={pageSize}
            sorting={sorting}
            onPage={setPage}
            onPageSize={setPageSize}
            onSorting={setSorting}
            onRowClick={openEdit}
            empty={
              <ListPageStates
                icon={<HardDriveIcon />}
                error={error}
                loading={servers === null}
                emptyTitle={t("server.empty")}
                emptyHint={t("server.emptyHint")}
                emptyAction={
                  <Button size="sm" onClick={() => openEdit(null)}>
                    <PlusIcon data-icon="inline-start" />
                    {t("server.add")}
                  </Button>
                }
              />
            }
          />
        </CardContent>
      </Card>

      <ServerDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        server={editing}
        onSaved={replace}
      />

      <AlertDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("server.deleteTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("server.deleteConfirm", { name: deleting?.name ?? "" })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={confirmDelete}>
              {t("common.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

function serverColumns(
  t: TFunction,
  onEdit: (server: Server) => void,
  onDelete: (server: Server) => void
): ServerColumn[] {
  return [
    {
      accessorKey: "name",
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("server.name")} />
      ),
      cell: ({ row }) => (
        <span className="font-mono font-medium">{row.original.name}</span>
      ),
    },
    {
      accessorKey: "host",
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("server.host")} />
      ),
      cell: ({ row }) => (
        <span className="font-mono text-muted-foreground">
          {row.original.user}@{row.original.host}:{row.original.port}
        </span>
      ),
    },
    {
      accessorKey: "auth",
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("server.auth")} />
      ),
      cell: ({ row }) => (
        <span className="text-muted-foreground">
          {t(authLabelKey(row.original.auth))}
        </span>
      ),
    },
    {
      accessorKey: "note",
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("server.note")} />
      ),
      cell: ({ row }) => (
        <span className="line-clamp-1 text-muted-foreground">
          {row.original.note}
        </span>
      ),
    },
    {
      id: "state",
      header: () => null,
      cell: ({ row }) => (
        <StatusDot
          tone={row.original.disabled ? "muted" : "success"}
          label={
            row.original.disabled ? t("server.disabled") : t("server.enabled")
          }
        />
      ),
    },
    {
      id: "actions",
      header: () => null,
      cell: ({ row }) => (
        // 行本身可点开编辑，这两个按钮不能把点击冒泡上去。
        <div
          className="flex justify-end gap-1"
          onClick={(e) => e.stopPropagation()}
        >
          <Hint label={t("server.edit")}>
            <Button
              variant="ghost"
              size="icon"
              aria-label={t("server.edit")}
              onClick={() => onEdit(row.original)}
            >
              <PencilIcon />
            </Button>
          </Hint>
          <Hint label={t("common.delete")} align="end">
            <Button
              variant="ghost"
              size="icon"
              aria-label={t("common.delete")}
              onClick={() => onDelete(row.original)}
            >
              <Trash2Icon />
            </Button>
          </Hint>
        </div>
      ),
    },
  ]
}
