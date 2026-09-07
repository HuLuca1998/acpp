import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { api } from "@/lib/api"
import type { DiscordBinding, DiscordJob } from "@/types/discord"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"

/**
 * 定时任务的新建 / 编辑表单。计划只做 cron（一次性任务在对话里让 AI 建
 * 更自然，表单里不放）；时区缺省本机；提示词是整个任务的关键，说明文字
 * 直接写在表单头上。
 */
export function JobDialog({
  job,
  bindings,
  defaultTz,
  onClose,
  onSaved,
}: {
  /** null = 新建 */
  job: DiscordJob | null
  bindings: DiscordBinding[]
  defaultTz?: string
  onClose: () => void
  onSaved: (next: DiscordJob, created: boolean) => void
}) {
  const { t } = useTranslation()
  const [name, setName] = useState(job?.name ?? "")
  const [channel, setChannel] = useState(
    job?.scope ?? bindings[0]?.channelId ?? ""
  )
  const [cron, setCron] = useState(job?.cron ?? "0 10 * * *")
  const [tz, setTz] = useState(job?.tz ?? defaultTz ?? "")
  const [prompt, setPrompt] = useState(job?.prompt ?? "")
  const [saving, setSaving] = useState(false)

  const presets: [string, string][] = [
    [t("discord.jobs.presetDaily"), "0 10 * * *"],
    [t("discord.jobs.presetWorkday"), "30 18 * * 1-5"],
    [t("discord.jobs.presetWeekly"), "0 9 * * 1"],
    [t("discord.jobs.presetHourly"), "0 */2 * * *"],
  ]

  const channelLabel = (id: string) => {
    const b = bindings.find((x) => x.channelId === id)
    return b ? `#${b.channelName || b.channelId} · ${b.repo}` : id
  }

  async function save() {
    setSaving(true)
    try {
      const next = job
        ? await api.discord.updateJob(job.id, { name, cron, tz, prompt })
        : await api.discord.addJob({
            channelId: channel,
            name,
            cron,
            tz,
            prompt,
          })
      onSaved(next, !job)
      toast.success(t("discord.jobs.saved"))
      onClose()
    } catch (err) {
      toast.error(t("discord.jobs.saveFailed"), {
        description: (err as Error).message,
      })
    } finally {
      setSaving(false)
    }
  }

  const valid = name.trim() && cron.trim() && prompt.trim() && channel

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {job ? t("discord.jobs.editTitle") : t("discord.jobs.createTitle")}
          </DialogTitle>
          <DialogDescription>{t("discord.jobs.formDesc")}</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="flex flex-col gap-2">
              <Label htmlFor="job-name">{t("discord.jobs.fieldName")}</Label>
              <Input
                id="job-name"
                value={name}
                placeholder={t("discord.jobs.namePlaceholder")}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label>{t("discord.jobs.fieldChannel")}</Label>
              <Select
                value={channel}
                disabled={job !== null}
                onValueChange={(v) => {
                  if (v) setChannel(v)
                }}
              >
                <SelectTrigger className="w-full">
                  <SelectValue>{channelLabel(channel)}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {bindings.map((b) => (
                    <SelectItem key={b.channelId} value={b.channelId}>
                      {channelLabel(b.channelId)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="grid gap-4 sm:grid-cols-[1fr_12rem]">
            <div className="flex flex-col gap-2">
              <Label htmlFor="job-cron">{t("discord.jobs.fieldCron")}</Label>
              <Input
                id="job-cron"
                className="font-mono"
                value={cron}
                placeholder={t("discord.jobs.cronPlaceholder")}
                onChange={(e) => setCron(e.target.value)}
              />
              <div className="flex flex-wrap items-center gap-1 text-xs text-muted-foreground">
                <span>{t("discord.jobs.cronHint")}</span>
                {presets.map(([label, expr]) => (
                  <Button
                    key={expr}
                    type="button"
                    variant={cron === expr ? "secondary" : "ghost"}
                    size="xs"
                    onClick={() => setCron(expr)}
                  >
                    {label}
                  </Button>
                ))}
              </div>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="job-tz">{t("discord.jobs.fieldTz")}</Label>
              <Input
                id="job-tz"
                className="font-mono"
                value={tz}
                placeholder="Asia/Shanghai"
                onChange={(e) => setTz(e.target.value)}
              />
            </div>
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="job-prompt">{t("discord.jobs.fieldPrompt")}</Label>
            <Textarea
              id="job-prompt"
              className="min-h-40 font-mono text-xs"
              value={prompt}
              placeholder={t("discord.jobs.promptPlaceholder")}
              onChange={(e) => setPrompt(e.target.value)}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button disabled={saving || !valid} onClick={() => void save()}>
            {t("common.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
