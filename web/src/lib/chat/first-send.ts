import type { Message, SendInput, SettingsPatch } from "@/types/acp"

/**
 * 草稿页 → 会话页的首发交棒。
 *
 * 新建会话的第一条消息要经过 applySettings（内含 agent 握手，可能数十秒）
 * 才能真正发出。草稿页在 create 成功后立即跳转并把首发内容存进这里，
 * 会话页进入时认领：先乐观渲染用户气泡与思考态，不对着空白页等握手；
 * 派发（applySettings + send）由草稿页的 start() 在后台继续跑——
 * 事件处理器只执行一次，天然不受 StrictMode 二次挂载影响。
 */
export interface FirstSend {
  input: SendInput
  patch: SettingsPatch
}

interface Entry extends FirstSend {
  /** 后台派发失败的错误文案；undefined 表示派发还在进行。 */
  error?: string
  /** 会话页在场时的失败通知口，页面卸载即解除。 */
  onFail?: (error: string) => void
}

const stash = new Map<number, Entry>()

/** 草稿页 create 成功后暂存首发内容，随后立即跳转。 */
export function stashFirstSend(sessionId: number, send: FirstSend): void {
  stash.set(sessionId, { ...send })
}

/** 派发成功：交棒完成，用户消息从此由 SSE 回声接管。 */
export function clearFirstSend(sessionId: number): void {
  stash.delete(sessionId)
}

/** 派发失败：会话页在场就当场通知；不在场先记着，等下次进入时消费。 */
export function failFirstSend(sessionId: number, error: string): void {
  const entry = stash.get(sessionId)
  if (!entry) return
  if (entry.onFail) {
    stash.delete(sessionId)
    entry.onFail(error)
    return
  }
  entry.error = error
}

/**
 * 会话页进入时认领交棒：返回首发内容供乐观渲染；已失败的连错误一起
 * 带回并出栈。派发中的条目保留在栈里——StrictMode 二次挂载或离开再
 * 回来时能重新认领，重复认领只是重挂失败回调，不会重复派发。
 */
export function claimFirstSend(
  sessionId: number,
  onFail: (error: string) => void
): { send: FirstSend; error?: string } | null {
  const entry = stash.get(sessionId)
  if (!entry) return null
  if (entry.error !== undefined) {
    stash.delete(sessionId)
    return { send: entry, error: entry.error }
  }
  entry.onFail = onFail
  return { send: entry }
}

/** 会话页卸载时解除失败回调（交棒本身保留，回来还能续上）。 */
export function releaseFirstSend(sessionId: number): void {
  const entry = stash.get(sessionId)
  if (entry?.onFail) entry.onFail = undefined
}

/**
 * 首发的乐观用户消息：临时毫秒 id + local 标记，形状对齐后端
 * chat_turn.go 广播的临时消息（content 原文 + images/files payload），
 * 服务端回声到达时被 reducer 原位替换（见 chat-events.ts）。
 */
export function optimisticUserMessage(
  sessionId: number,
  input: SendInput
): Message {
  const payload: Record<string, unknown> = { local: true }
  if (input.images?.length) payload.images = input.images
  if (input.files?.length) payload.files = input.files
  return {
    id: Date.now(),
    sessionId,
    role: "user",
    kind: "text",
    content: input.content,
    payload,
    createdAt: new Date().toISOString(),
  }
}

/** 是不是本地乐观消息（还没被服务端回声替换掉的那条）。 */
export function isOptimisticMessage(m: Message | undefined): m is Message {
  return (
    m?.role === "user" &&
    (m.payload as { local?: boolean } | null)?.local === true
  )
}
