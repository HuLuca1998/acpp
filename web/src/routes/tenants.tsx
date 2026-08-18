import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { ListPageStates } from "@/components/list-page-states"
import { StatusDot } from "@/components/status-dot"
import { usePagedData } from "@/hooks/use-paged-data"
import { DataTable } from "@/components/data-table/data-table"
import { DataTableHeader } from "@/components/data-table/data-table-header"
import type { dataTableFeatures } from "@/components/data-table/data-table-features"
import type { ColumnDef } from "@tanstack/react-table"
import { api } from "@/lib/api"
import { formatRelativeTime } from "@/lib/format"
import type { Tenant } from "@/types/acp"

type TenantColumn = ColumnDef<typeof dataTableFeatures, Tenant, unknown>
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
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  CopyIcon,
  TriangleAlertIcon,
  MoreHorizontalIcon,
  PlusIcon,
  RefreshCwIcon,
  Trash2Icon,
  UsersIcon,
} from "lucide-react"

/**
 * 连接页 = 多租户管理（adr-007）。owner 在这里建人、发分享链接、随时
 * 关停。租户凭链接换到的 cookie 长期有效，收回访问靠停用而不是等过期。
 */
export function Tenants() {
  const { t, i18n } = useTranslation()
  const {
    items: tenants,
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
  } = usePagedData((params) => api.tenants.list(params))

  const [creating, setCreating] = useState(false)
  const [newName, setNewName] = useState("")
  const [createError, setCreateError] = useState<string | null>(null)
  const [deleting, setDeleting] = useState<Tenant | null>(null)
  const [rotating, setRotating] = useState<Tenant | null>(null)

  function upsert(saved: Tenant) {
    replace(saved)
  }

  async function copyInvite(tenant: Tenant) {
    try {
      await navigator.clipboard.writeText(tenant.inviteUrl)
      toast.success(
        tenant.shareable
          ? t("tenants.linkCopied")
          : t("tenants.linkCopiedLocal")
      )
    } catch {
      // 剪贴板在非安全上下文里不可用（局域网 http 访问就是这种情况），
      // 退而把链接摆出来让人手动复制。
      toast.info(tenant.inviteUrl)
    }
  }

  async function create() {
    const name = newName.trim()
    if (!name) return
    try {
      const created = await api.tenants.create(name)
      upsert(created)
      setCreating(false)
      setNewName("")
      setCreateError(null)
      await copyInvite(created)
    } catch (err) {
      setCreateError((err as Error).message)
    }
  }

  async function toggle(tenant: Tenant) {
    try {
      const updated = await api.tenants.update(tenant.id, {
        disabled: !tenant.disabled,
      })
      // update 返回的是裸租户（没有 inviteUrl），保留原链接不覆盖。
      upsert({ ...tenant, ...updated, inviteUrl: tenant.inviteUrl })
      toast.success(
        tenant.disabled ? t("tenants.enabled") : t("tenants.disabled")
      )
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  async function rotate() {
    if (!rotating) return
    try {
      const updated = await api.tenants.rotate(rotating.id)
      upsert(updated)
      await copyInvite(updated)
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setRotating(null)
    }
  }

  async function remove() {
    if (!deleting) return
    try {
      await api.tenants.remove(deleting.id)
      dropRow(deleting.id)
      toast.success(t("tenants.removed"))
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setDeleting(null)
    }
  }

  // 列 id 就是数据库列名，原样进 `?sort=`（后端白名单把关）。
  const columns: TenantColumn[] = [
    {
      id: "name",
      accessorFn: (tenant: Tenant) => tenant.name,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("tenants.name")} />
      ),
      meta: { label: t("tenants.name"), className: "font-medium" },
      cell: ({ row }) => (
        <span className="inline-flex items-center gap-2">
          <StatusDot tone={row.original.disabled ? "muted" : "success"} />
          {row.original.name}
        </span>
      ),
    },
    {
      id: "root",
      accessorFn: (tenant: Tenant) => tenant.root,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("tenants.root")} />
      ),
      meta: {
        label: t("tenants.root"),
        className: "max-w-80 truncate font-mono text-xs text-muted-foreground",
      },
      cell: ({ row }) => row.original.root,
    },
    {
      id: "sessions",
      // 会话数是聚合出来的，不是 tenants 表上的列，没法进 ORDER BY。
      enableSorting: false,
      header: t("tenants.sessions"),
      meta: {
        label: t("tenants.sessions"),
        className: "text-right tabular-nums",
      },
      cell: ({ row }) => row.original.sessionCount,
    },
    {
      id: "last_seen_at",
      accessorFn: (tenant: Tenant) => tenant.lastSeenAt,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("tenants.lastSeen")} />
      ),
      meta: {
        label: t("tenants.lastSeen"),
        className: "text-muted-foreground tabular-nums",
      },
      cell: ({ row }) =>
        row.original.lastSeenAt
          ? formatRelativeTime(row.original.lastSeenAt, i18n.language)
          : t("tenants.never"),
    },
    {
      id: "actions",
      enableSorting: false,
      enableHiding: false,
      meta: { className: "w-10 text-right" },
      cell: ({ row }) => (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                size="icon-sm"
                variant="ghost"
                className="text-muted-foreground"
                aria-label={t("tenants.actions")}
              />
            }
          >
            <MoreHorizontalIcon />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-52">
            <DropdownMenuItem onClick={() => void copyInvite(row.original)}>
              <CopyIcon />
              <span>{t("tenants.copyLink")}</span>
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => setRotating(row.original)}>
              <RefreshCwIcon />
              <span>{t("tenants.rotate")}</span>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => void toggle(row.original)}>
              <StatusDot tone={row.original.disabled ? "success" : "muted"} />
              <span>
                {row.original.disabled
                  ? t("tenants.enable")
                  : t("tenants.disable")}
              </span>
            </DropdownMenuItem>
            <DropdownMenuItem
              variant="destructive"
              onClick={() => setDeleting(row.original)}
            >
              <Trash2Icon />
              <span>{t("common.delete")}</span>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      ),
    },
  ]

  return (
    <div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6">
      <div className="px-4 lg:px-6">
        <Card>
          <CardHeader>
            <CardTitle>{t("tenants.title")}</CardTitle>
            <CardDescription>{t("tenants.description")}</CardDescription>
            <CardAction>
              <Button size="sm" onClick={() => setCreating(true)}>
                <PlusIcon data-icon="inline-start" />
                {t("tenants.add")}
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            {/* 只监听本机时，任何链接发出去都打不开——与其让人试半天，
                不如直接说清楚现在的状态和开启方式。 */}
            {tenants && tenants.length > 0 && !tenants[0].shareable ? (
              <Alert>
                <TriangleAlertIcon />
                <AlertTitle>{t("tenants.localOnlyTitle")}</AlertTitle>
                <AlertDescription>
                  {t("tenants.localOnlyHint")}
                </AlertDescription>
              </Alert>
            ) : null}
            <DataTable
              columns={columns}
              data={error ? null : tenants}
              total={total}
              page={page}
              pageSize={pageSize}
              sorting={sorting}
              onPage={setPage}
              onPageSize={setPageSize}
              onSorting={setSorting}
              empty={
                <ListPageStates
                  icon={<UsersIcon />}
                  error={error}
                  loading={tenants === null}
                  emptyTitle={t("tenants.empty")}
                  emptyHint={t("tenants.emptyHint")}
                  emptyAction={
                    <Button size="sm" onClick={() => setCreating(true)}>
                      <PlusIcon data-icon="inline-start" />
                      {t("tenants.add")}
                    </Button>
                  }
                />
              }
            />
          </CardContent>
        </Card>
      </div>

      <Dialog open={creating} onOpenChange={setCreating}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("tenants.add")}</DialogTitle>
            <DialogDescription>{t("tenants.addDesc")}</DialogDescription>
          </DialogHeader>
          <Field>
            <FieldLabel htmlFor="tenant-name">{t("tenants.name")}</FieldLabel>
            <Input
              id="tenant-name"
              value={newName}
              autoFocus
              placeholder={t("tenants.namePlaceholder")}
              onChange={(e) => setNewName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") void create()
              }}
            />
            <FieldDescription>
              {createError ?? t("tenants.nameHint")}
            </FieldDescription>
          </Field>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreating(false)}>
              {t("common.cancel")}
            </Button>
            <Button onClick={() => void create()} disabled={!newName.trim()}>
              {t("tenants.addAndCopy")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={rotating !== null}
        onOpenChange={(open) => !open && setRotating(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("tenants.rotate")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("tenants.rotateConfirm", { name: rotating?.name ?? "" })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction onClick={() => void rotate()}>
              {t("tenants.rotate")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("tenants.removeTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("tenants.removeConfirm", { name: deleting?.name ?? "" })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => void remove()}
            >
              {t("common.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
