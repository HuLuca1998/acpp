import { useEffect, useRef } from "react"

import type { WorkspaceScopeApi } from "@/lib/api"

/**
 * 两次刷新之间至少隔这么久。后端已经把成串的文件事件合成一声，这里再兜
 * 一层：连续保存、一次构建的产物落盘，仍可能几百毫秒来一声，而每一声对
 * 前端都是一次 git 汇总 + 一轮面板重读。
 */
const MIN_INTERVAL_MS = 1000

/**
 * 订阅工作目录的文件变动，有动静就回调（已节流）。
 *
 * 工作区面板原本只在 agent 干完一件事之后重读；用户自己在编辑器里改的、
 * 命令行里跑出来的，界面一概不知道。这条流补上那一半——改动来自谁都算数。
 *
 * 两条自我克制：
 *  - **页面在后台就断开**。没人看的标签页没有理由占着一条长连接，更没有
 *    理由让后端为它守着一棵树的文件句柄；回到前台重新连上，那一刻自然会
 *    刷一次。
 *  - **后端说不监视就不再重连**（树太大，依赖与产物没排干净）。EventSource
 *    默认会一直重试，那只会变成每几秒一次的无用连接。
 */
export function useWorkspaceWatch(
  scope: WorkspaceScopeApi,
  sessionId: number,
  /** 工作区数据面可用吗（草稿态没选目录时不成立）。 */
  enabled: boolean,
  onChange: () => void
): void {
  // 回调走 ref：它多半是每次渲染新建的闭包，进依赖就等于每渲染一次
  // 重连一次。
  const onChangeRef = useRef(onChange)
  useEffect(() => {
    onChangeRef.current = onChange
  }, [onChange])

  useEffect(() => {
    if (!enabled) return
    let source: EventSource | null = null
    let timer: ReturnType<typeof setTimeout> | null = null
    let last = 0
    // 后端明确说了这棵树不监视：别再连。
    let givenUp = false

    const close = () => {
      source?.close()
      source = null
    }

    const fire = () => {
      // 已经排了一次就让它去跑，不叠加——「变了」不累加，最新那一声
      // 就代表全部。
      if (timer) return
      const wait = Math.max(0, MIN_INTERVAL_MS - (Date.now() - last))
      timer = setTimeout(() => {
        timer = null
        last = Date.now()
        onChangeRef.current()
      }, wait)
    }

    const open = () => {
      if (givenUp || source) return
      const es = new EventSource(scope.workspaceWatchUrl(sessionId))
      source = es
      es.onmessage = (e) => {
        let kind: string
        try {
          kind = (JSON.parse(e.data) as { kind?: string }).kind ?? ""
        } catch {
          // 半条 JSON 没有意义，丢掉即可。
          return
        }
        if (kind === "unavailable") {
          givenUp = true
          close()
          return
        }
        if (kind === "fs_changed") fire()
      }
    }

    const sync = () => {
      if (document.hidden) close()
      else open()
    }

    sync()
    document.addEventListener("visibilitychange", sync)
    return () => {
      document.removeEventListener("visibilitychange", sync)
      if (timer) clearTimeout(timer)
      close()
    }
  }, [scope, sessionId, enabled])
}
