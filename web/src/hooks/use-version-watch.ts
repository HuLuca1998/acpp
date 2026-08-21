import { useEffect, useRef } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { api } from "@/lib/api"

/**
 * 浏览器彻底放弃后的自建重连延迟，逐次翻倍到上限。EventSource 只对网络层
 * 中断自动重连，撞上 HTTP 错误状态会直接 CLOSED 再也不回来——那种情况得
 * 我们接手。退避是必要的：走到这里最常见的原因不是后端在重启（那是网络层
 * 中断，浏览器自己会重连），而是这条流被拒了（比如租客的访问已被 owner
 * 关停），死磕只会变成一秒一次的无用请求。
 */
const RECONNECT_MS = 3_000
const RECONNECT_MAX_MS = 30_000

/**
 * 版本哨兵：后端换了版本就提示刷新。
 *
 * 为什么需要它：owner 在设置页点的「一键更新」会替换 .app 并重启后端，
 * 但**别人的浏览器不会自己刷新**——局域网访客手里那一页仍是旧前端，接着
 * 用下去就是旧界面打新后端，接口契约一变就出错，出了错也看不出所以然。
 *
 * 为什么用长连接而不是轮询：及时性全看「多久发现后端换了」，而轮询的发现
 * 延迟下限就是轮询间隔——要做到秒级，就得让局域网里每个页面每秒打一次
 * health。长连接反过来利用了更新的本质：**进程被换掉，这条流必断**，断开
 * 本身就是信号。浏览器自动重连，重连拿到的 hello 版本对不上就说明更新过。
 * 平时零请求，发现延迟只是后端起来后的一次重连（间隔由服务端 retry 指定）。
 *
 * 为什么只提示不强制刷新：刷新会丢掉输入框里没发出去的草稿，而更新随时
 * 可能发生。会话状态在后端、刷新即恢复，唯独没发出去的那段话找不回来，
 * 所以把「什么时候刷」交给用户。但提示条不自动消失、也划不走——不刷新就是
 * 在用旧界面，这不是可以随手拂掉的通知。
 */
export function useVersionWatch() {
  const { t } = useTranslation()
  // 基线是「这一页连上时后端的版本」，不是最新版本：变了就说明后端换过。
  const baseline = useRef<string | null>(null)
  const notified = useRef(false)

  useEffect(() => {
    let source: EventSource | null = null
    let retry: ReturnType<typeof setTimeout> | null = null
    let backoff = RECONNECT_MS
    let stopped = false

    const onVersion = (version: string) => {
      if (baseline.current === null) {
        baseline.current = version
        return
      }
      if (version === baseline.current || notified.current) return
      notified.current = true
      toast(t("backend.updated", { version }), {
        description: t("backend.updatedDesc"),
        duration: Infinity,
        dismissible: false,
        action: {
          label: t("backend.reload"),
          onClick: () => window.location.reload(),
        },
      })
    }

    const scheduleRetry = () => {
      if (retry !== null || stopped) return
      retry = setTimeout(() => {
        retry = null
        connect()
      }, backoff)
      backoff = Math.min(backoff * 2, RECONNECT_MAX_MS)
    }

    const connect = () => {
      if (stopped) return
      source?.close()
      source = new EventSource(api.serverEventsUrl())
      source.onmessage = (e) => {
        // 连通了就把退避收回起点，下次真出事仍然反应快。
        backoff = RECONNECT_MS
        try {
          const ev = JSON.parse(e.data) as { kind?: string; version?: string }
          if (ev.kind === "hello" && ev.version) onVersion(ev.version)
        } catch {
          // 半条 JSON 没有意义，丢掉即可。
        }
      }
      source.onerror = () => {
        // 还在 CONNECTING 说明浏览器自己正在重连，别插手；CLOSED 才是它
        // 放弃了（后端起来了但这条路径答了个错误状态），由我们重来。
        if (source?.readyState === EventSource.CLOSED) scheduleRetry()
      }
    }

    connect()

    // 设备休眠再唤醒时连接可能已经悄悄死了，切回这一页立刻确认一次。
    const onVisible = () => {
      if (document.visibilityState !== "visible") return
      if (!source || source.readyState === EventSource.CLOSED) connect()
    }
    document.addEventListener("visibilitychange", onVisible)

    return () => {
      stopped = true
      if (retry !== null) clearTimeout(retry)
      document.removeEventListener("visibilitychange", onVisible)
      source?.close()
    }
  }, [t])
}
