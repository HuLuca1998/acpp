import * as React from "react"

import { IdentityContext } from "@/hooks/identity-context"
import { api } from "@/lib/api"
import type { Identity } from "@/types/acp"

/**
 * 身份提供者（adr-007）。启动做两件事：兑换邀请链接、问后端我是谁。
 *
 * 凭证是 HttpOnly cookie，前端拿不到也不需要拿——SSE 与终端 WebSocket
 * 都带不了自定义 header，cookie 是三条通道唯一的共同载体。
 */
export function IdentityProvider({ children }: { children: React.ReactNode }) {
  const [identity, setIdentity] = React.useState<Identity | null>(null)
  const [loading, setLoading] = React.useState(true)
  const [error, setError] = React.useState<string | null>(null)

  const load = React.useCallback(async () => {
    try {
      setIdentity(await api.auth.me())
      setError(null)
    } catch (err) {
      setError((err as Error).message)
    }
  }, [])

  React.useEffect(() => {
    let cancelled = false

    const bootstrap = async () => {
      const params = new URLSearchParams(window.location.search)
      const invite = params.get("invite")

      if (invite) {
        // 先把 token 从地址栏抹掉再兑换：留在 URL 里迟早会被人连着链接
        // 一起转发出去，而这串东西等同于这个租户的全部会话。
        params.delete("invite")
        const query = params.toString()
        window.history.replaceState(
          {},
          "",
          `${window.location.pathname}${query ? `?${query}` : ""}${window.location.hash}`
        )
        try {
          const me = await api.auth.redeem(invite)
          if (!cancelled) {
            setIdentity(me)
            setLoading(false)
          }
          return
        } catch {
          // 兑换失败（链接无效、或这个租户已被关停）交给 me 给出准确状态：
          // 后端在关停时同样种了 cookie，me 会回 revoked。
        }
      }

      await load()
      if (!cancelled) setLoading(false)
    }

    void bootstrap()
    return () => {
      cancelled = true
    }
  }, [load])

  const value = React.useMemo(
    () => ({ identity, loading, error, refresh: load }),
    [identity, loading, error, load]
  )

  return (
    <IdentityContext.Provider value={value}>
      {children}
    </IdentityContext.Provider>
  )
}
