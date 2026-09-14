/**
 * 吉祥物偏好。存 localStorage 不存后端：它纯粹是「这台设备上的这个人想不想
 * 要这只球、想让它蹲哪边」，和账号、租户都无关，没必要跨设备同步。
 *
 * 有开关这件事是硬要求：一个 56px、会动、会说话的角色，如果没法关掉，
 * 对不喜欢它的人就是纯粹的干扰。
 */

const STORAGE_KEY = "acpp.mascot.prefs"

export type MascotSide = "left" | "right"

export interface MascotPrefs {
  /** 整只关掉。关了输入卡回到原来的样子，不留占位。 */
  enabled: boolean
  /**
   * 安静模式：保留表情与目光，但不说话、不撒花、不转圈。
   * 想要一点陪伴又不想被逗的人用这个，而不是直接关掉。
   */
  quiet: boolean
  /** 蹲输入卡的哪一边。 */
  side: MascotSide
}

/** 默认开着、会说话、蹲右边——默认静默的功能等于不存在。 */
const DEFAULTS: MascotPrefs = {
  enabled: true,
  quiet: false,
  side: "right",
}

export function loadMascotPrefs(): MascotPrefs {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return DEFAULTS
    const saved = JSON.parse(raw) as Partial<MascotPrefs>
    return {
      enabled: saved.enabled ?? DEFAULTS.enabled,
      quiet: saved.quiet ?? DEFAULTS.quiet,
      side: saved.side === "left" ? "left" : "right",
    }
  } catch {
    // 隐私窗口 / 站点数据被清 / localStorage 被禁：拿默认值继续，不报错。
    return DEFAULTS
  }
}

/** 模块级广播：输入卡改了偏好，同一页里别处（设置面板）要立刻跟上。 */
type Listener = (prefs: MascotPrefs) => void
const listeners = new Set<Listener>()
let cache: MascotPrefs | null = null

export function getMascotPrefs(): MascotPrefs {
  if (cache === null) cache = loadMascotPrefs()
  return cache
}

export function saveMascotPrefs(patch: Partial<MascotPrefs>): MascotPrefs {
  const next = { ...getMascotPrefs(), ...patch }
  cache = next
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(next))
  } catch {
    // 存不下也要让本次会话生效，缓存已经更新了。
  }
  listeners.forEach((fn) => fn(next))
  return next
}

export function subscribeMascotPrefs(fn: Listener): () => void {
  listeners.add(fn)
  return () => listeners.delete(fn)
}
