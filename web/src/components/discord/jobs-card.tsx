import { useState } from "react"
import { useTranslation } from "react-i18next"
import type { TFunction } from "i18next"
import { toast } from "sonner"
import type { ColumnDef } from "@tanstack/react-table"
import {
  CalendarClockIcon,
  HistoryIcon,
  PencilIcon,
  PlayIcon,
  PlusIcon,
  Trash2Icon,
} from "lucide-react"

import { api } from "@/lib/api"
import type { DiscordInfo, DiscordJob, DiscordJobRun } from "@/types/discord"
import { formatDateTime, formatRelativeTime } from "@/lib/format"
import { useAsyncData } from "@/hooks/use-async-data"
import { ListPageStates } from "@/components/list-page-states"
import { Hint } from "@/components/hint"
import { StatusDot } from "@/components/status-dot"
import { DataTable } from "@/components/data-table/data-table"
import type { dataTableFeatures } from "@/components/data-table/data-table-features"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
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
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { JobDialog } from "./job-dialog"

type JobColumn = ColumnDef<typeof dataTableFeatures, DiscordJob, unknown>

/** 运行状态的展示名（key 保持字面量，动态模板过不了 i18n 类型增强）。 */
function statusLabel(s: DiscordJobRun["status"] | undefined, t: TFunction) {
  switch (s) {
    case "running":
      return t("discord.jobs.statusLabel.running")
    case "ok":
      return t("discord.jobs.statusLabel.ok")
    case "silent":
      return t("discord.jobs.statusLabel.silent")
    case "error":
      return t("discord.jobs.statusLabel.error")
    case "skipped":
      return t("discord.jobs.statusLabel.skipped")
    default:
      return ""
  }
}

function statusTone(s: DiscordJobRun["status"] | undefined) {
  switch (s) {
    case "ok":
    case "silent":
      return "success" as const
    case "error":
      return "destructive" as const
    case "running":
      return "warning" as const
    default:
      return "muted" as const
  }
}

/**
 * Discord 页的「定时任务」区块：挂在频道绑定上的任务清单，能新建、编辑、
 * 启停、立即运行、看运行记录、删除。主路是在子区里让 AI 建（acpp-cron
 * 工具面），这里是管理面。
 */
export function DiscordJobsCard({ info }: { info: DiscordInfo }) {
  const { t, i18n } = useTranslation()
  const {
    data: jobs,
    error,
    setData,
  } = useAsyncData<DiscordJob[]>(() => api.discord.jobs(), [])
  const [editing, setEditing] = useState<DiscordJob | "new" | null>(null)
  const [removing, setRemoving] = useState<DiscordJob | null>(null)
  const [runsOf, setRunsOf] = useState<DiscordJob | null>(null)

  const list = jobs ?? []
  const channelName = (id: string) => {
    const b = info.bindings.find((x) => x.channelId === id)
    return b ? `#${b.channelName || b.channelId}` : id
  }
  const guildOf = (id: string) =>
    info.bindings.find((x) => x.channelId === id)?.guildId

  function upsert(next: DiscordJob) {
    setData(
      list.some((j) => j.id === next.id)
        ? list.map((j) => (j.id === next.id ? next : j))
        : [...list, next]
    )
  }

  async function toggle(job: DiscordJob, enabled: boolean) {
    try {
      upsert(await api.discord.updateJob(job.id, { enabled }))
    } catch (err) {
      toast.error(t("discord.jobs.saveFailed"), {
        description: (err as Error).message,
      })
    }
  }

  async function run(job: DiscordJob) {
    try {
      await api.discord.runJob(job.id)
      toast.success(t("discord.jobs.triggered"))
      upsert({ ...job, running: true })
    } catch (err) {
      toast.error(t("discord.jobs.triggerFailed"), {
        description: (err as Error).message,
      })
    }
  }

  async function remove(job: DiscordJob) {
    try {
      await api.discord.removeJob(job.id)
      setData(list.filter((j) => j.id !== job.id))
      toast.success(t("discord.jobs.removed"))
    } catch (err) {
      toast.error(t("discord.jobs.removeFailed"), {
        description: (err as Error).message,
      })
    } finally {
      setRemoving(null)
    }
  }

  const columns: JobColumn[] = [
    {
      id: "name",
      accessorFn: (j) => j.name,
      header: () => t("discord.jobs.name"),
      meta: { label: t("discord.jobs.name"), pin: "left" },
      cell: ({ row }) => (
        <div className="flex flex-col gap-0.5">
          <span className="font-medium">{row.original.name}</span>
          <span className="text-xs text-muted-foreground">
            {channelName(row.original.scope)}
          </span>
        </div>
      ),
    },
    {
      id: "plan",
      accessorFn: (j) => j.plan ?? j.cron,
      header: () => t("discord.jobs.schedule"),
      meta: { label: t("discord.jobs.schedule") },
      cell: ({ row }) => (
        <span className="text-sm" title={row.original.cron}>
          {row.original.plan || row.original.cron}
        </span>
      ),
    },
    {
      id: "status",
      accessorFn: (j) => j.enabled,
      header: () => t("discord.jobs.status"),
      meta: { label: t("discord.jobs.status") },
      cell: ({ row }) => {
        const j = row.original
        return (
          <div className="flex items-center gap-2">
            <Switch
              size="sm"
              checked={j.enabled}
              aria-label={t("discord.jobs.enabled")}
              onCheckedChange={(v) => void toggle(j, v)}
            />
            {j.running ? (
              <StatusDot tone="warning" label={t("discord.jobs.running")} />
            ) : j.disabledReason ? (
              <span className="text-xs text-destructive">{j.disabledReason}</span>
            ) : null}
          </div>
        )
      },
    },
    {
      id: "next",
      accessorFn: (j) => j.nextRunAt ?? "",
      header: () => t("discord.jobs.next"),
      meta: { label: t("discord.jobs.next"), className: "tabular-nums" },
      cell: ({ row }) =>
        row.original.enabled && row.original.nextRunAt ? (
          <span title={row.original.nextRunAt}>
            {formatDateTime(row.original.nextRunAt, i18n.language)}
          </span>
        ) : (
          <span className="text-muted-foreground">—</span>
        ),
    },
    {
      id: "last",
      accessorFn: (j) => j.lastRunAt ?? "",
      header: () => t("discord.jobs.last"),
      meta: { label: t("discord.jobs.last") },
      cell: ({ row }) => {
        const j = row.original
        if (!j.lastRunAt) {
          return (
            <span className="text-xs text-muted-foreground">
              {t("discord.jobs.never")}
            </span>
          )
        }
        return (
          <div className="flex flex-col gap-0.5">
            <StatusDot
              tone={statusTone(j.lastStatus)}
              label={`${statusLabel(j.lastStatus, t)} · ${formatRelativeTime(j.lastRunAt, i18n.language)}`}
            />
            {j.lastSummary ? (
              <span
                className="line-clamp-1 max-w-[min(20rem,18vw)] text-xs text-muted-foreground"
                title={j.lastSummary}
              >
                {j.lastSummary}
              </span>
            ) : null}
          </div>
        )
      },
    },
    {
      id: "actions",
      header: () => null,
      meta: { pin: "right" },
      cell: ({ row }) => (
        <div className="flex justify-end gap-0.5">
          <Hint label={t("discord.jobs.run")}>
            <Button
              variant="ghost"
              size="icon"
              aria-label={t("discord.jobs.run")}
              disabled={row.original.running}
              onClick={() => void run(row.original)}
            >
              <PlayIcon />
            </Button>
          </Hint>
          <Hint label={t("discord.jobs.runs")}>
            <Button
              variant="ghost"
              size="icon"
              aria-label={t("discord.jobs.runs")}
              onClick={() => setRunsOf(row.original)}
            >
              <HistoryIcon />
            </Button>
          </Hint>
          <Hint label={t("discord.jobs.edit")}>
            <Button
              variant="ghost"
              size="icon"
              aria-label={t("discord.jobs.edit")}
              onClick={() => setEditing(row.original)}
            >
              <PencilIcon />
            </Button>
          </Hint>
          <Hint label={t("discord.jobs.remove")} align="end">
            <Button
              variant="ghost"
              size="icon"
              aria-label={t("discord.jobs.remove")}
              onClick={() => setRemoving(row.original)}
            >
              <Trash2Icon />
            </Button>
          </Hint>
        </div>
      ),
    },
  ]

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("discord.jobs.title")}</CardTitle>
        <CardDescription>{t("discord.jobs.description")}</CardDescription>
        <CardAction>
          <Button
            size="sm"
            disabled={info.bindings.length === 0}
            onClick={() => setEditing("new")}
          >
            <PlusIcon data-icon="inline-start" />
            {t("discord.jobs.add")}
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent>
        <DataTable
          columns={columns}
          data={error ? null : list}
          total={list.length}
          page={1}
          pageSize={list.length || 20}
          sorting={[]}
          onPage={() => {}}
          onPageSize={() => {}}
          onSorting={() => {}}
          empty={
            <ListPageStates
              icon={<CalendarClockIcon className="size-6" />}
              error={error}
              loading={!jobs && !error}
              emptyTitle={t("discord.jobs.empty")}
              emptyHint={t("discord.jobs.emptyHint")}
            />
          }
        />
      </CardContent>

      {editing ? (
        <JobDialog
          job={editing === "new" ? null : editing}
          bindings={info.bindings}
          defaultTz={info.defaultTz}
          onClose={() => setEditing(null)}
          onSaved={upsert}
        />
      ) : null}

      {runsOf ? (
        <RunsDialog
          job={runsOf}
          guildId={guildOf(runsOf.scope)}
          onClose={() => setRunsOf(null)}
        />
      ) : null}

      <AlertDialog
        open={removing !== null}
        onOpenChange={(open) => {
          if (!open) setRemoving(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("discord.jobs.removeTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("discord.jobs.removeDesc", { name: removing?.name })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                if (removing) void remove(removing)
              }}
            >
              {t("discord.jobs.removeConfirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  )
}

/** 最近运行记录：新的在前，每条带状态、耗时、摘要或失败原因，能跳到子区。 */
function RunsDialog({
  job,
  guildId,
  onClose,
}: {
  job: DiscordJob
  guildId?: string
  onClose: () => void
}) {
  const { t, i18n } = useTranslation()
  const runs = [...(job.runs ?? [])].reverse()
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("discord.jobs.runsTitle", { name: job.name })}</DialogTitle>
        </DialogHeader>
        {runs.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("discord.jobs.runsEmpty")}</p>
        ) : (
          <ul className="flex max-h-[60vh] flex-col gap-3 overflow-y-auto text-sm">
            {runs.map((r) => (
              <li key={r.id} className="flex flex-col gap-0.5">
                <div className="flex flex-wrap items-center gap-2">
                  <StatusDot tone={statusTone(r.status)} label={statusLabel(r.status, t)} />
                  <span className="tabular-nums" title={r.startedAt}>
                    {formatDateTime(r.startedAt, i18n.language)}
                  </span>
                  {r.endedAt && r.status !== "skipped" ? (
                    <span className="text-xs text-muted-foreground tabular-nums">
                      {Math.max(
                        1,
                        Math.round(
                          (new Date(r.endedAt).getTime() - new Date(r.startedAt).getTime()) / 1000
                        )
                      )}
                      s
                    </span>
                  ) : null}
                  {r.tools ? (
                    <span className="text-xs text-muted-foreground">🔧 {r.tools}</span>
                  ) : null}
                  {r.trigger === "manual" ? (
                    <span className="text-xs text-muted-foreground">{t("discord.jobs.manual")}</span>
                  ) : null}
                  {r.ref && guildId ? (
                    <a
                      className="text-xs text-primary underline-offset-2 hover:underline"
                      href={`https://discord.com/channels/${guildId}/${r.ref}`}
                      target="_blank"
                      rel="noreferrer"
                    >
                      {t("discord.jobs.openThread")}
                    </a>
                  ) : null}
                </div>
                {r.error || r.summary ? (
                  <span
                    className={
                      r.error ? "text-xs text-destructive" : "text-xs text-muted-foreground"
                    }
                  >
                    {r.error || r.summary}
                  </span>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </DialogContent>
    </Dialog>
  )
}
