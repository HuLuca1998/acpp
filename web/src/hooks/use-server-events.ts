import { useEffect, useRef } from "react"

import { api } from "@/lib/api"
import type { ServerEvent } from "@/types/acp"

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
 * 全局事件流的**单一连接**（模块级，不随组件走）。
 *
 * 版本哨兵与通知都要听这条流，各建一条 EventSource 就是两倍连接、两倍
 * 重连风暴。浏览器对同源并发连接本来就有上限（HTTP/1.1 六条），会话流与
 * 工作区终端还要占位置。第一个订阅者到来时接通，最后一个离开时挂断。
 */
const subscribers = new Set<(ev: ServerEvent) => void>()

let source: EventSource | null = null
let retry: ReturnType<typeof setTimeout> | null = null
let backoff = RECONNECT_MS

function connect() {
  if (subscribers.size === 0) return
  source?.close()
  source = new EventSource(api.serverEventsUrl())

  source.onmessage = (e) => {
    // 连通了就把退避收回起点，下次真出事仍然反应快。
    backoff = RECONNECT_MS
    let ev: ServerEvent
    try {
      ev = JSON.parse(e.data) as ServerEvent
    } catch {
      // 半条 JSON 没有意义，丢掉即可。
      return
    }
    for (const fn of [...subscribers]) fn(ev)
  }

  source.onerror = () => {
    // 还在 CONNECTING 说明浏览器自己正在重连，别插手；CLOSED 才是它放弃了
    //（后端起来了但这条路径答了个错误状态），由我们重来。
    if (source?.readyState === EventSource.CLOSED) scheduleRetry()
  }
}

function scheduleRetry() {
  if (retry !== null || subscribers.size === 0) return
  retry = setTimeout(() => {
    retry = null
    connect()
  }, backoff)
  backoff = Math.min(backoff * 2, RECONNECT_MAX_MS)
}

function disconnect() {
  if (retry !== null) {
    clearTimeout(retry)
    retry = null
  }
  source?.close()
  source = null
  backoff = RECONNECT_MS
}

// 开发期注入口：控制台伪造一条全局事件，联调通知界面用（生产构建裁掉）。
//   window.__acppServerEvent({ kind: "notify", event: "permission", ... })
if (import.meta.env.DEV && typeof window !== "undefined") {
  ;(
    window as Window & { __acppServerEvent?: (ev: ServerEvent) => void }
  ).__acppServerEvent = (ev) => {
    for (const fn of [...subscribers]) fn(ev)
  }
}

// 设备休眠再唤醒时连接可能已经悄悄死了，切回页面立刻确认一次。
// 挂在模块级：连接只有一条，这个监听也只该有一个。
if (typeof document !== "undefined") {
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState !== "visible" || subscribers.size === 0) return
    if (!source || source.readyState === EventSource.CLOSED) connect()
  })
}

/**
 * 订阅全局事件流。
 *
 * 回调放进 ref 再订阅：调用方几乎一定传的是内联箭头函数，直接进依赖数组
 * 会每次渲染都退订重连——一条本该长期挂着的流，会变成随渲染抖动的连接。
 */
export function useServerEvents(onEvent: (ev: ServerEvent) => void) {
  const handler = useRef(onEvent)
  // 每次渲染后同步最新回调。写在 effect 里而不是渲染期直接赋值：渲染期改
  // ref 会让并发渲染下的读到值不确定（React 的 refs 规则也直接禁止）。
  useEffect(() => {
    handler.current = onEvent
  })

  useEffect(() => {
    const fn = (ev: ServerEvent) => handler.current(ev)
    subscribers.add(fn)
    if (subscribers.size === 1) connect()

    return () => {
      subscribers.delete(fn)
      if (subscribers.size === 0) disconnect()
    }
  }, [])
}
