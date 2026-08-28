import { useState } from "react"
import { useTranslation } from "react-i18next"

import { isDesktop } from "@/lib/desktop"
import {
  loadNotifyPrefs,
  saveNotifyPrefs,
  type NotifyPrefs,
} from "@/lib/notify/prefs"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Label } from "@/components/ui/label"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"

const SWITCHES = [
  {
    key: "decisions",
    label: "notify.prefs.decisions",
    desc: "notify.prefs.decisionsDesc",
  },
  {
    key: "results",
    label: "notify.prefs.results",
    desc: "notify.prefs.resultsDesc",
  },
  {
    key: "errors",
    label: "notify.prefs.errors",
    desc: "notify.prefs.errorsDesc",
  },
] as const

/**
 * 通知偏好：哪几类事值得打断人。
 *
 * 原本挂在顶栏铃铛的下拉里，但那颗铃铛现在是「通知中心」的入口——一个按钮
 * 不该既是收件箱又是设置面板。偏好是低频动作，归设置页；铃铛只管看有什么事。
 */
export function NotifyPrefsCard() {
  const { t } = useTranslation()
  const [prefs, setPrefs] = useState<NotifyPrefs>(loadNotifyPrefs)

  const toggle = (key: keyof NotifyPrefs) => {
    const next = { ...prefs, [key]: !prefs[key] }
    setPrefs(next)
    saveNotifyPrefs(next)
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("notify.prefs.title")}</CardTitle>
        <CardDescription>{t("notify.prefs.description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {SWITCHES.map(({ key, label, desc }) => (
          <div key={key} className="flex items-start justify-between gap-4">
            <div className="min-w-0">
              <Label htmlFor={`notify-${key}`} className="text-sm font-medium">
                {t(label)}
              </Label>
              <p className="text-xs text-muted-foreground">{t(desc)}</p>
            </div>
            <Switch
              id={`notify-${key}`}
              checked={prefs[key]}
              onCheckedChange={() => toggle(key)}
            />
          </div>
        ))}

        {/* 提示音只在浏览器这一侧归我们管：系统通知的声音由 macOS 通知设置
            控制，这里再放一个开关只会两处打架。 */}
        {!isDesktop() ? (
          <>
            <Separator />
            <div className="flex items-start justify-between gap-4">
              <div className="min-w-0">
                <Label htmlFor="notify-sound" className="text-sm font-medium">
                  {t("notify.prefs.sound")}
                </Label>
                <p className="text-xs text-muted-foreground">
                  {t("notify.prefs.soundDesc")}
                </p>
              </div>
              <Switch
                id="notify-sound"
                checked={prefs.sound}
                onCheckedChange={() => toggle("sound")}
              />
            </div>
          </>
        ) : null}
      </CardContent>
    </Card>
  )
}
