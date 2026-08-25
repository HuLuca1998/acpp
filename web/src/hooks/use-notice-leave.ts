import { useState } from "react"

import { dismissNotice } from "@/lib/notify/store"

/** 离场动画时长。再长就变成「删不掉」的错觉（规范上限 300ms）。 */
const LEAVE_MS = 240

/**
 * 关闭一条通知的两步走：先播离场动画，播完才真正从存量里删。
 * React 卸载是瞬时的，不给这一拍，卡片就是「啪」地消失——出现有动画、
 * 消失没有，像话说一半。
 */
export function useNoticeLeave(id: string) {
  const [leaving, setLeaving] = useState(false)
  const leave = () => {
    if (leaving) return
    setLeaving(true)
    setTimeout(() => dismissNotice(id), LEAVE_MS)
  }
  return { leaving, leave }
}
