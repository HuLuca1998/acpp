/**
 * 输入框草稿的微型 store。
 *
 * 打字是最高频的用户动作，draft 若是页面 state，每个按键都会重渲整棵
 * 工作区树（消息流、文件树、子代理面板全部跟着）——低配机器上空格要
 * 零点几秒才上屏就是这么来的。放进 ref 化的 store 之后，只有真正订阅
 * 它的那一个组件（输入框本身）重渲，页面层与其余面板保持安静。
 */
export interface DraftStore {
  get: () => string
  /** 支持函数式更新（回填、追加）。 */
  set: (next: string | ((prev: string) => string)) => void
  subscribe: (listener: () => void) => () => void
}

export function createDraftStore(): DraftStore {
  let value = ""
  const listeners = new Set<() => void>()
  return {
    get: () => value,
    set: (next) => {
      const resolved = typeof next === "function" ? next(value) : next
      if (resolved === value) return
      value = resolved
      listeners.forEach((listener) => listener())
    },
    subscribe: (listener) => {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
  }
}
