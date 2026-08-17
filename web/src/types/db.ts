// 数据库数据源（adr-008）的领域类型。
// 与 server/internal/model/datasource.go 及 internal/datasource 的返回形状对齐；
// 从 ./acp 一并转出，调用方仍统一 import "@/types/acp"。

/** SSH 隧道的验证方式，照 Navicat 的三选一。 */
export type SSHAuth = "password" | "key" | "both"

/**
 * 一个 MySQL 数据源。身份是「项目 + 环境」两级，`ref` 是派生的
 * `<项目>/<环境>`——它同时是列表里的显示名与 AI 调工具时填的 source。
 * 密码永不下发，只有 hasPassword 这类标志位。
 */
export interface DataSource {
  id: number
  project: string
  env: string
  ref: string
  host: string
  port: number
  user: string
  database: string
  params: string
  note: string
  sshEnabled: boolean
  sshHost: string
  sshPort: number
  sshUser: string
  sshAuth: SSHAuth
  sshKeyPath: string
  disabled: boolean
  hasPassword: boolean
  hasSSHPassword: boolean
  hasSSHPassphrase: boolean
  createdAt: string
  updatedAt: string
}

/**
 * 新建/更新数据源的入参。三个密码字段是「没传就不改」的语义：
 * 编辑时留空表示保持原密码，不是清空。
 */
export interface DataSourceInput {
  project: string
  env: string
  host: string
  port: number
  user: string
  password?: string
  database?: string
  params?: string
  note?: string
  sshEnabled?: boolean
  sshHost?: string
  sshPort?: number
  sshUser?: string
  sshAuth?: SSHAuth
  sshPassword?: string
  sshKeyPath?: string
  sshPassphrase?: string
  disabled?: boolean
}

/** 测试连接的结果：连不上是配置问题，返回 200 带 error 文本。 */
export interface DataSourceTest {
  ok: boolean
  version?: string
  error?: string
}

export interface DbDatabase {
  name: string
  charset?: string
  collation?: string
  /** MySQL 自带的库（information_schema 等），标出来但不隐藏。 */
  system?: boolean
  tables: number
}

export interface DbTable {
  name: string
  type: string
  engine?: string
  /** information_schema 的估算值，InnoDB 下并不精确。 */
  rows: number
  comment?: string
}

export interface DbColumn {
  name: string
  type: string
  nullable: boolean
  key?: string
  default?: string
  extra?: string
  comment?: string
}

export interface DbIndex {
  name: string
  unique: boolean
  type?: string
  columns: string[]
}

export interface DbTableDetail {
  database: string
  name: string
  columns: DbColumn[]
  indexes: DbIndex[]
  /** SHOW CREATE TABLE 原文，权限不足时为空。 */
  ddl?: string
}

/**
 * 一条语句的执行结果。查询类填 columns/rows，写入类填 affected，
 * 失败只填 error（其后的语句不会执行）。
 */
export interface SqlStatementResult {
  statement: string
  kind: "query" | "exec"
  columns?: string[]
  rows?: (string | number | boolean | null)[][]
  rowCount: number
  /** 命中上限被截断：显示的不是全部。 */
  truncated?: boolean
  affected?: number
  lastInsertId?: number
  elapsedMs: number
  error?: string
}

/** 一次执行请求的整体结果（可含多条语句）。 */
export interface SqlExecResult {
  database: string
  results: SqlStatementResult[]
  elapsedMs: number
}
