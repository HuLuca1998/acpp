import { useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"
import { toast } from "sonner"

import { api } from "@/lib/api"
import type { DiscordBinding, DiscordInfo } from "@/types/acp"
import { formatRelativeTime } from "@/lib/format"
import { useAsyncData } from "@/hooks/use-async-data"
import { DiscordIcon } from "@/components/agent-icon"
import { ListPageStates } from "@/components/list-page-states"
import { StatusDot } from "@/components/status-dot"
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { PencilIcon, SettingsIcon, Trash2Icon } from "lucide-react"

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
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-4 p-4 lg:p-6">
      <div className="flex items-center justify-between gap-4">
        <p className="text-sm text-muted-foreground">
          {t("discord.page.description")}
        </p>
        <div className="flex shrink-0 items-center gap-3">
          <StatusDot
            tone={info?.status.connected ? "success" : "muted"}
            label={statusText}
          />
          <Button
            variant="outline"
            size="sm"
            render={<Link to="/settings?section=discord" />}
          >
            <SettingsIcon data-icon="inline-start" />
            {t("discord.page.gotoSettings")}
          </Button>
        </div>
      </div>

      {error || !info || bindings.length === 0 ? (
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
      ) : (
        <div className="overflow-x-auto rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("discord.page.channel")}</TableHead>
                <TableHead>{t("discord.page.repo")}</TableHead>
                <TableHead>{t("discord.page.workdir")}</TableHead>
                <TableHead>{t("discord.page.model")}</TableHead>
                <TableHead>{t("discord.page.effort")}</TableHead>
                <TableHead>{t("discord.page.updatedAt")}</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {bindings.map((b) => (
                <TableRow key={b.channelId}>
                  <TableCell>
                    <span className="font-medium">
                      #{b.channelName || b.channelId}
                    </span>
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {b.repo}
                    {b.branch ? `@${b.branch}` : ""}
                  </TableCell>
                  <TableCell
                    className="max-w-56 truncate font-mono text-xs text-muted-foreground"
                    title={b.workdir}
                  >
                    {b.workdir}
                  </TableCell>
                  <TableCell className="text-sm">
                    {b.modelLabel || `${b.agent} · ${b.model}`}
                  </TableCell>
                  <TableCell className="text-sm">
                    {b.effort || t("discord.page.effortDefault")}
                  </TableCell>
                  <TableCell
                    className="text-sm text-muted-foreground tabular-nums"
                    title={b.updatedAt}
                  >
                    {formatRelativeTime(b.updatedAt, i18n.language)}
                  </TableCell>
                  <TableCell className="w-20">
                    <div className="flex items-center justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={t("discord.page.edit")}
                        className="text-muted-foreground hover:text-foreground"
                        onClick={() => setEditing(b)}
                      >
                        <PencilIcon />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={t("discord.page.unbind")}
                        className="text-muted-foreground hover:text-destructive"
                        onClick={() => setRemoving(b)}
                      >
                        <Trash2Icon />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

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

/** 编辑绑定：模型（按 agent 分组）与思考深度。仓库不在这改——重建工作区走频道里的 /init。 */
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
  const [saving, setSaving] = useState(false)

  const efforts = useMemo(() => {
    const agent = model.split("|")[0]
    const fromCatalog = info.catalog.find((a) => a.agent === agent)?.efforts
    return fromCatalog?.length
      ? fromCatalog
      : ["low", "medium", "high", "xhigh", "max"]
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
                <SelectValue />
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
                <SelectValue />
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
