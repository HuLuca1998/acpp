import { useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import type { TFunction } from "i18next"
import { Link } from "react-router"
import { toast } from "sonner"

import { api } from "@/lib/api"
import type { DiscordBinding, DiscordInfo } from "@/types/acp"
import { formatRelativeTime } from "@/lib/format"
import { useAsyncData } from "@/hooks/use-async-data"
import { DiscordIcon } from "@/components/agent-icon"
import { ListPageHeader } from "@/components/list-page-header"
import { ListPageStates } from "@/components/list-page-states"
import { StatusDot } from "@/components/status-dot"
import { Hint } from "@/components/hint"
import { DataTable } from "@/components/data-table/data-table"
import type { dataTableFeatures } from "@/components/data-table/data-table-features"
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
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  DatabaseIcon,
  HardDriveIcon,
  PencilIcon,
  SettingsIcon,
  Trash2Icon,
} from "lucide-react"

/**
 * Discord 页：频道 ↔ 仓库工作区的绑定管理（adr-016）。
 * 绑定只能在 Discord 频道里用 /init 创建；这里看得到、改得了模型与
 * 思考深度、能解绑。与会话体系完全独立。
 */
export function Discord() {
  const { t, i18n } = useTranslation()
  const {
    data: info,
    error,
    setData,
    reload,
  } = useAsyncData<DiscordInfo>(() => api.discord.get(), [])
  const [editing, setEditing] = useState<DiscordBinding | null>(null)
  const [removing, setRemoving] = useState<DiscordBinding | null>(null)

  const bindings = info?.bindings ?? []

  function patchBinding(next: DiscordBinding) {
    if (!info) return
    setData({
      ...info,
      bindings: info.bindings.map((b) =>
        b.channelId === next.channelId ? next : b
      ),
    })
  }

  async function unbind(binding: DiscordBinding) {
    try {
      await api.discord.removeBinding(binding.channelId)
      if (info) {
        setData({
          ...info,
          bindings: info.bindings.filter(
            (b) => b.channelId !== binding.channelId
          ),
        })
      }
      toast.success(t("discord.page.unbound"))
    } catch (err) {
      toast.error(t("discord.page.unbindFailed"), {
        description: (err as Error).message,
      })
    } finally {
      setRemoving(null)
    }
  }

  const statusText = info?.status.connected
    ? t("discord.page.botOnline", { name: info.status.botUser })
    : t("discord.page.botOffline")

  return (
    <div className="flex flex-col gap-4 p-4 lg:p-6">
      <ListPageHeader
        title={t("nav.discord")}
        description={t("discord.page.description")}
        total={info ? bindings.length : undefined}
      />
      <DataTable
        columns={bindingColumns(t, i18n.language, setEditing, setRemoving)}
        data={error ? null : bindings}
        total={bindings.length}
        page={1}
        pageSize={bindings.length || 20}
        sorting={[]}
        actions={
          <>
            <Button
              variant="outline"
              size="sm"
              render={<Link to="/settings?section=discord" />}
            >
              <SettingsIcon data-icon="inline-start" />
              {t("discord.page.gotoSettings")}
            </Button>
            <StatusDot
              tone={info?.status.connected ? "success" : "muted"}
              label={statusText}
            />
          </>
        }
        onReload={reload}
        onPage={() => {}}
        onPageSize={() => {}}
        onSorting={() => {}}
        empty={
          <ListPageStates
            icon={<DiscordIcon className="size-6" />}
            error={error}
            loading={!info && !error}
            emptyTitle={t("discord.page.empty")}
            emptyHint={t("discord.page.emptyHint")}
            emptyAction={
              <Button
                variant="outline"
                size="sm"
                render={<Link to="/settings?section=discord" />}
              >
                {t("discord.page.gotoSettings")}
              </Button>
            }
          />
        }
      />

      {editing && info ? (
        <EditBindingDialog
          binding={editing}
          info={info}
          onClose={() => setEditing(null)}
          onSaved={patchBinding}
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
            <AlertDialogTitle>{t("discord.page.unbindTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("discord.page.unbindDesc", {
                channel: `#${removing?.channelName || removing?.channelId}`,
                repo: removing?.repo,
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                if (removing) void unbind(removing)
              }}
            >
              {t("discord.page.unbindConfirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

/** 权限档展示名（key 保持字面量，动态模板过不了 i18n 类型增强）。 */
function accessLabel(v: string | undefined, t: TFunction) {
  switch (v) {
    case "safe":
      return t("discord.page.accessSafe")
    case "full":
      return t("discord.page.accessFull")
    default:
      return t("discord.page.accessAutoEdit")
  }
}

/** 编辑绑定：模型（按 agent 分组）、思考深度与安全权限。仓库不在这改——重建工作区走频道里的 /init。 */

type BindingColumn = ColumnDef<
  typeof dataTableFeatures,
  DiscordBinding,
  unknown
>

/**
 * 绑定列表的列定义。与数据库页、服务器页同一套骨架（Card + DataTable）——
 * 这一页此前是裸表格，风格和别处对不上。
 *
 * 数据库与服务器两列是这一页的重点：频道锁定了哪个库、哪台机器，正是
 * 「这个频道的 AI 能碰到什么」的全部答案。
 */
function bindingColumns(
  t: TFunction,
  lang: string,
  onEdit: (b: DiscordBinding) => void,
  onRemove: (b: DiscordBinding) => void
): BindingColumn[] {
  return [
    {
      id: "channel",
      accessorFn: (b: DiscordBinding) => b.channelName || b.channelId,
      header: () => t("discord.page.channel"),
      meta: { label: t("discord.page.channel"), pin: "left" },
      cell: ({ row }) => (
        <span className="font-medium">
          #{row.original.channelName || row.original.channelId}
        </span>
      ),
    },
    {
      id: "repo",
      accessorFn: (b: DiscordBinding) => b.repo,
      header: () => t("discord.page.repo"),
      meta: { label: t("discord.page.repo") },
      cell: ({ row }) => (
        <span className="font-mono text-xs">
          {row.original.repo}
          {row.original.branch ? `@${row.original.branch}` : ""}
        </span>
      ),
    },
    {
      id: "scope",
      header: () => t("discord.page.scope"),
      meta: { label: t("discord.page.scope") },
      // 库与机器并排一格：它们是同一个问题的两半——这个频道的 AI 够得着什么。
      cell: ({ row }) => (
        <div className="flex flex-col gap-0.5 text-xs">
          <span className="flex items-center gap-1 text-muted-foreground">
            <DatabaseIcon className="size-3 shrink-0" />
            <span className="truncate font-mono">
              {row.original.dataSourceRef || t("discord.page.scopeAll")}
            </span>
          </span>
          <span className="flex items-center gap-1 text-muted-foreground">
            <HardDriveIcon className="size-3 shrink-0" />
            <span className="truncate font-mono">
              {row.original.serverName || t("discord.page.scopeAll")}
            </span>
          </span>
        </div>
      ),
    },
    {
      id: "workdir",
      accessorFn: (b: DiscordBinding) => b.workdir,
      header: () => t("discord.page.workdir"),
      meta: { label: t("discord.page.workdir") },
      cell: ({ row }) => (
        <span
          className="line-clamp-1 max-w-[min(18rem,16vw)] font-mono text-xs text-muted-foreground"
          title={row.original.workdir}
        >
          {row.original.workdir}
        </span>
      ),
    },
    {
      id: "model",
      accessorFn: (b: DiscordBinding) => b.modelLabel || b.model,
      header: () => t("discord.page.model"),
      meta: { label: t("discord.page.model") },
      cell: ({ row }) => (
        <span className="text-sm">
          {row.original.modelLabel ||
            `${row.original.agent} · ${row.original.model}`}
        </span>
      ),
    },
    {
      id: "effort",
      accessorFn: (b: DiscordBinding) => b.effort,
      header: () => t("discord.page.effort"),
      meta: { label: t("discord.page.effort") },
      cell: ({ row }) => (
        <span className="text-sm">
          {row.original.effort || t("discord.page.effortDefault")}
        </span>
      ),
    },
    {
      id: "access",
      accessorFn: (b: DiscordBinding) => b.access,
      header: () => t("discord.page.access"),
      meta: { label: t("discord.page.access") },
      cell: ({ row }) => (
        <span className="text-sm">{accessLabel(row.original.access, t)}</span>
      ),
    },
    {
      id: "updated_at",
      accessorFn: (b: DiscordBinding) => b.updatedAt,
      header: () => t("discord.page.updatedAt"),
      meta: {
        label: t("discord.page.updatedAt"),
        className: "text-muted-foreground tabular-nums",
      },
      cell: ({ row }) => (
        <span title={row.original.updatedAt}>
          {formatRelativeTime(row.original.updatedAt, lang)}
        </span>
      ),
    },
    {
      id: "actions",
      header: () => null,
      meta: { pin: "right" },
      cell: ({ row }) => (
        <div className="flex justify-end gap-0.5">
          <Hint label={t("discord.page.edit")}>
            <Button
              variant="ghost"
              size="icon"
              aria-label={t("discord.page.edit")}
              onClick={() => onEdit(row.original)}
            >
              <PencilIcon />
            </Button>
          </Hint>
          <Hint label={t("discord.page.unbind")} align="end">
            <Button
              variant="ghost"
              size="icon"
              aria-label={t("discord.page.unbind")}
              onClick={() => onRemove(row.original)}
            >
              <Trash2Icon />
            </Button>
          </Hint>
        </div>
      ),
    },
  ]
}

function EditBindingDialog({
  binding,
  info,
  onClose,
  onSaved,
}: {
  binding: DiscordBinding
  info: DiscordInfo
  onClose: () => void
  onSaved: (next: DiscordBinding) => void
}) {
  const { t } = useTranslation()
  // 与后端 /init 表单同一编码：`agent|modelID`。
  const [model, setModel] = useState(`${binding.agent}|${binding.model}`)
  const [effort, setEffort] = useState(binding.effort || "default")
  const [access, setAccess] = useState(binding.access || "auto-edit")
  const [saving, setSaving] = useState(false)

  const efforts = useMemo(() => {
    const agent = model.split("|")[0]
    const fromCatalog = info.catalog.find((a) => a.agent === agent)?.efforts
    return fromCatalog?.length
      ? fromCatalog
      : ["low", "medium", "high", "xhigh", "max"]
  }, [info.catalog, model])

  // Base UI 的 SelectValue 闭合态直接渲染 value 字符串（`claude|xxx`），
  // 手动映射回展示名。
  const modelDisplay = useMemo(() => {
    const [agent, modelID] = model.split("|")
    const label = info.catalog
      .find((a) => a.agent === agent)
      ?.models.find((m) => m.id === modelID)?.label
    return label ? `${agent} · ${label}` : model
  }, [info.catalog, model])

  async function save() {
    const [agent, modelID] = model.split("|")
    const label = info.catalog
      .find((a) => a.agent === agent)
      ?.models.find((m) => m.id === modelID)?.label
    setSaving(true)
    try {
      const next = await api.discord.updateBinding(binding.channelId, {
        agent,
        model: modelID,
        modelLabel: label ? `${agent} · ${label}` : modelID,
        effort: effort === "default" ? "" : effort,
        access,
      })
      onSaved(next)
      toast.success(t("discord.page.saved"))
      onClose()
    } catch (err) {
      toast.error(t("discord.page.saveFailed"), {
        description: (err as Error).message,
      })
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("discord.page.editTitle")}</DialogTitle>
          <DialogDescription>
            #{binding.channelName || binding.channelId} · {binding.repo}
            <br />
            {t("discord.page.editDesc")}
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <Label>{t("discord.page.model")}</Label>
            <Select
              value={model}
              onValueChange={(v) => {
                if (v) setModel(v)
              }}
            >
              <SelectTrigger className="w-full">
                <SelectValue>{modelDisplay}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {info.catalog.map((a) => (
                  <SelectGroup key={a.agent}>
                    <SelectLabel>{a.agent}</SelectLabel>
                    {a.models.map((m) => (
                      <SelectItem key={m.id} value={`${a.agent}|${m.id}`}>
                        {a.agent} · {m.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="flex flex-col gap-2">
            <Label>{t("discord.page.effort")}</Label>
            <Select
              value={effort}
              onValueChange={(v) => {
                if (v) setEffort(v)
              }}
            >
              <SelectTrigger className="w-full">
                <SelectValue>
                  {effort === "default"
                    ? t("discord.page.effortDefault")
                    : effort}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="default">
                  {t("discord.page.effortDefault")}
                </SelectItem>
                {efforts.map((e) => (
                  <SelectItem key={e} value={e}>
                    {e}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="flex flex-col gap-2">
            <Label>{t("discord.page.access")}</Label>
            <Select
              value={access}
              onValueChange={(v) => {
                if (v) setAccess(v)
              }}
            >
              <SelectTrigger className="w-full">
                <SelectValue>{accessLabel(access, t)}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="auto-edit">
                  {t("discord.page.accessAutoEdit")}
                </SelectItem>
                <SelectItem value="full">
                  {t("discord.page.accessFull")}
                </SelectItem>
                <SelectItem value="safe">
                  {t("discord.page.accessSafe")}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button disabled={saving} onClick={() => void save()}>
            {t("common.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
