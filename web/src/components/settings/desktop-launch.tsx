import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { PowerIcon } from "lucide-react"

import { desktopLaunch, isDesktop, type LaunchPrefs } from "@/lib/desktop"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"

/**
 * 桌面应用的启动方式。只在 macOS 壳里出现——它改的是本机登录项，
 * 浏览器（含局域网访客）里没有这回事。
 */
export function DesktopLaunchCard() {
  const { t } = useTranslation()
  const [prefs, setPrefs] = useState<LaunchPrefs | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!isDesktop()) return
    let alive = true
    desktopLaunch
      .get()
      .then((p) => alive && setPrefs(p))
      .catch(() => alive && setPrefs(null))
    return () => {
      alive = false
    }
  }, [])

  if (!isDesktop() || !prefs) return null

  // 变更后一律用壳回读的快照覆盖：系统可能拒绝注册，开关得如实弹回去。
  const apply = (next: Promise<LaunchPrefs>) => {
    void next
      .then((p) => {
        setPrefs(p)
        setError(p.error ?? null)
      })
      .catch((e: Error) => setError(e.message))
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <PowerIcon className="size-4" />
          {t("settingsPage.desktopLaunch.title")}
        </CardTitle>
        <CardDescription>
          {t("settingsPage.desktopLaunch.description")}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1">
            <Label htmlFor="open-at-login">
              {t("settingsPage.desktopLaunch.openAtLogin")}
            </Label>
            <p className="text-xs text-muted-foreground">
              {t("settingsPage.desktopLaunch.openAtLoginHint")}
            </p>
          </div>
          <Switch
            id="open-at-login"
            checked={prefs.openAtLogin}
            onCheckedChange={(on) =>
              apply(desktopLaunch.setOpenAtLogin(on === true))
            }
          />
        </div>

        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1">
            <Label htmlFor="start-minimized">
              {t("settingsPage.desktopLaunch.startMinimized")}
            </Label>
            <p className="text-xs text-muted-foreground">
              {t("settingsPage.desktopLaunch.startMinimizedHint")}
            </p>
          </div>
          <Switch
            id="start-minimized"
            checked={prefs.startMinimized}
            onCheckedChange={(on) =>
              apply(desktopLaunch.setStartMinimized(on === true))
            }
          />
        </div>

        {error ? (
          <p className="text-xs text-destructive">
            {t("settingsPage.desktopLaunch.failed", { reason: error })}
          </p>
        ) : null}
      </CardContent>
    </Card>
  )
}
