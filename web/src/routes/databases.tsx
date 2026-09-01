import { useState } from "react"
import { useTranslation } from "react-i18next"
import type { TFunction } from "i18next"
import { toast } from "sonner"

import { Hint } from "@/components/hint"
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
import type { DataSource, DataSourceInput } from "@/types/acp"

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
  CopyPlusIcon,
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
  //
  // 用 `repo`（git 仓库身份）而不是 `name`（磁盘位置）：同一个仓库克隆到
  // 租户目录、discord 工作树、owner 自己的目录，位置各不相同但项目是同一个，
  // 按位置给下拉会列出一串重复项，选中的值还匹配不上别处开的会话。
  const { data: projects } = useAsyncData(
    () =>
      api.projects
        .list()
        .then((res) =>
          [...new Set(res.items.map((p) => p.repo || p.name))]
            .filter(Boolean)
            .sort()
        )
        .catch(() => []),
    []
  )

  const [editing, setEditing] = useState<DataSource | null>(null)
  const [prefill, setPrefill] = useState<DataSourceInput | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)
  // 复制的序号：只用来让表单在「连着复制两条」时重建（key 变了）。
  // 放 state 而不是 ref——组件体内的函数在 React Compiler 眼里可能落在
  // render 路径上，读写 ref 与调 Date.now() 都会被拦。
  const [copyNonce, setCopyNonce] = useState(0)
  const [deleting, setDeleting] = useState<DataSource | null>(null)
  const [opened, setOpened] = useState<DataSource | null>(null)

  function openEdit(source: DataSource | null) {
    setEditing(source)
    setPrefill(null)
    setDialogOpen(true)
  }

  /**
   * 复制一条连接：同一台库上常常要开好几个连接（几个库、几个环境），
   * 地址、账号、密码、SSH 全都一样，只有项目 / 环境 / 库不同。
   *
   * 密码得单独取——列表里没有它（响应只给 hasPassword）。取不到就照常
   * 打开，让用户自己填：复制是个便利，不该因为拿不到密码就整个不能用。
   */
  async function openCopy(source: DataSource) {
    let password = ""
    try {
      password = (await api.datasources.secret(source.id)).password
    } catch {
      toast.warning(t("db.copyNoPassword"))
    }
    setEditing(null)
    setCopyNonce((n) => n + 1)
    setPrefill({
      project: "",
      env: "",
      host: source.host,
      port: source.port,
      user: source.user,
      password,
      database: "",
      params: source.params,
      note: source.note,
      sshEnabled: source.sshEnabled,
      serverId: source.serverId,
      readOnly: source.readOnly,
    })
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

  const columns = sourceColumns(t, openEdit, openCopy, setDeleting)

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
            // 走 --row-bg 而不是直接 bg-*：固定列（项目、操作）自带不透明
            // 底遮挡滚动内容，底色得跟这一行同源，否则展开的那行会从固定
            // 列这里断色。
            rowClassName={(source) =>
              opened?.id === source.id
                ? "[--row-bg:var(--accent)] bg-[var(--row-bg)]"
                : undefined
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
              <Hint label={t("common.close")} align="end">
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={t("common.close")}
                  onClick={() => setOpened(null)}
                >
                  <XIcon />
                </Button>
              </Hint>
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
        prefill={prefill}
        prefillKey={`copy-${copyNonce}`}
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
  // 跳板机的地址在服务器页维护，这里只显示它的名字——名字本来就是为了
  // 一眼认出是哪台机器而起的。
  return `${source.serverName ?? "?"} → ${target}`
}

/**
 * 数据源列表的列定义。放在组件外是为了让它读起来就是「这张表长什么样」，
 * 但它要用 t 与两个回调，所以做成工厂。
 */
function sourceColumns(
  t: TFunction,
  onEdit: (source: DataSource) => void,
  onCopy: (source: DataSource) => void,
  onDelete: (source: DataSource) => void
): SourceColumn[] {
  return [
    {
      id: "project",
      accessorFn: (source: DataSource) => source.project,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("db.project")} />
      ),
      meta: { label: t("db.project"), className: "font-medium", pin: "left" },
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
      meta: { className: "w-24 py-0", pin: "right" },
      cell: ({ row }) => (
        <div className="flex justify-end gap-0.5">
          <Hint label={t("db.editTitle")}>
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
          </Hint>
          {/* 复制：同一台库上常常要开好几个连接（几个库、几个环境），
              地址、账号、密码、SSH 全都一样，只有项目 / 环境 / 库不同。 */}
          <Hint label={t("db.copy")} desc={t("db.copyDesc")}>
            <Button
              variant="ghost"
              size="icon"
              aria-label={t("db.copy")}
              onClick={(e) => {
                e.stopPropagation()
                onCopy(row.original)
              }}
            >
              <CopyPlusIcon />
            </Button>
          </Hint>
          <Hint label={t("db.deleteTitle")} align="end">
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
          </Hint>
        </div>
      ),
    },
  ]
}
