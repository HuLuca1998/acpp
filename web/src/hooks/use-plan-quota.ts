import { useCallback, useEffect, useRef, useState } from "react"

import { api } from "@/lib/api"
import type { QuotaFlavor } from "@/lib/chat/usage"
import type { PlanQuota } from "@/types/system"

/**
 * 前端侧的复用窗口。后端本身缓存一分钟（claude 那条路每次要拉一个 CLI
 * 进程），这里再存一份只是为了面板反复开合时不闪骨架屏——弹层内容关了
 * 就卸载，没有这层每次点开都是空的。
 */
const FRESH_MS = 60_000

const cache = new Map<QuotaFlavor, { quota: PlanQuota; at: number }>()
/** 同一方言在途的请求只发一次：两个面板同时点开不该打两遍。 */
const inflight = new Map<string, Promise<PlanQuota>>()

function fetchQuota(flavor: QuotaFlavor, refresh: boolean): Promise<PlanQuota> {
  const key = `${flavor}:${refresh ? "refresh" : "cached"}`
  let pending = inflight.get(key)
  if (!pending) {
    pending = api.system
      .quota(flavor, refresh)
      .then((quota) => {
        cache.set(flavor, { quota, at: Date.now() })
        return quota
      })
      .finally(() => inflight.delete(key))
    inflight.set(key, pending)
  }
  return pending
}

/**
 * 套餐用量：某个方言账号在订阅上的限额水位。挂载即拉（缓存新鲜就不拉），
 * refresh 绕过前后端两层缓存重取。调用方按 flavor 加 key，换方言即重挂。
 *
 * 没有单独的 loading：首次拉取时 quota 与 error 都空，界面据此画骨架；
 * refreshing 只在手动重取期间为真（已有数据在屏上，只是按钮转圈）。
 */
export function usePlanQuota(flavor: QuotaFlavor) {
  const [quota, setQuota] = useState<PlanQuota | null>(
    () => cache.get(flavor)?.quota ?? null
  )
  const [error, setError] = useState<string | null>(null)
  const [refreshing, setRefreshing] = useState(false)
  // 弹层关闭即卸载，飞行中的请求回来时别再往已卸载的组件里写。
  const alive = useRef(true)

  const run = useCallback(
    (refresh: boolean) =>
      fetchQuota(flavor, refresh).then(
        (next) => {
          if (!alive.current) return
          setQuota(next)
          setError(null)
        },
        (err: unknown) => {
          if (!alive.current) return
          setError(err instanceof Error ? err.message : String(err))
        }
      ),
    [flavor]
  )

  useEffect(() => {
    alive.current = true
    const hit = cache.get(flavor)
    if (!hit || Date.now() - hit.at >= FRESH_MS) void run(false)
    return () => {
      alive.current = false
    }
  }, [flavor, run])

  const refresh = useCallback(() => {
    setRefreshing(true)
    return run(true).finally(() => {
      if (alive.current) setRefreshing(false)
    })
  }, [run])

  return { quota, error, refreshing, refresh }
}
