import { useEffect, useRef } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { api } from "@/lib/api"

/** 版本轮询间隔。更新是低频事件，一分钟一次足够，也不值得更密。 */
const POLL_MS = 60_000

/**
 * 版本哨兵：后端换了版本就提示刷新。
 *
 * 为什么需要它：owner 在设置页点的「一键更新」会替换 .app 并重启后端，
 * 但**别人的浏览器不会自己刷新**——局域网访客手里那一页仍是旧前端，接着
 * 用下去就是旧界面打新后端，出问题也看不出所以然。
 *
 * 为什么只提示不强制刷新：刷新会丢掉输入框里没发出去的草稿，而更新随时
 * 可能发生。会话状态在后端、刷新即恢复，唯独没发出去的那段话找不回来，
 * 所以把「什么时候刷」交给用户，提示条挂着不消失（duration: Infinity）。
 *
 * 探测走 `/api/health` 而不是 SSE：没开会话的页面（列表页、设置页）也要
 * 能发现更新，那些页面上没有事件流。后端重启期间请求会失败，忽略即可,
 * 下一轮自然拿到新版本。
 */
export function useVersionWatch() {
  const { t } = useTranslation()
  // 基线是「这一页启动时后端的版本」，不是最新版本：变了就说明后端换过。
  const baseline = useRef<string | null>(null)
  const notified = useRef(false)

  useEffect(() => {
    let cancelled = false

    const check = async () => {
      try {
        const res = await api.health()
        if (cancelled || !res.version) return
        if (baseline.current === null) {
          baseline.current = res.version
          return
        }
        if (res.version === baseline.current || notified.current) return
        notified.current = true
        toast(t("backend.updated", { version: res.version }), {
          description: t("backend.updatedDesc"),
          duration: Infinity,
          action: {
            label: t("backend.reload"),
            onClick: () => window.location.reload(),
          },
        })
      } catch {
        // 后端正在重启，下一轮再看。
      }
    }

    void check()
    const timer = setInterval(() => void check(), POLL_MS)
    // 切回这个标签页时立刻查一次：更新往往发生在页面被晾在后台的时候，
    // 等下一次轮询才发现就白等了大半分钟。
    const onVisible = () => {
      if (document.visibilityState === "visible") void check()
    }
    document.addEventListener("visibilitychange", onVisible)

    return () => {
      cancelled = true
      clearInterval(timer)
      document.removeEventListener("visibilitychange", onVisible)
    }
  }, [t])
}
