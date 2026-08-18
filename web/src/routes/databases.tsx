import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { ListPageStates } from "@/components/list-page-states"
import { DataSourceDialog } from "@/components/db/datasource-dialog"
import { DataSourceExplorer } from "@/components/db/datasource-explorer"
import { StatusDot } from "@/components/status-dot"
import { useAsyncData } from "@/hooks/use-async-data"
import { usePagedData } from "@/hooks/use-paged-data"
import { DataPagination } from "@/components/data-pagination"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { DataSource } from "@/types/acp"
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
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
    setPage,
    setPageSize,
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
          {error || sources === null || sources.length === 0 ? (
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
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("db.project")}</TableHead>
                  <TableHead>{t("db.env")}</TableHead>
                  <TableHead>{t("db.database")}</TableHead>
                  <TableHead>{t("db.address")}</TableHead>
                  <TableHead>{t("db.mode")}</TableHead>
                  <TableHead className="w-20" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {sources.map((source) => (
                  <SourceRow
                    key={source.id}
                    source={source}
                    active={opened?.id === source.id}
                    onToggle={() =>
                      setOpened((prev) =>
                        prev?.id === source.id ? null : source
                      )
                    }
                    onEdit={() => openEdit(source)}
                    onDelete={() => setDeleting(source)}
                  />
                ))}
              </TableBody>
            </Table>
          )}
          <DataPagination
            total={total}
            page={page}
            pageSize={pageSize}
            onPage={setPage}
            onPageSize={setPageSize}
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

function SourceRow({
  source,
  active,
  onToggle,
  onEdit,
  onDelete,
}: {
  source: DataSource
  active: boolean
  onToggle: () => void
  onEdit: () => void
  onDelete: () => void
}) {
  const { t } = useTranslation()
  return (
    <TableRow
      className={cn("group cursor-pointer", active && "bg-accent/50")}
      onClick={onToggle}
    >
      <TableCell className="font-medium">
        <span className="flex items-center gap-2">
          <StatusDot tone={source.disabled ? "muted" : "success"} />
          {source.project}
        </span>
      </TableCell>
      <TableCell className="font-mono">{source.env}</TableCell>
      <TableCell className="font-mono text-muted-foreground">
        {source.database}
      </TableCell>
      <TableCell
        className="max-w-72 truncate font-mono text-muted-foreground tabular-nums"
        title={source.sshEnabled ? t("db.viaSsh") : undefined}
      >
        {addressOf(source)}
      </TableCell>
      <TableCell className="text-muted-foreground">
        {source.readOnly ? t("db.readOnlyBadge") : t("db.writable")}
      </TableCell>
      <TableCell className="py-0">
        <div className="flex justify-end gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
          <Button
            variant="ghost"
            size="icon"
            aria-label={t("db.editTitle")}
            onClick={(e) => {
              e.stopPropagation()
              onEdit()
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
              onDelete()
            }}
          >
            <Trash2Icon />
          </Button>
        </div>
      </TableCell>
    </TableRow>
  )
}
