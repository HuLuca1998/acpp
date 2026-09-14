import { useSyncExternalStore } from "react"
import { useTranslation } from "react-i18next"

import {
  getMascotPrefs,
  saveMascotPrefs,
  subscribeMascotPrefs,
} from "@/lib/chat/mascot-prefs"
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
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"

/**
 * 吉祥物偏好。
 *
 * 「能关掉」是这块面板存在的首要理由：输入卡上那只球 56px、会动、会说话，
 * 对不喜欢它的人就是纯粹的干扰，没有开关的角色是个负担。安静模式是中间档
 * ——留着表情和目光当状态指示，但不说话、不转圈、不撒花。
 *
 * 订阅走模块级广播（不是 useState 读一次）：输入卡上右键菜单也能改这几项，
 * 两处得立刻对上。
 */
export function MascotPrefsCard() {
  const { t } = useTranslation()
  const prefs = useSyncExternalStore(subscribeMascotPrefs, getMascotPrefs)

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">
          {t("chat.mascot.prefs.title")}
        </CardTitle>
        <CardDescription>{t("chat.mascot.prefs.desc")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0">
            <Label htmlFor="mascot-enabled" className="text-sm font-medium">
              {t("chat.mascot.prefs.enabled")}
            </Label>
            <p className="text-xs text-muted-foreground">
              {t("chat.mascot.prefs.enabledDesc")}
            </p>
          </div>
          <Switch
            id="mascot-enabled"
            checked={prefs.enabled}
            onCheckedChange={(enabled) => saveMascotPrefs({ enabled })}
          />
        </div>

        {/* 关掉之后下面两项没有意义，整段收起来别留死控件。 */}
        {prefs.enabled ? (
          <>
            <Separator />
            <div className="flex items-start justify-between gap-4">
              <div className="min-w-0">
                <Label htmlFor="mascot-quiet" className="text-sm font-medium">
                  {t("chat.mascot.prefs.quiet")}
                </Label>
                <p className="text-xs text-muted-foreground">
                  {t("chat.mascot.prefs.quietDesc")}
                </p>
              </div>
              <Switch
                id="mascot-quiet"
                checked={prefs.quiet}
                onCheckedChange={(quiet) => saveMascotPrefs({ quiet })}
              />
            </div>

            <Separator />
            <div className="flex items-center justify-between gap-4">
              <Label className="text-sm font-medium">
                {t("chat.mascot.prefs.side")}
              </Label>
              <ToggleGroup
                value={[prefs.side]}
                onValueChange={(value) => {
                  const side = value[0]
                  if (side === "left" || side === "right") {
                    saveMascotPrefs({ side })
                  }
                }}
                className="shrink-0"
              >
                <ToggleGroupItem value="left">
                  {t("chat.mascot.prefs.sideLeft")}
                </ToggleGroupItem>
                <ToggleGroupItem value="right">
                  {t("chat.mascot.prefs.sideRight")}
                </ToggleGroupItem>
              </ToggleGroup>
            </div>
          </>
        ) : null}
      </CardContent>
    </Card>
  )
}
