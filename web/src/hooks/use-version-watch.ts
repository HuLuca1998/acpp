import { useRef, useState } from "react"

import { useServerEvents } from "@/hooks/use-server-events"
import { pushNotice } from "@/lib/notify/store"

/**
 * 版本哨兵：后端换了版本就报出新版本号（没换时为 null），由侧栏底部的
 * 状态条就地给出刷新入口。
 *
 * 为什么需要它：owner 在设置页点的「一键更新」会替换 .app 并重启后端，
 * 但**别人的浏览器不会自己刷新**——局域网访客手里那一页仍是旧前端，接着
 * 用下去就是旧界面打新后端，接口契约一变就出错，出了错也看不出所以然。
 *
 * 为什么靠长连接而不是轮询：及时性全看「多久发现后端换了」，而轮询的发现
 * 延迟下限就是轮询间隔——要做到秒级，就得让局域网里每个页面每秒打一次
 * health。长连接反过来利用了更新的本质：**进程被换掉，那条流必断**，断开
 * 本身就是信号。浏览器自动重连，重连拿到的 hello 版本对不上就说明更新过。
 * 平时零请求，发现延迟只是后端起来后的一次重连（见 use-server-events.ts）。
 *
 * 为什么长在版本号旁边而不是弹提示条：提示条出现在角落，正好是人眼最容易
 * 略过的位置，而且它总要走——「这一页是旧的」是个持续状态，不是一次事件。
 * 版本号本来就写在那儿，更新入口挨着它才有上下文：看到的是「你手上是旧
 * 版本，点这里换新的」。刷新时机仍然交给用户：会话状态在后端、刷新即恢复，
 * 唯独输入框里没发出去的草稿找不回来。
 */
export function useVersionWatch(): string | null {
  // 基线是「这一页连上时后端的版本」，不是最新版本：变了就说明后端换过。
  const baseline = useRef<string | null>(null)
  const [updated, setUpdated] = useState<string | null>(null)

  useServerEvents((ev) => {
    if (ev.kind !== "hello" || !ev.version) return
    if (baseline.current === null) {
      baseline.current = ev.version
      return
    }
    if (ev.version === baseline.current) return
    setUpdated(ev.version)
    // 也落进通知中心：那里是「有事等你处理」的统一去处，而且带得了刷新
    // 按钮。侧栏状态条讲的是持久状态（你手上这份是旧的），两者不冲突。
    pushNotice({
      id: "update",
      kind: "update",
      text: `v${ev.version}`,
      at: Date.now(),
    })
  })

  return updated
}
