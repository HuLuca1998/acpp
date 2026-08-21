/**
 * 通知偏好。存 localStorage 而不是后端：这是「这台设备上的这个人此刻想不
 * 想被打扰」，同一个租户在手机和电脑上大可以有不同答案，不该跨设备同步。
 */

const STORAGE_KEY = "acpp.notify.prefs"

export interface NotifyPrefs {
  /** 决策与问答：agent 停在那儿等你，不通知就是让它干等。 */
  decisions: boolean
  /** 回答完成：长任务跑完了。 */
  results: boolean
  /** 出错：这一轮没能跑完。 */
  errors: boolean
  /** 提示音。人在看别的窗口时，声音是唯一还传得到的信号。 */
  sound: boolean
}

/**
 * 默认全开。宁可一开始吵一点让用户去关，也别让人以为「通知没做」——
 * 默认静默的功能等于不存在。
 */
const DEFAULT_NOTIFY_PREFS: NotifyPrefs = {
  decisions: true,
  results: true,
  errors: true,
  sound: true,
}

export function loadNotifyPrefs(): NotifyPrefs {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return DEFAULT_NOTIFY_PREFS
    // 逐项兜底：以后加了新开关，老的存档里没有那一项，不该整份作废。
    const saved = JSON.parse(raw) as Partial<NotifyPrefs>
    return { ...DEFAULT_NOTIFY_PREFS, ...saved }
  } catch {
    return DEFAULT_NOTIFY_PREFS
  }
}

export function saveNotifyPrefs(prefs: NotifyPrefs) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(prefs))
  } catch {
    // 隐私模式下 localStorage 会抛错。存不下就用默认值，不该连页面一起崩。
  }
}
