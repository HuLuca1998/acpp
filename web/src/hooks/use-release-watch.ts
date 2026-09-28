import { useEffect, useState } from "react"

import { api } from "@/lib/api"
import { dismissNotice, pushNotice } from "@/lib/notify/store"

/** 设置页「关于与更新」分区的深链：通知卡与用户菜单都跳这里。 */
export const RELEASE_SETTINGS_PATH = "/settings?section=about"

/**
 * 重查间隔。后端对 GitHub Releases 有自己的缓存与每日自查，这里问的是
 * 那份缓存，不打外网——一小时一次只为让一直开着的页面也能等到新版本。
 */
const RECHECK_MS = 60 * 60 * 1000

/** 本页已经推过通知的版本：用户划掉后，同一版本不再推回来。 */
let pushedVersion: string | null = null

/**
 * 新版本哨兵：GitHub 上有了比当前更新的 release 就报出版本号（没有时为 null）。
 *
 * 与 useVersionWatch 是两件事：那个管「后端已经换了，你这页是旧的，刷新」，
 * 这个管「有新版本还没装，去设置里点更新」。
 *
 * 提示走通知中心（web/AGENTS.md §5.5：有事等人处理的不用 toast）：置顶的
 * release 卡是主入口，用户菜单的状态点与「前往更新」项是卡被划掉后的常驻
 * 兜底。两处点下去都进设置的更新分区。
 *
 * 只有 owner 能装更新，租户不问也不提示（接口本来就是 owner-only）。
 */
export function useReleaseWatch(enabled: boolean): string | null {
  const [latest, setLatest] = useState<string | null>(null)

  useEffect(() => {
    if (!enabled) return
    let cancelled = false

    const check = () => {
      api.system
        .update()
        .then((info) => {
          if (cancelled) return
          const version = info.hasUpdate ? (info.latestVersion ?? null) : null
          setLatest(version)
          if (!version) {
            pushedVersion = null
            dismissNotice("release")
            return
          }
          if (pushedVersion === version) return
          pushedVersion = version
          pushNotice({
            id: "release",
            kind: "release",
            text: `v${version}`,
            at: Date.now(),
          })
        })
        .catch(() => {
          // 查不到（离线、GitHub 限流）就当没有：下一轮再说，不打扰人。
        })
    }

    check()
    const timer = setInterval(check, RECHECK_MS)
    return () => {
      cancelled = true
      clearInterval(timer)
    }
  }, [enabled])

  return enabled ? latest : null
}
