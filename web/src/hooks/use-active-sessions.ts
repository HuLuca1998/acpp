import { useSyncExternalStore } from "react"

import {
  getActiveSessions,
  subscribeSessionActivity,
} from "@/lib/session-activity"

/**
 * 订阅「哪些会话正在跑一轮」。侧边栏用它给状态点补上呼吸——它自己的
 * 列表数据只在路由变化时拉，看不到本页正在发生的事（lib/session-activity）。
 */
export function useActiveSessions(): ReadonlySet<number> {
  return useSyncExternalStore(subscribeSessionActivity, getActiveSessions)
}
