import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { ListPageStates } from "@/components/list-page-states"
import { AgentIcon } from "@/components/agent-icon"
import { RoleDialog } from "@/components/roles/role-dialog"
import { useAsyncData } from "@/hooks/use-async-data"
import { usePagedData } from "@/hooks/use-paged-data"
import { DataTable } from "@/components/data-table/data-table"
import { DataTableHeader } from "@/components/data-table/data-table-header"
import type { dataTableFeatures } from "@/components/data-table/data-table-features"
import { api } from "@/lib/api"
import type { Role } from "@/types/acp"
import type { ColumnDef } from "@tanstack/react-table"
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
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { DramaIcon, PlusIcon, Trash2Icon } from "lucide-react"

type RoleColumn = ColumnDef<typeof dataTableFeatures, Role, unknown>

/**
 * 角色页（adr-006）：编排里可雇佣的子代理定义。列表 + Dialog 编辑，
 * 角色是轻量配置对象，不配详情路由。
 */
export function Roles() {
  const { t } = useTranslation()
  const {
    items: roles,
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
  } = usePagedData((params) => api.roles.list(params))
  const { data: agents } = useAsyncData(
    () => api.agents.list().then((res) => res.items),
    []
  )
  const [editing, setEditing] = useState<Role | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [deleting, setDeleting] = useState<Role | null>(null)

  function agentName(id: number) {
    return agents?.find((a) => a.id === id)?.name ?? `#${id}`
  }

  function agentFlavor(id: number) {
    return agents?.find((a) => a.id === id)?.flavor
  }

  function openEdit(role: Role | null) {
    setEditing(role)
    setDialogOpen(true)
  }

  function onSaved(saved: Role) {
    replace(saved)
  }

  async function remove() {
    if (!deleting) return
    try {
      await api.roles.remove(deleting.id)
      dropRow(deleting.id)
      toast.success(t("roles.deleted"))
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setDeleting(null)
    }
  }

  // 列 id 就是数据库列名，原样进 `?sort=`（后端白名单把关）。
  const columns: RoleColumn[] = [
    {
      id: "name",
      accessorFn: (role: Role) => role.name,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("roles.name")} />
      ),
      meta: { label: t("roles.name") },
      cell: ({ row }) => (
        <span className="font-medium">
          {/* 整行可点开编辑（拉伸按钮模式），删除在其上单独可点。 */}
          <button
            type="button"
            className="after:absolute after:inset-0"
            onClick={() => openEdit(row.original)}
          >
            {row.original.name}
          </button>
          {row.original.builtin ? (
            <Badge variant="secondary" className="ml-2">
              {t("roles.builtin")}
            </Badge>
          ) : null}
        </span>
      ),
    },
    {
      id: "description",
      // 按描述排序没有意义，表头就给纯文字，不摆一个点了没反应的按钮。
      enableSorting: false,
      header: t("roles.colDescription"),
      meta: {
        label: t("roles.colDescription"),
        className: "max-w-96 truncate text-muted-foreground",
      },
      cell: ({ row }) => row.original.description || t("common.none"),
    },
    {
      id: "agent_id",
      accessorFn: (role: Role) => role.agentId,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("roles.agent")} />
      ),
      meta: { label: t("roles.agent") },
      cell: ({ row }) => (
        <span className="inline-flex items-center gap-1.5">
          <AgentIcon
            flavor={agentFlavor(row.original.agentId)}
            className="size-4"
          />
          {agentName(row.original.agentId)}
        </span>
      ),
    },
    {
      id: "model",
      accessorFn: (role: Role) => role.model,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("roles.model")} />
      ),
      meta: { label: t("roles.model"), className: "text-muted-foreground" },
      cell: ({ row }) => row.original.model || t("roles.default"),
    },
    {
      id: "level",
      accessorFn: (role: Role) => role.level,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("roles.level")} />
      ),
      meta: { label: t("roles.level"), className: "text-muted-foreground" },
      cell: ({ row }) =>
        row.original.level
          ? t(`chat.settings.level.${row.original.level}` as never, {
              defaultValue: row.original.level,
            })
          : t("roles.default"),
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
          className="relative text-muted-foreground opacity-0 group-hover:opacity-100 hover:text-destructive focus-visible:opacity-100"
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
            <CardTitle>{t("roles.title")}</CardTitle>
            <CardDescription>{t("roles.description")}</CardDescription>
            <CardAction>
              <Button size="sm" onClick={() => openEdit(null)}>
                <PlusIcon data-icon="inline-start" />
                {t("roles.add")}
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent>
            <DataTable
              columns={columns}
              data={error ? null : roles}
              total={total}
              page={page}
              pageSize={pageSize}
              sorting={sorting}
              onPage={setPage}
              onPageSize={setPageSize}
              onSorting={setSorting}
              empty={
                <ListPageStates
                  icon={<DramaIcon />}
                  error={error}
                  loading={roles === null}
                  emptyTitle={t("roles.empty")}
                  emptyHint={t("roles.emptyHint")}
                  emptyAction={
                    <Button size="sm" onClick={() => openEdit(null)}>
                      <PlusIcon data-icon="inline-start" />
                      {t("roles.add")}
                    </Button>
                  }
                />
              }
            />
          </CardContent>
        </Card>
      </div>

      <RoleDialog
        open={dialogOpen}
        role={editing}
        agents={agents ?? []}
        onClose={() => setDialogOpen(false)}
        onSaved={onSaved}
      />

      <AlertDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("roles.deleteTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("roles.deleteBody", { name: deleting?.name ?? "" })}
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
