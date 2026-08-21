import { useState } from "react"
import { useTranslation } from "react-i18next"
import { BellIcon, BellOffIcon } from "lucide-react"

import { isDesktop } from "@/lib/desktop"
import {
  loadNotifyPrefs,
  saveNotifyPrefs,
  type NotifyPrefs,
} from "@/lib/notify/prefs"
import { Hint } from "@/components/hint"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"

/** 开关清单：key 对应偏好字段，desc 说明「这类事什么时候发生」。 */
const SWITCHES = [
  { key: "decisions", label: "notify.prefs.decisions", desc: "notify.prefs.decisionsDesc" },
  { key: "results", label: "notify.prefs.results", desc: "notify.prefs.resultsDesc" },
  { key: "errors", label: "notify.prefs.errors", desc: "notify.prefs.errorsDesc" },
] as const

/**
 * 顶栏的通知开关。
 *
 * 为什么不放进设置页：设置页是 owner 专属（adr-007），而局域网访客同样会
 * 被通知打扰，得有地方关。顶栏是两种身份都够得着的唯一位置。
 * 设置页里那张卡管的是另一件事——macOS 的系统授权，那本来就只有壳里有。
 */
export function NotifyMenu() {
  const { t } = useTranslation()
  const [prefs, setPrefs] = useState<NotifyPrefs>(loadNotifyPrefs)

  const toggle = (key: keyof NotifyPrefs) => {
    const next = { ...prefs, [key]: !prefs[key] }
    setPrefs(next)
    saveNotifyPrefs(next)
  }

  // 全关了就把铃铛划掉：自己关过什么必须一眼看得见，否则下次奇怪
  // 「怎么没通知」的时候无从查起。
  const muted = !prefs.decisions && !prefs.results && !prefs.errors

  return (
    <Popover>
      <Hint label={t("notify.prefs.title")}>
        <PopoverTrigger
          render={
            <Button
              variant="ghost"
              size="icon"
              className="size-8"
              aria-label={t("notify.prefs.title")}
            />
          }
        >
          {muted ? (
            <BellOffIcon className="size-4 text-muted-foreground" />
          ) : (
            <BellIcon className="size-4" />
          )}
        </PopoverTrigger>
      </Hint>
      <PopoverContent align="end" className="w-72">
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-1">
            <p className="text-sm font-medium">{t("notify.prefs.title")}</p>
            <p className="text-xs text-muted-foreground">
              {isDesktop()
                ? t("notify.prefs.descDesktop")
                : t("notify.prefs.descBrowser")}
            </p>
          </div>

          {SWITCHES.map(({ key, label, desc }) => (
            <div key={key} className="flex items-start justify-between gap-4">
              <div className="flex flex-col gap-0.5">
                <Label htmlFor={`notify-${key}`} className="text-sm">
                  {t(label)}
                </Label>
                <span className="text-xs text-muted-foreground">{t(desc)}</span>
              </div>
              <Switch
                id={`notify-${key}`}
                checked={prefs[key]}
                onCheckedChange={() => toggle(key)}
              />
            </div>
          ))}

          {/* 提示音只在浏览器这一侧归我们管：系统通知的声音由 macOS
              通知设置控制，这里再放一个开关只会两处打架。 */}
          {!isDesktop() ? (
            <>
              <Separator />
              <div className="flex items-start justify-between gap-4">
                <div className="flex flex-col gap-0.5">
                  <Label htmlFor="notify-sound" className="text-sm">
                    {t("notify.prefs.sound")}
                  </Label>
                  <span className="text-xs text-muted-foreground">
                    {t("notify.prefs.soundDesc")}
                  </span>
                </div>
                <Switch
                  id="notify-sound"
                  checked={prefs.sound}
                  onCheckedChange={() => toggle("sound")}
                />
              </div>
            </>
          ) : null}
        </div>
      </PopoverContent>
    </Popover>
  )
}
