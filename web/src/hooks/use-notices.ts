import { useSyncExternalStore } from "react"

import { getNotices, subscribeNotices, type Notice } from "@/lib/notify/store"

/** 订阅通知中心的存量（模块级 store，见 lib/notify/store.ts）。 */
export function useNotices(): Notice[] {
  return useSyncExternalStore(subscribeNotices, getNotices, getNotices)
}
