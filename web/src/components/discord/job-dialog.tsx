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
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"

type Mode = "cron" | "once"

/** datetime-local 的值（浏览器本地时区、到分钟）→ 绝对时刻的 ISO 串。 */
function toISO(local: string) {
  return local ? new Date(local).toISOString() : ""
}

/** 绝对时刻 → datetime-local 输入框要的本地时区串（不能直接用 ISO，它带 Z）。 */
function toLocalInput(iso?: string) {
  if (!iso) return ""
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function inHours(h: number) {
  return toLocalInput(new Date(Date.now() + h * 3_600_000).toISOString())
}

function tomorrowAt(hour: number) {
  const d = new Date()
  d.setDate(d.getDate() + 1)
  d.setHours(hour, 0, 0, 0)
  return toLocalInput(d.toISOString())
}

/**
 * 定时任务的新建 / 编辑表单。计划分两种：循环填 cron，一次性选一个时刻
 * （跑成功即自动删）；两者在后端是 cron / at 二选一，切换模式时把另一边
 * 清掉。时区缺省本机；提示词是整个任务的关键，说明文字直接写在表单头上。
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
  const [mode, setMode] = useState<Mode>(job?.at ? "once" : "cron")
  const [cron, setCron] = useState(job?.cron || "0 10 * * *")
  const [at, setAt] = useState(toLocalInput(job?.at))
  const [tz, setTz] = useState(job?.tz ?? defaultTz ?? "")
  const [prompt, setPrompt] = useState(job?.prompt ?? "")
  const [saving, setSaving] = useState(false)

  const presets: [string, string][] = [
    [t("discord.jobs.presetDaily"), "0 10 * * *"],
    [t("discord.jobs.presetWorkday"), "30 18 * * 1-5"],
    [t("discord.jobs.presetWeekly"), "0 9 * * 1"],
    [t("discord.jobs.presetHourly"), "0 */2 * * *"],
  ]
  // 一次性的快捷是「点了才算」的函数：值随当下时间变，不能预先算死。
  const oncePresets: [string, () => string][] = [
    [t("discord.jobs.presetIn1h"), () => inHours(1)],
    [t("discord.jobs.presetIn2h"), () => inHours(2)],
    [t("discord.jobs.presetTomorrow9"), () => tomorrowAt(9)],
  ]

  const channelLabel = (id: string) => {
    const b = bindings.find((x) => x.channelId === id)
    return b ? `#${b.channelName || b.channelId} · ${b.repo}` : id
  }

  async function save() {
    setSaving(true)
    try {
      // 后端 cron / at 互斥且自动对冲（给了 cron 就清 at，反之亦然）；这里
      // 一次性显式清 cron、切回循环显式 clearAt，只是把意图写明白。
      const timing =
        mode === "once"
          ? { cron: "", at: toISO(at) }
          : { cron, ...(job?.at ? { clearAt: true } : {}) }
      const next = job
        ? await api.discord.updateJob(job.id, { name, tz, prompt, ...timing })
        : await api.discord.addJob({
            channelId: channel,
            name,
            tz,
            prompt,
            ...timing,
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

  // 只查填没填；「时刻已过去」交给后端（报错带当前时刻），渲染期不读时钟。
  const timingOk = mode === "once" ? Boolean(at) : Boolean(cron.trim())
  const valid = name.trim() && timingOk && prompt.trim() && channel

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
              <div className="flex items-center justify-between gap-2">
                <Label htmlFor={mode === "once" ? "job-at" : "job-cron"}>
                  {mode === "once"
                    ? t("discord.jobs.fieldAt")
                    : t("discord.jobs.fieldCron")}
                </Label>
                <ToggleGroup
                  value={[mode]}
                  variant="outline"
                  size="sm"
                  onValueChange={(v) => {
                    const next = v[0] as Mode | undefined
                    if (next) setMode(next)
                  }}
                >
                  <ToggleGroupItem value="cron">
                    {t("discord.jobs.modeCron")}
                  </ToggleGroupItem>
                  <ToggleGroupItem value="once">
                    {t("discord.jobs.modeOnce")}
                  </ToggleGroupItem>
                </ToggleGroup>
              </div>
              {mode === "once" ? (
                <>
                  <Input
                    id="job-at"
                    type="datetime-local"
                    className="font-mono"
                    value={at}
                    onChange={(e) => setAt(e.target.value)}
                  />
                  <div className="flex flex-wrap items-center gap-1 text-xs text-muted-foreground">
                    <span>{t("discord.jobs.atHint")}</span>
                    {oncePresets.map(([label, pick]) => (
                      <Button
                        key={label}
                        type="button"
                        variant="ghost"
                        size="xs"
                        onClick={() => setAt(pick())}
                      >
                        {label}
                      </Button>
                    ))}
                  </div>
                </>
              ) : (
                <>
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
                </>
              )}
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
