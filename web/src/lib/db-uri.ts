import type { DataSourceInput, ServerInput, SSHAuth } from "@/types/acp"

/**
 * 连接 URI 的**解析**——一条链接换一整套连接参数，省掉逐字段手敲。
 *
 * 认两族格式：
 *
 *   navicat://conn.mysql?Conn.Host=…&Conn.Port=…&Conn.UseSSH=true&…
 *   mysql://user@host:3306/db?charset=utf8mb4
 *   jdbc:mysql://host:3306/db?user=x                     Java 那边的写法
 *   host:3306                                            光给地址也认
 *
 * Navicat 那族是从它的「URI」按钮复制出来的原样格式；后面几种是业界通用
 * 写法（DBeaver、TablePlus、命令行客户端说的都是它们）。SSH 隧道不在通用
 * 标准里，用 `sshHost`/`sshPort`/`sshUser`/`sshAuth`/`sshKeyPath` 查询参数
 * 扩展，导入导出对称。
 *
 * 导出在后端（server/internal/datasource/uri.go）：编辑已有连接时密码不
 * 下发到浏览器，这边根本拼不出带密码的链接。解析留在前端是因为它处理的
 * 是用户自己粘进来的文本，不涉及已存的秘密——顺带把 Navicat 的
 * `<PASSWORD>` 占位符当成「没给密码」，免得把它当真值填进表单。
 */

/**
 * 解析结果：能填进表单的字段（没出现在 URI 里的字段不返回）。
 *
 * 跳板机信息单独放在 `sshHint` 而不是摊进表单：数据源本身只存一个
 * `serverId`（adr-019），跳板机是服务器页的记录。一条外面来的 URI 描述的
 * 机器未必已经配过，所以这里只把它原样带出来，由对话框问用户要不要照它
 * 新建一台——直接按主机名去猜某条已存记录，猜错就连到别的机器上去了。
 */
export type ParsedUri = Partial<DataSourceInput> & {
  sshHint?: ServerInput
}

const SSH_AUTHS: SSHAuth[] = ["password", "key", "both"]

/** Navicat 导出时用的密码占位符，别把它当成真密码。 */
const PLACEHOLDERS = new Set(["<password>", "<passphrase>", ""])

function realSecret(value: string | null): string {
  const v = (value ?? "").trim()
  return PLACEHOLDERS.has(v.toLowerCase()) ? "" : v
}

/**
 * 解析一条连接 URI。认不出来返回 null——宁可让用户看到「这不是一条连接
 * URI」，也不要把半懂不懂的东西填进表单。
 */
export function parseDbUri(raw: string): ParsedUri | null {
  const text = raw.trim()
  if (!text) return null
  if (/^navicat:\/\//i.test(text)) return parseNavicatUri(text)
  return parseStandardUri(text)
}

/** Navicat「URI」按钮复制出来的格式：全部信息在 `Conn.*` 查询参数里。 */
function parseNavicatUri(text: string): ParsedUri | null {
  let url: URL
  try {
    url = new URL(text)
  } catch {
    return null
  }
  // 参数名大小写按 Navicat 的原样（Conn.Host），但宽松匹配更耐用——
  // 不同版本的大小写不保证一致。
  const params = new Map<string, string>()
  for (const [k, v] of url.searchParams) params.set(k.toLowerCase(), v)
  const get = (key: string) => params.get(key.toLowerCase()) ?? null

  const host = get("Conn.Host")
  if (!host) return null

  const out: ParsedUri = { host }
  const port = Number(get("Conn.Port"))
  if (port > 0) out.port = port
  const user = get("Conn.Username")
  if (user) out.user = user
  const password = realSecret(get("Conn.Password"))
  if (password) out.password = password
  // 库名的键各版本叫法不一，两个都试。
  const database = get("Conn.Database") || get("Conn.InitialDatabase")
  if (database) out.database = database

  if (isTrue(get("Conn.UseSSH"))) {
    const sshHost = get("Conn.SSH.Host") ?? ""
    const sshPort = Number(get("Conn.SSH.Port"))
    out.sshEnabled = true
    out.sshHint = {
      name: sshHost,
      host: sshHost,
      port: sshPort > 0 ? sshPort : 22,
      user: get("Conn.SSH.Username") ?? "root",
      auth: navicatAuth(get("Conn.SSH.AuthenticationMethod")),
      keyPath:
        get("Conn.SSH.PrivateKey") || get("Conn.SSH.PrivateKeyPath") || "",
      password: realSecret(get("Conn.SSH.Password")),
      passphrase: realSecret(get("Conn.SSH.Passphrase")),
    }
  }

  // 连接名拆成项目/环境：`dmit-dev` → dmit / dev。拆不开就整个当项目，
  // 反正两个字段都在表单里摆着，用户一眼能改。
  const name = get("Conn.Name")
  if (name) Object.assign(out, splitConnectionName(name))
  return out
}

/** `mysql://` / `jdbc:mysql://` / 裸 `host:port`。 */
function parseStandardUri(raw: string): ParsedUri | null {
  // jdbc:mysql://… 只是多了个前缀，剥掉之后与标准写法一致。
  let text = raw.replace(/^jdbc:/i, "")
  // 只给了 host:port 的话补上 scheme 让 URL 能解析。
  if (!/^[a-z][a-z0-9+.-]*:\/\//i.test(text)) text = `mysql://${text}`

  let url: URL
  try {
    url = new URL(text)
  } catch {
    return null
  }
  // 主机名得像个主机名。不校验的话 `随便写点什么` 会被 URL 当成 hostname
  // （还 punycode 编码一遍），于是一段普通文字就"解析成功"了。
  // 方括号是 IPv6 的写法（`[::1]`）。
  if (!/^[a-z0-9.\-_[\]:]+$/i.test(url.hostname)) return null

  const params = url.searchParams
  const out: ParsedUri = { host: url.hostname }

  if (url.port) out.port = Number(url.port)
  // 账号密码可以在 authority 段，也可以在查询参数里（JDBC 的习惯）。
  const user = decodeURIComponent(url.username) || params.get("user") || ""
  if (user) out.user = user
  const password = realSecret(
    decodeURIComponent(url.password) || params.get("password")
  )
  if (password) out.password = password

  const database = url.pathname.replace(/^\//, "")
  if (database) out.database = decodeURIComponent(database)

  const sshHost = params.get("sshHost") || params.get("ssh_host")
  if (sshHost) {
    const sshPort = Number(params.get("sshPort") || params.get("ssh_port"))
    const sshAuth = (params.get("sshAuth") || params.get("ssh_auth")) as SSHAuth
    out.sshEnabled = true
    out.sshHint = {
      name: sshHost,
      host: sshHost,
      port: sshPort > 0 ? sshPort : 22,
      user: params.get("sshUser") || params.get("ssh_user") || "root",
      auth: SSH_AUTHS.includes(sshAuth) ? sshAuth : "password",
      keyPath: params.get("sshKeyPath") || params.get("ssh_key_path") || "",
    }
  }

  // 其余参数原样进「连接参数」——tls、charset 这些驱动自己认得。
  const known = new Set([
    "user",
    "password",
    "sshhost",
    "ssh_host",
    "sshport",
    "ssh_port",
    "sshuser",
    "ssh_user",
    "sshauth",
    "ssh_auth",
    "sshkeypath",
    "ssh_key_path",
  ])
  const rest = [...params.entries()].filter(
    ([k]) => !known.has(k.toLowerCase())
  )
  if (rest.length > 0) {
    out.params = rest.map(([k, v]) => `${k}=${v}`).join("&")
  }
  return out
}

/**
 * 常见环境名。连接名只有一个词时用它判断这词是环境还是项目——
 * Navicat 里把本地连接直接叫 `local` 太常见了，那显然不是项目名。
 */
const COMMON_ENVS = new Set([
  "local",
  "dev",
  "development",
  "test",
  "testing",
  "sit",
  "uat",
  "pre",
  "preprod",
  "staging",
  "stage",
  "prod",
  "production",
  "online",
])

/**
 * 连接名拆项目与环境：`dmit-dev` → dmit / dev，按最后一个 `-` 或 `_` 分开。
 * 只有一个词时看它像不像环境名（`local`），像就当环境，否则当项目。
 *
 * 拆错也不要紧——两个字段都摆在表单里，用户一眼能改。
 */
function splitConnectionName(name: string): ParsedUri {
  const at = Math.max(name.lastIndexOf("-"), name.lastIndexOf("_"))
  if (at > 0 && at < name.length - 1) {
    return { project: name.slice(0, at), env: name.slice(at + 1) }
  }
  return COMMON_ENVS.has(name.toLowerCase()) ? { env: name } : { project: name }
}

/** Navicat 的 SSH 验证方法 → 我们的三档。 */
function navicatAuth(method: string | null): SSHAuth {
  switch ((method ?? "").toLowerCase()) {
    case "publickey":
      return "key"
    case "password":
    default:
      return "password"
  }
}

function isTrue(value: string | null): boolean {
  return (value ?? "").toLowerCase() === "true"
}
