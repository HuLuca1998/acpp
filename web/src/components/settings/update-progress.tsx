import { useTranslation } from "react-i18next"
import { RotateCcwIcon } from "lucide-react"

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Progress } from "@/components/ui/progress"
import { Spinner } from "@/components/ui/spinner"
import { formatBytes, formatDuration } from "@/lib/format"
import { isUpdateActive } from "@/lib/desktop"
import type { UpdateProgress } from "@/types/system"

/**
 * 一键更新的进度卡：下载阶段给真进度（字节、速度、剩余时间），解包与
 * 安装没有字节数可数就给不定态条，重启与失败各有终态。失败可原地重试。
 */
export function UpdateProgressCard({
  progress,
  onRetry,
  onDismiss,
}: {
  progress: UpdateProgress
  onRetry: () => void
  onDismiss: () => void
}) {
  const { t } = useTranslation()
  const p = progress
  const busy = isUpdateActive(p.phase) || p.phase === "restarting"

  const title = (() => {
    switch (p.phase) {
      case "downloading":
        return t("settingsPage.about.progress.downloading", {
          version: p.version ?? "",
        })
      case "unpacking":
        return t("settingsPage.about.progress.unpacking")
      case "installing":
        return t("settingsPage.about.progress.installing")
      case "restarting":
        return t("settingsPage.about.progress.restarting")
      case "failed":
        return t("settingsPage.about.progress.failed")
      default:
        return t("settingsPage.about.progress.done")
    }
  })()

  // 下载阶段按字节算百分比；服务端没给总长时走不定态。解包 / 安装没有
  // 字节数，也是不定态；终态铺满。
  const value =
    p.phase === "downloading"
      ? p.total > 0
        ? Math.min(100, (p.downloaded / p.total) * 100)
        : null
      : p.phase === "unpacking" || p.phase === "installing"
        ? null
        : 100

  const stats = (() => {
    if (p.phase !== "downloading") return null
    const done = formatBytes(p.downloaded)
    const speed = formatBytes(Math.max(0, p.speed))
    if (p.total <= 0)
      return t("settingsPage.about.progress.statsUnknown", { done, speed })
    const total = formatBytes(p.total)
    if (p.speed <= 0)
      return t("settingsPage.about.progress.statsNoEta", { done, total, speed })
    const etaMs = ((p.total - p.downloaded) / p.speed) * 1000
    return t("settingsPage.about.progress.stats", {
      done,
      total,
      speed,
      eta: formatDuration(etaMs),
    })
  })()

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          {busy ? <Spinner className="size-4" /> : null}
          <span className={busy ? "text-shimmer" : undefined}>{title}</span>
        </CardTitle>
        {p.message && p.phase !== "failed" ? (
          <CardDescription>{p.message}</CardDescription>
        ) : null}
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {p.phase !== "failed" ? (
          <Progress value={value} aria-label={title} />
        ) : null}
        {stats ? (
          <p className="text-xs text-muted-foreground tabular-nums">{stats}</p>
        ) : null}
        {p.phase === "failed" ? (
          <>
            <Alert variant="destructive">
              <AlertDescription>{p.error}</AlertDescription>
            </Alert>
            <div className="flex gap-2">
              <Button size="sm" onClick={onRetry}>
                <RotateCcwIcon data-icon="inline-start" />
                {t("settingsPage.about.progress.retry")}
              </Button>
              <Button size="sm" variant="ghost" onClick={onDismiss}>
                {t("settingsPage.about.progress.dismiss")}
              </Button>
            </div>
          </>
        ) : null}
      </CardContent>
    </Card>
  )
}
