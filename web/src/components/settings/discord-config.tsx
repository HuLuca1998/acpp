import { useCallback, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import type { TFunction } from "i18next"
import { Link } from "react-router"
import { CopyIcon } from "lucide-react"
import { toast } from "sonner"

import { api } from "@/lib/api"
import { copyText } from "@/lib/clipboard"
import type { DiscordInfo } from "@/types/acp"
import { DiscordIcon } from "@/components/agent-icon"
import { DirPicker } from "@/components/dir-picker/dir-picker"
import { StatusDot, type StatusTone } from "@/components/status-dot"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { FolderOpenIcon, RefreshCwIcon } from "lucide-react"

/**
 * 设置页的 Discord 分区：开关、bot token、工作根与上线状态（adr-016）。
 * 开关即时生效；token 与工作根走「保存」。状态是 gateway 的实时快照——
 * 保存后延迟拉一次，把「连接中 → 已上线」的变化接住。
 */
export function DiscordConfigCard() {
  const { t } = useTranslation()
  const [info, setInfo] = useState<DiscordInfo | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [token, setToken] = useState("")
  // null = 用户没动过，显示后端值；编辑后才进保存补丁。
  const [workRoot, setWorkRoot] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [pickerOpen, setPickerOpen] = useState(false)
  const refreshTimer = useRef<number | null>(null)

  const refresh = useCallback(() => {
    return api.discord
      .get()
      .then((res) => {
        setInfo(res)
        setError(null)
      })
      .catch((err: Error) => setError(err.message))
  }, [])

  useEffect(() => {
    void refresh()
    return () => {
      if (refreshTimer.current !== null)
        window.clearTimeout(refreshTimer.current)
    }
  }, [refresh])

  /** gateway 连上需要一两秒，保存后延迟再拉一次状态。 */
  const scheduleRefresh = useCallback(() => {
    if (refreshTimer.current !== null) window.clearTimeout(refreshTimer.current)
    refreshTimer.current = window.setTimeout(() => void refresh(), 2000)
  }, [refresh])

  async function toggle(enabled: boolean) {
    setSaving(true)
    try {
      setInfo(await api.discord.saveConfig({ enabled }))
      scheduleRefresh()
    } catch (err) {
      toast.error(t("discord.settings.saveFailed"), {
        description: (err as Error).message,
      })
    } finally {
      setSaving(false)
    }
  }

  async function save() {
    const patch: { botToken?: string; workRoot?: string } = {}
    if (token.trim() !== "") patch.botToken = token.trim()
    if (workRoot !== null && workRoot !== info?.config.workRoot)
      patch.workRoot = workRoot
    if (Object.keys(patch).length === 0) return
    setSaving(true)
    try {
      setInfo(await api.discord.saveConfig(patch))
      setToken("")
      setWorkRoot(null)
      toast.success(t("discord.settings.saved"))
      scheduleRefresh()
    } catch (err) {
      toast.error(t("discord.settings.saveFailed"), {
        description: (err as Error).message,
      })
    } finally {
      setSaving(false)
    }
  }

  if (info === null && error === null) {
    return <Skeleton className="h-64 w-full" />
  }

  const status = statusLine(info, t)
  const dirty =
    token.trim() !== "" ||
    (workRoot !== null && workRoot !== info?.config.workRoot)

  return (
    <div className="flex flex-col gap-4">
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      {info ? (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <DiscordIcon className="size-4" />
              {t("discord.settings.title")}
            </CardTitle>
            <CardDescription>
              {t("discord.settings.description")}
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-5">
            {/* 开关 + 实时状态一行：这是这块配置的「现在怎么样」。 */}
            <div className="flex items-center justify-between gap-4">
              <div className="flex min-w-0 flex-col gap-1">
                <Label htmlFor="discord-enabled">
                  {t("discord.settings.enable")}
                </Label>
                <span className="text-sm text-muted-foreground">
                  {t("discord.settings.enableDesc")}
                </span>
              </div>
              <div className="flex shrink-0 items-center gap-3">
                <span className="flex items-center gap-2 text-sm text-muted-foreground">
                  <StatusDot tone={status.tone} pulse={status.pulse} />
                  {status.text}
                </span>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={t("discord.settings.refresh")}
                  onClick={() => void refresh()}
                >
                  <RefreshCwIcon />
                </Button>
                <Switch
                  id="discord-enabled"
                  checked={info.config.enabled}
                  disabled={saving}
                  onCheckedChange={(v) => void toggle(v)}
                />
              </div>
            </div>

            {info.status.lastError && info.config.enabled ? (
              <Alert variant="destructive">
                <AlertDescription className="font-mono text-xs">
                  {info.status.lastError}
                </AlertDescription>
              </Alert>
            ) : null}

            {/* token：永不回显，已配置只给标记。 */}
            <div className="flex flex-col gap-2">
              <div className="flex items-center gap-2">
                <Label htmlFor="discord-token">
                  {t("discord.settings.token")}
                </Label>
                {info.config.tokenSet ? (
                  <Badge variant="secondary">
                    {t("discord.settings.tokenSet")}
                  </Badge>
                ) : null}
              </div>
              <Input
                id="discord-token"
                type="password"
                autoComplete="off"
                value={token}
                onChange={(e) => setToken(e.target.value)}
                placeholder={
                  info.config.tokenSet
                    ? t("discord.settings.tokenReplacePlaceholder")
                    : t("discord.settings.tokenPlaceholder")
                }
                className="font-mono text-sm"
              />
            </div>

            {/* 工作根：频道仓库的克隆落点。 */}
            <div className="flex flex-col gap-2">
              <Label htmlFor="discord-workroot">
                {t("discord.settings.workRoot")}
              </Label>
              <div className="flex items-center gap-2">
                <Input
                  id="discord-workroot"
                  value={workRoot ?? info.config.workRoot}
                  onChange={(e) => setWorkRoot(e.target.value)}
                  className="font-mono text-sm"
                />
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setPickerOpen(true)}
                >
                  <FolderOpenIcon data-icon="inline-start" />
                  {t("common.browse")}
                </Button>
              </div>
              <span className="text-sm text-muted-foreground">
                {t("discord.settings.workRootDesc")}
              </span>
            </div>

            <div className="flex items-center justify-between gap-4">
              <span className="text-sm text-muted-foreground">
                {t("discord.settings.howTo")}{" "}
                <Link to="/discord" className="underline underline-offset-4">
                  {t("nav.discord")}
                </Link>
              </span>
              <Button
                size="sm"
                disabled={!dirty || saving}
                onClick={() => void save()}
              >
                {t("discord.settings.save")}
              </Button>
            </div>
          </CardContent>
        </Card>
      ) : null}

      {/* 所在服务器：bot 有没有被请进门，一眼可见。 */}
      {info?.config.enabled && info.config.tokenSet ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">
              {t("discord.settings.guilds")}
            </CardTitle>
          </CardHeader>
          <CardContent>
            {info.status.guilds.length === 0 ? (
              <p className="text-sm text-muted-foreground">
                {t("discord.settings.noGuilds")}
              </p>
            ) : (
              <div className="flex flex-wrap gap-2">
                {info.status.guilds.map((g) => (
                  <Badge key={g.id} variant="secondary">
                    {g.name || g.id}
                  </Badge>
                ))}
              </div>
            )}
            {info.inviteUrl ? (
              <div className="mt-4 space-y-1.5">
                <p className="text-sm font-medium">
                  {t("discord.settings.invite")}
                </p>
                <div className="flex items-center gap-2">
                  <Input
                    readOnly
                    value={info.inviteUrl}
                    className="font-mono text-xs"
                    onFocus={(e) => e.currentTarget.select()}
                  />
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="shrink-0"
                    onClick={() => {
                      void copyText(info.inviteUrl ?? "").then((ok) =>
                        ok
                          ? toast.success(t("common.copied"))
                          : toast.error(t("common.copyFailed"))
                      )
                    }}
                  >
                    <CopyIcon data-icon="inline-start" />
                    {t("common.copy")}
                  </Button>
                </div>
                <p className="text-xs text-muted-foreground">
                  {t("discord.settings.inviteHint")}
                </p>
              </div>
            ) : null}
          </CardContent>
        </Card>
      ) : null}

      <DirPicker
        open={pickerOpen}
        onOpenChange={setPickerOpen}
        initialPath={workRoot ?? info?.config.workRoot ?? undefined}
        onSelect={(path) => setWorkRoot(path)}
      />
    </div>
  )
}

/** 状态行推导：出错 > 连接中 > 已上线 > 等 token > 未启用。 */
function statusLine(
  info: DiscordInfo | null,
  t: TFunction
): { tone: StatusTone; pulse: boolean; text: string } {
  if (!info || !info.config.enabled) {
    return {
      tone: "muted",
      pulse: false,
      text: t("discord.settings.status.disabled"),
    }
  }
  if (!info.config.tokenSet) {
    return {
      tone: "warning",
      pulse: false,
      text: t("discord.settings.status.waitingToken"),
    }
  }
  if (info.status.connected) {
    return {
      tone: "success",
      pulse: false,
      text: t("discord.settings.status.online", { name: info.status.botUser }),
    }
  }
  if (info.status.lastError) {
    return {
      tone: "destructive",
      pulse: false,
      text: t("discord.settings.status.error"),
    }
  }
  return {
    tone: "warning",
    pulse: true,
    text: t("discord.settings.status.connecting"),
  }
}
