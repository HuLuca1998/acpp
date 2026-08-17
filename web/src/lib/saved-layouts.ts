import type { SerializedDockview } from "dockview-react"

/** 自定义布局的存放键。内置预设是代码里的常量，这里只存用户自己存的。 */
const STORAGE_KEY = "acpp.workspace.saved-layouts.v1"

/** 存几套够用：布局是「几种干活姿势」，不是收藏夹。 */
export const MAX_SAVED_LAYOUTS = 8

export interface SavedLayout {
  name: string
  layout: SerializedDockview
  savedAt: string
}

/**
 * 用户自存的工作区布局（localStorage）。
 *
 * 与内置预设分开：预设是我们给的几种起点（IDE / 审查 / Git 工作台），
 * 存的是用户自己调出来的那一套——拖了半天的分栏值得被留住，而不是
 * 下次开会话又从头拖。
 */
export function loadSavedLayouts(): SavedLayout[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw) as SavedLayout[]
    if (!Array.isArray(parsed)) return []
    return parsed.filter(
      (item) => typeof item?.name === "string" && item.layout != null
    )
  } catch {
    // 存储被写坏了就当没存过——布局不是数据，丢了重新拖一次即可。
    return []
  }
}

/** 存一套布局；同名覆盖（用户第二次点「保存」就是想更新它）。 */
export function saveLayout(
  name: string,
  layout: SerializedDockview
): SavedLayout[] {
  const trimmed = name.trim()
  if (!trimmed) return loadSavedLayouts()

  const rest = loadSavedLayouts().filter((item) => item.name !== trimmed)
  const next = [
    { name: trimmed, layout, savedAt: new Date().toISOString() },
    ...rest,
  ].slice(0, MAX_SAVED_LAYOUTS)
  persist(next)
  return next
}

export function deleteLayout(name: string): SavedLayout[] {
  const next = loadSavedLayouts().filter((item) => item.name !== name)
  persist(next)
  return next
}

function persist(layouts: SavedLayout[]) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(layouts))
  } catch {
    // 配额满或隐私模式：存不下就算了，不打断正在干的活。
  }
}
