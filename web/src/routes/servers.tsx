import { useState } from "react"
import { useTranslation } from "react-i18next"
import type { TFunction } from "i18next"
import { toast } from "sonner"
import type { ColumnDef } from "@tanstack/react-table"
import {
  DatabaseIcon,
  HardDriveIcon,
  PencilIcon,
  PlusIcon,
  Trash2Icon,
} from "lucide-react"

import { api } from "@/lib/api"
import type { Server } from "@/types/acp"
import { Hint } from "@/components/hint"
import { ListPageHeader } from "@/components/list-page-header"
import { ListPageStates } from "@/components/list-page-states"
import { authLabelKey } from "@/components/servers/auth-label"
import { ServerDialog } from "@/components/servers/server-dialog"
import { StatusDot } from "@/components/status-dot"
import { usePagedData } from "@/hooks/use-paged-data"
import { DataTable } from "@/components/data-table/data-table"
import {
  SearchBar,
  SearchText,
} from "@/components/data-table/data-table-search"
import { useSearchDraft } from "@/hooks/use-search-draft"
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
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"

type ServerColumn = ColumnDef<typeof dataTableFeatures, Server, unknown>

/**
 * 服务器页：管理 SSH 连接（adr-019）。
 *
 * 一行一台机器。这些记录同时供两处使用——AI 的只读观察工具面，与数据源
 * 的拨号跳板，所以这里改一次，两边都跟着变。
 */
export function Servers() {
  const { t } = useTranslation()
  const search = useSearchDraft({ q: "" })
  const { values } = search
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
    fetching,
    reload,
    replace,
    remove: dropRow,
  } = usePagedData((params) => api.servers.list({ ...params, q: values.q }), {
    deps: [values],
  })
  // 提交或重置都回第一页：停在旧条件的第 5 页上已经是另一批数据了。
  const submitSearch = () => {
    search.commit()
    setPage(1)
  }
  const resetSearch = () => {
    search.reset()
    setPage(1)
  }

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
      <ListPageHeader
        title={t("server.title")}
        total={servers ? total : undefined}
      />
      <DataTable
        columns={serverColumns(t, openEdit, setDeleting)}
        data={error ? null : servers}
        total={total}
        page={page}
        pageSize={pageSize}
        sorting={sorting}
        search={
          <SearchBar onSearch={submitSearch} onReset={resetSearch}>
            <SearchText
              value={search.draft.q}
              onChange={(v) => search.set("q", v)}
              placeholder={t("server.searchPlaceholder")}
            />
          </SearchBar>
        }
        fetching={fetching}
        actions={
          <Button size="sm" onClick={() => openEdit(null)}>
            <PlusIcon data-icon="inline-start" />
            {t("server.add")}
          </Button>
        }
        onReload={reload}
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
      meta: { pin: "left" },
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
      // 限宽：备注常常是一整句话，不封顶会把右边的引用数、状态与操作按钮
      // 整个挤出可视区——那几样恰恰是这张表上要动手的东西。
      cell: ({ row }) => (
        <span
          title={row.original.note}
          className="line-clamp-1 max-w-[min(22rem,18vw)] text-muted-foreground"
        >
          {row.original.note}
        </span>
      ),
    },
    {
      id: "usedBy",
      header: () => null,
      // 被数据源当跳板机的条数。放在状态点前面：它决定了这台能不能删，
      // 而那件事只有在点了删除被拒绝时才会被发现。
      cell: ({ row }) =>
        row.original.usedBy > 0 ? (
          <Hint label={t("server.usedByHint", { count: row.original.usedBy })}>
            <Badge variant="outline" className="gap-1 text-[11px]">
              <DatabaseIcon className="size-3" />
              {row.original.usedBy}
            </Badge>
          </Hint>
        ) : null,
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
      meta: { pin: "right" },
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
          {/* 被引用时直接禁用而不是点了再报错——按钮能按却必然失败，
              是最没必要的一次往返。 */}
          <Hint
            label={
              row.original.usedBy > 0
                ? t("server.deleteInUse")
                : t("common.delete")
            }
            align="end"
          >
            <Button
              variant="ghost"
              size="icon"
              aria-label={t("common.delete")}
              disabled={row.original.usedBy > 0}
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
