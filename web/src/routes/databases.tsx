import { useState } from "react"
import { useTranslation } from "react-i18next"
import type { TFunction } from "i18next"
import { toast } from "sonner"

import { ListPageStates } from "@/components/list-page-states"
import { DataSourceDialog } from "@/components/db/datasource-dialog"
import { DataSourceExplorer } from "@/components/db/datasource-explorer"
import { StatusDot } from "@/components/status-dot"
import { useAsyncData } from "@/hooks/use-async-data"
import { usePagedData } from "@/hooks/use-paged-data"
import { DataTable } from "@/components/data-table/data-table"
import { DataTableHeader } from "@/components/data-table/data-table-header"
import type { dataTableFeatures } from "@/components/data-table/data-table-features"
import type { ColumnDef } from "@tanstack/react-table"
import { api } from "@/lib/api"
import type { DataSource } from "@/types/acp"

type SourceColumn = ColumnDef<typeof dataTableFeatures, DataSource, unknown>
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
import {
  DatabaseIcon,
  PencilIcon,
  PlusIcon,
  Trash2Icon,
  XIcon,
} from "lucide-react"

/**
 * 数据库页：管理 MySQL 连接（adr-008）。
 *
 * 一行一条连接（项目 / 环境 / 库 / 地址 / 读写），点行展开它的表浏览与
 * SQL 控制台。项目与环境是这条连接的身份，项目还决定了哪些会话看得到它；
 * 库是绑定的——一条连接只对应一个库。
 */
export function Databases() {
  const { t } = useTranslation()
  const {
    items: sources,
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
  } = usePagedData((params) => api.datasources.list(params))

  // 项目名建议来自工作区已有的仓库；拉不到不影响填写（自由输入）。
  const { data: projects } = useAsyncData(
    () =>
      api.projects
        .list()
        .then((res) => res.items.map((p) => p.name.split("/").pop() ?? p.name))
        .catch(() => []),
    []
  )

  const [editing, setEditing] = useState<DataSource | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [deleting, setDeleting] = useState<DataSource | null>(null)
  const [opened, setOpened] = useState<DataSource | null>(null)

  function openEdit(source: DataSource | null) {
    setEditing(source)
    setDialogOpen(true)
  }

  function handleSaved(saved: DataSource) {
    replace(saved)
    setEditing(saved)
    // 展开中的那条被改了：换成新记录，免得面板还按旧连接查。
    setOpened((prev) => (prev?.id === saved.id ? saved : prev))
  }

  async function confirmDelete() {
    if (!deleting) return
    try {
      await api.datasources.remove(deleting.id)
      dropRow(deleting.id)
      if (opened?.id === deleting.id) setOpened(null)
      toast.success(t("db.deleted"))
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setDeleting(null)
    }
  }

  const columns = sourceColumns(t, openEdit, setDeleting)

  return (
    <div className="flex flex-col gap-4 p-4 lg:p-6">
      <Card>
        <CardHeader>
          <CardTitle>{t("db.title")}</CardTitle>
          <CardDescription>{t("db.description")}</CardDescription>
          <CardAction>
            <Button size="sm" onClick={() => openEdit(null)}>
              <PlusIcon data-icon="inline-start" />
              {t("db.add")}
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent>
          <DataTable
            columns={columns}
            data={error ? null : sources}
            total={total}
            page={page}
            pageSize={pageSize}
            sorting={sorting}
            onPage={setPage}
            onPageSize={setPageSize}
            onSorting={setSorting}
            onRowClick={(source) =>
              setOpened((prev) => (prev?.id === source.id ? null : source))
            }
            rowClassName={(source) =>
              opened?.id === source.id ? "bg-accent/50" : undefined
            }
            empty={
              <ListPageStates
                icon={<DatabaseIcon />}
                error={error}
                loading={sources === null}
                emptyTitle={t("db.empty")}
                emptyHint={t("db.emptyHint")}
                emptyAction={
                  <Button size="sm" onClick={() => openEdit(null)}>
                    <PlusIcon data-icon="inline-start" />
                    {t("db.add")}
                  </Button>
                }
              />
            }
          />
        </CardContent>
      </Card>

      {/* 展开的那条连接：表浏览 + SQL 控制台。放表格下方而不是塞进行里
          ——它是一整块工作区，挤在表格行内没法用。 */}
      {opened ? (
        <Card>
          <CardHeader>
            <CardTitle className="font-mono text-base">
              {opened.ref} / {opened.database}
            </CardTitle>
            <CardDescription>{addressOf(opened)}</CardDescription>
            <CardAction>
              <Button
                variant="ghost"
                size="icon"
                aria-label={t("common.cancel")}
                onClick={() => setOpened(null)}
              >
                <XIcon />
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent>
            <DataSourceExplorer key={opened.id} source={opened} />
          </CardContent>
        </Card>
      ) : null}

      <DataSourceDialog
        open={dialogOpen}
        source={editing}
        projects={projects ?? []}
        onClose={() => setDialogOpen(false)}
        onSaved={handleSaved}
      />

      <AlertDialog
        open={deleting !== null}
        onOpenChange={(o) => !o && setDeleting(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("db.deleteTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("db.deleteBody", { name: deleting?.ref ?? "" })}
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

/**
 * 地址列：走隧道时真正决定「连到哪台机器」的是跳板机，所以把它显示在
 * 前面——只显示 127.0.0.1:3306 会让几条不同的线上库看起来一模一样。
 */
function addressOf(source: DataSource): string {
  const target = `${source.host}:${source.port}`
  if (!source.sshEnabled) return target
  const jump = source.sshUser
    ? `${source.sshUser}@${source.sshHost}`
    : source.sshHost
  return `${jump}:${source.sshPort} → ${target}`
}

/**
 * 数据源列表的列定义。放在组件外是为了让它读起来就是「这张表长什么样」，
 * 但它要用 t 与两个回调，所以做成工厂。
 */
function sourceColumns(
  t: TFunction,
  onEdit: (source: DataSource) => void,
  onDelete: (source: DataSource) => void
): SourceColumn[] {
  return [
    {
      id: "project",
      accessorFn: (source: DataSource) => source.project,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("db.project")} />
      ),
      meta: { label: t("db.project"), className: "font-medium" },
      cell: ({ row }) => (
        <span className="flex items-center gap-2">
          <StatusDot tone={row.original.disabled ? "muted" : "success"} />
          {row.original.project}
        </span>
      ),
    },
    {
      id: "env",
      accessorFn: (source: DataSource) => source.env,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("db.env")} />
      ),
      meta: { label: t("db.env"), className: "font-mono" },
      cell: ({ row }) => row.original.env,
    },
    {
      id: "database",
      accessorFn: (source: DataSource) => source.database,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("db.database")} />
      ),
      meta: {
        label: t("db.database"),
        className: "font-mono text-muted-foreground",
      },
      cell: ({ row }) => row.original.database,
    },
    {
      id: "host",
      accessorFn: (source: DataSource) => source.host,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("db.address")} />
      ),
      meta: {
        label: t("db.address"),
        className:
          "max-w-72 truncate font-mono text-muted-foreground tabular-nums",
      },
      cell: ({ row }) => (
        <span title={row.original.sshEnabled ? t("db.viaSsh") : undefined}>
          {addressOf(row.original)}
        </span>
      ),
    },
    {
      id: "read_only",
      accessorFn: (source: DataSource) => source.readOnly,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("db.mode")} />
      ),
      meta: { label: t("db.mode"), className: "text-muted-foreground" },
      cell: ({ row }) =>
        row.original.readOnly ? t("db.readOnlyBadge") : t("db.writable"),
    },
    {
      id: "actions",
      enableSorting: false,
      enableHiding: false,
      meta: { className: "w-20 py-0" },
      cell: ({ row }) => (
        <div className="flex justify-end gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
          <Button
            variant="ghost"
            size="icon"
            aria-label={t("db.editTitle")}
            onClick={(e) => {
              // 行本身是展开/收起，编辑与删除不该顺带把它也切一下。
              e.stopPropagation()
              onEdit(row.original)
            }}
          >
            <PencilIcon />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            aria-label={t("common.delete")}
            onClick={(e) => {
              e.stopPropagation()
              onDelete(row.original)
            }}
          >
            <Trash2Icon />
          </Button>
        </div>
      ),
    },
  ]
}
