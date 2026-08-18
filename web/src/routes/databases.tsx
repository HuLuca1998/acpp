import { useState } from "react"
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
  DatabaseIcon,
  NetworkIcon,
  PencilIcon,
  PlusIcon,
  Trash2Icon,
} from "lucide-react"

/**
 * 数据库页：按项目分组管理 MySQL 连接（adr-008）。
 *
 * 分组不是装饰——数据源的身份就是「项目 + 环境」，而项目决定了哪些
 * 会话能看到它。按项目摆，看到的结构就是生效的结构。
 */
export function Databases() {
  const { t } = useTranslation()
  const {
    data: sources,
    error,
    setData: setSources,
  } = useAsyncData(() => api.datasources.list(), [])
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
  const [browsing, setBrowsing] = useState<DataSource | null>(null)

  function openEdit(source: DataSource | null) {
    setEditing(source)
    setDialogOpen(true)
  }

  function handleSaved(saved: DataSource) {
    setSources((prev) => {
      const list = prev ?? []
      const at = list.findIndex((s) => s.id === saved.id)
      const next = at >= 0 ? list.with(at, saved) : [...list, saved]
      return next.sort((a, b) => a.ref.localeCompare(b.ref))
    })
    setEditing(saved)
  }

  async function confirmDelete() {
    if (!deleting) return
    try {
      await api.datasources.remove(deleting.id)
      setSources((prev) => (prev ?? []).filter((s) => s.id !== deleting.id))
      if (browsing?.id === deleting.id) setBrowsing(null)
      toast.success(t("db.deleted"))
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setDeleting(null)
    }
  }

  const groups = groupByProject(sources ?? [])

  return (
    <div className="flex flex-col gap-4 p-4 lg:p-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-lg font-semibold">{t("db.title")}</h1>
          <p className="text-sm text-muted-foreground">{t("db.description")}</p>
        </div>
        <Button onClick={() => openEdit(null)}>
          <PlusIcon data-icon="inline-start" />
          {t("db.add")}
        </Button>
      </div>

      {sources === null || error || sources.length === 0 ? (
        <ListPageStates
          icon={<DatabaseIcon />}
          error={error}
          loading={sources === null}
          emptyTitle={t("db.empty")}
          emptyHint={t("db.emptyHint")}
          emptyAction={
            <Button onClick={() => openEdit(null)}>
              <PlusIcon data-icon="inline-start" />
              {t("db.add")}
            </Button>
          }
        />
      ) : (
        <div className="flex flex-col gap-4">
          {groups.map(([project, items]) => (
            <Card key={project}>
              <CardHeader>
                <CardTitle className="font-mono">{project}</CardTitle>
                <CardDescription>
                  {t("db.envCount", { count: items.length })}
                </CardDescription>
                <CardAction>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => openEdit(null)}
                  >
                    <PlusIcon data-icon="inline-start" />
                    {t("db.add")}
                  </Button>
                </CardAction>
              </CardHeader>
              <CardContent className="flex flex-col gap-1">
                {items.map((source) => (
                  <EnvRow
                    key={source.id}
                    source={source}
                    active={browsing?.id === source.id}
                    onToggle={() =>
                      setBrowsing((prev) =>
                        prev?.id === source.id ? null : source
                      )
                    }
                    onEdit={() => openEdit(source)}
                    onDelete={() => setDeleting(source)}
                  />
                ))}
                {browsing && items.some((s) => s.id === browsing.id) ? (
                  <div className="mt-2 border-t border-border pt-3">
                    <DataSourceExplorer source={browsing} />
                  </div>
                ) : null}
              </CardContent>
            </Card>
          ))}
        </div>
      )}

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

function EnvRow({
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
    <div
      className={cn(
        "group flex items-center gap-2 rounded-md px-2 py-1.5 text-sm",
        active ? "bg-accent text-accent-foreground" : "hover:bg-muted"
      )}
    >
      <StatusDot tone={source.disabled ? "muted" : "success"} />
      <button
        type="button"
        onClick={onToggle}
        className="flex min-w-0 flex-1 items-center gap-2 text-left outline-none"
      >
        <span className="shrink-0 font-medium">{source.env}</span>
        <span className="min-w-0 truncate font-mono text-xs text-muted-foreground">
          {source.host}:{source.port}
          {source.database ? `/${source.database}` : ""}
        </span>
        {source.readOnly ? (
          <span className="shrink-0 rounded border border-border px-1 text-[10px] text-muted-foreground">
            {t("db.readOnlyBadge")}
          </span>
        ) : null}
        {source.sshEnabled ? (
          <NetworkIcon
            className="size-3.5 shrink-0 text-muted-foreground"
            aria-label={t("db.ssh")}
          />
        ) : null}
        {source.note ? (
          <span className="min-w-0 truncate text-xs text-muted-foreground">
            {source.note}
          </span>
        ) : null}
      </button>
      <div className="flex shrink-0 gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
        <Button
          variant="ghost"
          size="icon"
          aria-label={t("db.editTitle")}
          onClick={onEdit}
        >
          <PencilIcon />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          aria-label={t("common.delete")}
          onClick={onDelete}
        >
          <Trash2Icon />
        </Button>
      </div>
    </div>
  )
}

/** 按项目分组，组内按环境名排序（后端已排好，这里只保持稳定）。 */
function groupByProject(sources: DataSource[]): [string, DataSource[]][] {
  const byProject = new Map<string, DataSource[]>()
  for (const source of sources) {
    const list = byProject.get(source.project)
    if (list) list.push(source)
    else byProject.set(source.project, [source])
  }
  return [...byProject.entries()].sort(([a], [b]) => a.localeCompare(b))
}
