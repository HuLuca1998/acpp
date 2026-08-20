/**
 * 会话活跃态的进程内广播。
 *
 * 侧边栏的最近会话只在路由变化时拉一次列表——用户在会话页里发消息，
 * 路由没变，列表里的 `state` 就一直停在拉取那一刻（多半是 idle），
 * 于是"正在跑"的呼吸点根本不亮。轮询能修，但为一个装饰性状态点每隔
 * 几秒打一次接口不划算：真正知道这件事的是会话页自己，让它直接说。
 *
 * 只覆盖本标签页内自己发起的会话；别处（另一个标签页、桌面版另一个
 * 窗口）跑的会话仍由列表数据决定，路由一变就跟上。
 */
const active = new Set<number>()
const listeners = new Set<() => void>()

/** 标记一条会话是否正在跑一轮。 */
export function markSessionActive(sessionId: number, running: boolean): void {
  if (!sessionId) return
  const had = active.has(sessionId)
  if (running === had) return
  if (running) active.add(sessionId)
  else active.delete(sessionId)
  // 快照必须换新对象，useSyncExternalStore 靠引用判变化。
  snapshot = new Set(active)
  listeners.forEach((l) => l())
}

let snapshot: ReadonlySet<number> = new Set<number>()

export function subscribeSessionActivity(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function getActiveSessions(): ReadonlySet<number> {
  return snapshot
}
