import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { RefreshCwIcon } from "lucide-react"

import { api } from "@/lib/api"
import { useVersionWatch } from "@/hooks/use-version-watch"
import { StatusDot } from "@/components/status-dot"
import { Button } from "@/components/ui/button"

type Health = { status: "ok"; version: string } | { status: "down" } | null

/**
 * 侧栏底部的后端连接状态：替代模板自带的假用户卡。
 * 本地控制台没有账号概念，底部最有价值的信息是「后端还活着吗」。
 *
 * 后端换版本后这里就是更新入口：版本号本来就写在这儿，「你手上是旧版本」
 * 挨着它说才有上下文，比在角落弹一条会消失的提示条更容易被看见——尤其对
 * 局域网访客，他们不知道 owner 刚点过更新。
 */
export function BackendStatus() {
  const { t } = useTranslation()
  const [health, setHealth] = useState<Health>(null)
  const updated = useVersionWatch()

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

  // 有更新时整行让位给它：这一页已经是旧的，连没连上暂时不是重点。
  if (updated) {
    return (
      <div className="flex items-center justify-between gap-2 rounded-lg px-2 py-1.5 text-xs">
        <StatusDot tone="warning" label={t("backend.updateAvailable")} />
        <Button
          size="sm"
          variant="ghost"
          className="h-6 gap-1 px-2 text-xs text-warning hover:text-warning"
          onClick={() => window.location.reload()}
        >
          <RefreshCwIcon className="size-3" />
          {t("backend.reload")}
          <span className="font-mono tabular-nums">v{updated}</span>
        </Button>
      </div>
    )
  }

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
