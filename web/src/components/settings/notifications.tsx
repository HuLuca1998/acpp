import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { BellIcon, ExternalLinkIcon, RefreshCwIcon } from "lucide-react"

import { desktopNotify, isDesktop, type NotifyStatus } from "@/lib/desktop"
import { StatusDot, type StatusTone } from "@/components/status-dot"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"

/** 授权状态 → 状态点色调与文案 key。 */
// as const 保住字面量：i18n 的类型增强只认字面 key，退化成 string 就漏检了。
const TONES = {
  authorized: { tone: "success", key: "settingsPage.notifications.authorized" },
  provisional: { tone: "warning", key: "settingsPage.notifications.provisional" },
  denied: { tone: "destructive", key: "settingsPage.notifications.denied" },
  notDetermined: { tone: "muted", key: "settingsPage.notifications.notDetermined" },
  unknown: { tone: "muted", key: "settingsPage.notifications.unknown" },
} as const satisfies Record<NotifyStatus["status"], { tone: StatusTone; key: string }>

/**
 * 系统通知权限。只在 macOS 壳里出现——它管的是这台机器的通知中心，浏览器
 * （含局域网访客）里没有这回事，那边走页内提示。
 *
 * 为什么要把状态摊开显示，而不是做成一个开关：macOS 的通知授权是**单向**
 * 的。只有「还没问过」时代码才能弹出系统对话框；一旦用户点了不允许，之后
 * 再怎么请求都只会立刻返回失败、连弹窗都不出现——那时唯一的出路是把人送
 * 去系统设置。做成开关会骗人：拨过去没反应，用户只会以为是 bug。
 */
export function NotificationsCard() {
  const { t } = useTranslation()
  const [status, setStatus] = useState<NotifyStatus | null>(null)
  const [busy, setBusy] = useState(false)

  // 进页面拉一次。首次加载不设 busy——那一刻整张卡还是骨架屏，
  // 「按钮转不转」没人看得见。
  useEffect(() => {
    if (!isDesktop()) return
    let alive = true
    desktopNotify
      .status()
      .then((s) => alive && setStatus(s))
      .catch(() => alive && setStatus(null))
    return () => {
      alive = false
    }
  }, [])

  if (!isDesktop()) return null
  if (status === null) return <Skeleton className="h-48 w-full" />

  const run = (task: Promise<NotifyStatus>) => {
    setBusy(true)
    task
      .then(setStatus)
      .catch(() => undefined)
      .finally(() => setBusy(false))
  }

  const check = () => run(desktopNotify.status())
  const request = () => run(desktopNotify.request())

  const { tone, key } = TONES[status.status]

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <BellIcon className="size-4" />
          {t("settingsPage.notifications.title")}
        </CardTitle>
        <CardDescription>
          {t("settingsPage.notifications.description")}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="flex items-center justify-between gap-4">
          <StatusDot tone={tone} label={t(key)} />
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="ghost"
              disabled={busy}
              onClick={check}
              data-icon="inline-start"
            >
              <RefreshCwIcon className="size-4" />
              {t("settingsPage.notifications.recheck")}
            </Button>
            {status.canRequest ? (
              <Button size="sm" disabled={busy} onClick={request}>
                {t("settingsPage.notifications.enable")}
              </Button>
            ) : status.status !== "authorized" ? (
              <Button
                size="sm"
                variant="outline"
                onClick={() => void desktopNotify.openSettings()}
                data-icon="inline-start"
              >
                <ExternalLinkIcon className="size-4" />
                {t("settingsPage.notifications.openSettings")}
              </Button>
            ) : null}
          </div>
        </div>

        {/* app 不在「应用程序」目录时授权会静默失败：请求直接返回错误、连
            系统弹窗都不出现，状态停在「尚未授权」。这一条必须先说，否则
            用户会一直点「开启通知」然后一直以为是 bug。 */}
        {!status.inApplicationsDir ? (
          <Alert variant="destructive">
            <AlertTitle>{t("settingsPage.notifications.notInApps")}</AlertTitle>
            <AlertDescription>
              <span>{t("settingsPage.notifications.notInAppsDesc")}</span>
              <span className="font-mono text-xs break-all">
                {status.bundlePath}
              </span>
            </AlertDescription>
          </Alert>
        ) : status.status === "denied" ? (
          <Alert>
            <AlertTitle>{t("settingsPage.notifications.deniedTitle")}</AlertTitle>
            <AlertDescription>
              {t("settingsPage.notifications.deniedDesc")}
            </AlertDescription>
          </Alert>
        ) : null}

        {status.error ? (
          <p className="text-xs text-muted-foreground">{status.error}</p>
        ) : null}
      </CardContent>
    </Card>
  )
}
