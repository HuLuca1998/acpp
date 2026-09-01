// 远程服务器（adr-019）的领域类型。
// 与 server/internal/model/server.go 及 internal/remote 的返回形状对齐；
// 从 ./acp 一并转出，调用方仍统一 import "@/types/acp"。

/** SSH 验证方式，照 Navicat 的三选一。 */
export type SSHAuth = "password" | "key" | "both"

/**
 * 一台可以 SSH 连上去的机器。它同时是两件事的底座：AI 的只读观察目标，
 * 与数据源的拨号跳板。`name` 是唯一标识，也是 AI 调工具时填的那个值。
 * 凭证永不下发，只有 hasPassword 这类标志位。
 */
export interface Server {
  id: number
  name: string
  host: string
  port: number
  user: string
  auth: SSHAuth
  /** 私钥路径；key/both 档下留空表示走 ssh-agent。 */
  keyPath: string
  /** 用途说明，**会随工具清单给 AI 看**。 */
  note: string
  disabled: boolean
  /** 有几条数据源把它当跳板机。>0 时不能删。 */
  usedBy: number
  hasPassword: boolean
  hasPassphrase: boolean
  createdAt: string
  updatedAt: string
}

/**
 * 新建/更新服务器的入参。两个凭证字段是「没传就不改」的语义：
 * 编辑时留空表示保持原值，不是清空。
 */
export interface ServerInput {
  name: string
  host: string
  port: number
  user: string
  auth?: SSHAuth
  password?: string
  keyPath?: string
  passphrase?: string
  note?: string
  disabled?: boolean
}

/** 测试连接的结果：连不上是配置问题，返回 200 带 error 文本。 */
export interface ServerTest {
  ok: boolean
  /** 对端的版本横幅，如 `SSH-2.0-OpenSSH_9.6p1`。 */
  version?: string
  error?: string
}
