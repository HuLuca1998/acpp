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
  /** 私钥库里那把钥匙的 id（0 = 没选）。推荐配法：私钥内容跟着配置走。 */
  keyId: number
  /** 私钥路径（早于私钥库的配法）；两者都空表示走 ssh-agent。 */
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
  /** 选私钥库里的一把钥匙；0 表示不用库里的。 */
  keyId?: number
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

/**
 * 私钥库里的一把钥匙。私钥内容与通行短语永不下发（要看走 secret 端点），
 * 指纹与公钥随列表返回——前者用来认出是哪一把，后者直接复制去装进目标
 * 机器的 authorized_keys。
 */
export interface SSHKey {
  id: number
  name: string
  fingerprint: string
  publicKey: string
  note: string
  hasPassphrase: boolean
  /** 用这把钥匙的服务器台数，删之前看得见影响谁。 */
  usedBy: number
  createdAt: string
  updatedAt: string
}

/** 新建/更新私钥的入参。三条路：生成、粘贴内容、从本机文件导入。 */
export interface SSHKeyInput {
  name: string
  note?: string
  /** 生成一把新的 ed25519（忽略 privateKey / keyPath）。 */
  generate?: boolean
  privateKey?: string
  keyPath?: string
  passphrase?: string
}
