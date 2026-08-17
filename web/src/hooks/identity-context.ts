import { createContext, useContext } from "react"

import type { Identity } from "@/types/acp"

/** 身份上下文的值：全应用共享「我是谁、能看到什么」（adr-007）。 */
export interface IdentityState {
  identity: Identity | null
  loading: boolean
  /** 拉身份本身失败（后端没起来），与「未认证」是两回事。 */
  error: string | null
  refresh: () => Promise<void>
}

export const IdentityContext = createContext<IdentityState | null>(null)

/** 读当前身份。必须在 IdentityProvider 内使用。 */
export function useIdentity(): IdentityState {
  const ctx = useContext(IdentityContext)
  if (!ctx) {
    throw new Error("useIdentity must be used within IdentityProvider")
  }
  return ctx
}

/** owner 才有的能力：编排、设置、技能与角色的写、访客管理。 */
export function useIsOwner(): boolean {
  return useIdentity().identity?.owner ?? false
}
