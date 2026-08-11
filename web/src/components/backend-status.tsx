import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"

import { api } from "@/lib/api"
import { StatusDot } from "@/components/status-dot"

type Health = { status: "ok"; version: string } | { status: "down" } | null

/**
 * 侧栏底部的后端连接状态：替代模板自带的假用户卡。
 * 本地控制台没有账号概念，底部最有价值的信息是「后端还活着吗」。
 */
export function BackendStatus() {
  const { t } = useTranslation()
  const [health, setHealth] = useState<Health>(null)

  useEffect(() => {
    let cancelled = false
    api
      .health()
      .then((res) => {
        if (!cancelled) setHealth({ status: "ok", version: res.version })
      })
      .catch(() => {
        if (!cancelled) setHealth({ status: "down" })
      })
    return () => {
      cancelled = true
    }
  }, [])

  if (health === null) return null

  return (
    <div className="flex items-center justify-between gap-2 rounded-lg px-2 py-1.5 text-xs text-muted-foreground">
      {health.status === "ok" ? (
        <>
          <StatusDot tone="success" pulse label={t("backend.connected")} />
          <span className="font-mono">v{health.version}</span>
        </>
      ) : (
        <StatusDot tone="destructive" label={t("backend.unreachable")} />
      )}
    </div>
  )
}
