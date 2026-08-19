import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { api } from "@/lib/api"
import { formatDateTime } from "@/lib/format"
import { Alert, AlertDescription } from "@/components/ui/alert"
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
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { useAsyncData } from "@/hooks/use-async-data"
import { DownloadIcon, RefreshCwIcon } from "lucide-react"

/**
 * 关于与更新分区：当前版本、GitHub Releases 检查（后端每日自查缓存）、
 * 一键更新（下载 → 替换 .app → 自动重启；仅桌面版）。
 */
export function AboutUpdate() {
  const { t, i18n } = useTranslation()
  const {
    data: info,
    error,
    setData,
  } = useAsyncData(() => api.system.update(), [])
  const [checking, setChecking] = useState(false)
  const [applying, setApplying] = useState(false)
  const [restarting, setRestarting] = useState<string | null>(null)
  // 有会话正在生成回复时后端会把更新拦下来，这里存计数弹确认框。
  const [busyTurns, setBusyTurns] = useState<number | null>(null)

  async function check() {
    setChecking(true)
    try {
      setData(await api.system.update(true))
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setChecking(false)
    }
  }

  async function apply(force = false) {
    setApplying(true)
    try {
      const res = await api.system.updateApply(force)
      if (!res.applied) {
        // 更新会杀掉全部 agent 子进程，正在跑的轮会拿不到回复——
        // 停下来让用户拍板，确认后带 force 重发。
        setBusyTurns(res.runningTurns ?? 0)
        setApplying(false)
        return
      }
      // 后端随即杀壳重启，这个页面马上会失联——把结果钉在界面上。
      setRestarting(res.message ?? "")
    } catch (err) {
      toast.error((err as Error).message)
      setApplying(false)
    }
  }

  if (restarting) {
    return (
      <Alert>
        <AlertDescription className="flex items-center gap-2">
          <Spinner className="size-4" />
          {restarting}
        </AlertDescription>
      </Alert>
    )
  }

  if (!info) {
    return (
      <div className="flex flex-col gap-4">
        {error ? (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : (
          <>
            <Skeleton className="h-28 w-full" />
            <Skeleton className="h-40 w-full" />
          </>
        )}
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      {/* 关于：身份与版本。 */}
      <Card>
        <CardHeader>
          <div className="flex items-center gap-3">
            <img src="/app-icon.svg" alt="" className="size-10 shrink-0" />
            <div className="flex min-w-0 flex-col">
              <CardTitle>ACPP</CardTitle>
              <CardDescription className="font-mono">
                v{info.currentVersion}
              </CardDescription>
            </div>
          </div>
        </CardHeader>
        <CardContent className="text-xs text-muted-foreground">
          <a
            href={`https://github.com/${info.repo}`}
            target="_blank"
            rel="noreferrer"
            className="font-mono underline-offset-4 hover:underline"
          >
            github.com/{info.repo}
          </a>
        </CardContent>
      </Card>

      {/* 更新：检查结果 + 一键更新。 */}
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <CardTitle className="text-base">
              {t("settingsPage.about.updateTitle")}
            </CardTitle>
            {info.hasUpdate ? (
              <Badge>
                {t("settingsPage.about.newVersion", {
                  version: info.latestVersion ?? "",
                })}
              </Badge>
            ) : null}
            <Button
              size="sm"
              variant="outline"
              className="ml-auto"
              disabled={checking || applying}
              onClick={() => void check()}
            >
              {checking ? (
                <Spinner className="size-3.5" data-icon="inline-start" />
              ) : (
                <RefreshCwIcon data-icon="inline-start" />
              )}
              {t("settingsPage.about.check")}
            </Button>
          </div>
          <CardDescription>
            {info.checkedAt
              ? t("settingsPage.about.checkedAt", {
                  time: formatDateTime(info.checkedAt, i18n.language),
                })
              : t("settingsPage.about.neverChecked")}
            {" · "}
            {t("settingsPage.about.autoCheckHint")}
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {info.checkError ? (
            <Alert>
              <AlertDescription>{info.checkError}</AlertDescription>
            </Alert>
          ) : info.hasUpdate ? (
            <>
              <div className="flex items-center gap-2 text-sm">
                <span className="font-mono">v{info.currentVersion}</span>
                <span className="text-muted-foreground">→</span>
                <span className="font-mono font-medium">
                  v{info.latestVersion}
                </span>
                {info.publishedAt ? (
                  <span className="text-xs text-muted-foreground">
                    {formatDateTime(info.publishedAt, i18n.language)}
                  </span>
                ) : null}
              </div>
              {info.notes ? (
                <pre className="max-h-64 overflow-auto rounded-lg border border-border bg-muted/40 p-3 text-xs whitespace-pre-wrap">
                  {info.notes}
                </pre>
              ) : null}
              {info.canApply ? (
                <Button
                  className="self-start"
                  disabled={applying}
                  onClick={() => void apply()}
                >
                  {applying ? (
                    <Spinner className="size-3.5" data-icon="inline-start" />
                  ) : (
                    <DownloadIcon data-icon="inline-start" />
                  )}
                  {applying
                    ? t("settingsPage.about.applying")
                    : t("settingsPage.about.apply")}
                </Button>
              ) : (
                <p className="text-xs text-muted-foreground">
                  {t("settingsPage.about.devHint")}
                </p>
              )}
            </>
          ) : (
            <p className="text-sm text-muted-foreground">
              {t("settingsPage.about.upToDate")}
            </p>
          )}
        </CardContent>
      </Card>

      <AlertDialog
        open={busyTurns !== null}
        onOpenChange={(open) => !open && setBusyTurns(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t("settingsPage.about.busyTitle")}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t("settingsPage.about.busyDescription", {
                count: busyTurns ?? 0,
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                setBusyTurns(null)
                void apply(true)
              }}
            >
              {t("settingsPage.about.busyConfirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
