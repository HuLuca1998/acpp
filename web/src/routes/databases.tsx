import { useCallback, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { ListPageStates } from "@/components/list-page-states"
import { DataSourceDialog } from "@/components/db/datasource-dialog"
import { DataSourceExplorer } from "@/components/db/datasource-explorer"
import { StatusDot } from "@/components/status-dot"
import { useAsyncData } from "@/hooks/use-async-data"
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

/** 一页多少条。列表分页而不是全量拉——连接多起来时页面不该跟着变慢。 */
const PAGE_SIZE = 50

/**
 * 数据库页：管理 MySQL 连接（adr-008）。
 *
 * 一行一条连接（项目 / 环境 / 库 / 地址 / 读写），点行展开它的表浏览与
 * SQL 控制台。项目与环境是这条连接的身份，项目还决定了哪些会话看得到它；
 * 库是绑定的——一条连接只对应一个库。
 */
export function Databases() {
  const { t } = useTranslation()
  const { data, error, setData } = useAsyncData(
    () => api.datasources.list({ pageSize: PAGE_SIZE }),
    []
  )
  const sources = data?.items ?? null
  const total = data?.total ?? 0

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

  /** 追加下一页：以已加载条数推算页码，不维护独立游标。 */
  const loadMore = useCallback(() => {
    const loaded = sources?.length ?? 0
    api.datasources
      .list({ page: Math.floor(loaded / PAGE_SIZE) + 1, pageSize: PAGE_SIZE })
      .then((res) => {
        setData((prev) => {
          const seen = new Set((prev?.items ?? []).map((s) => s.id))
          return {
            ...res,
            items: [
              ...(prev?.items ?? []),
              ...res.items.filter((s) => !seen.has(s.id)),
            ],
          }
        })
      })
      .catch((err) => toast.error((err as Error).message))
  }, [sources, setData])

  function openEdit(source: DataSource | null) {
    setEditing(source)
    setDialogOpen(true)
  }

  function handleSaved(saved: DataSource) {
    setData((prev) => {
      const items = prev?.items ?? []
      const at = items.findIndex((s) => s.id === saved.id)
      const next = at >= 0 ? items.with(at, saved) : [...items, saved]
      return {
        items: next.sort((a, b) => a.ref.localeCompare(b.ref)),
        total: at >= 0 ? (prev?.total ?? next.length) : (prev?.total ?? 0) + 1,
        page: prev?.page ?? 1,
        pageSize: prev?.pageSize ?? PAGE_SIZE,
      }
    })
    setEditing(saved)
    // 展开中的那条被改了：换成新记录，免得面板还按旧连接查。
    setOpened((prev) => (prev?.id === saved.id ? saved : prev))
  }

  async function confirmDelete() {
    if (!deleting) return
    try {
      await api.datasources.remove(deleting.id)
      setData((prev) =>
        prev
          ? {
              ...prev,
              items: prev.items.filter((s) => s.id !== deleting.id),
              total: Math.max(prev.total - 1, 0),
            }
          : prev
      )
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
          {sources !== null && sources.length < total ? (
            <div className="flex justify-center py-3">
              <Button
                variant="ghost"
                size="sm"
                className="text-muted-foreground"
                onClick={loadMore}
              >
                {t("db.loadMore", { loaded: sources.length, total })}
              </Button>
            </div>
          ) : null}
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
