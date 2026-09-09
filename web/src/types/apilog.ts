// 请求日志的领域类型，与 server/internal/model/api_log.go 对齐。
// 从 acp.ts 拆出来：那份是 ACP 会话域的契约，请求日志与它无关。

/** 一次 HTTP API 请求的记录。列表不带头与正文（省传输），详情才有。 */
export interface ApiLog {
  id: number
  method: string
  path: string
  query: string
  status: number
  durationMs: number
  /** 对方 IP（去掉端口） */
  remoteAddr: string
  /** 请求发自哪个页面（Origin，没有就是 Referer）；CLI 请求为空 */
  origin: string
  userAgent: string
  /** owner / 租户名 / anonymous */
  identity: string
  /** JSON 对象文本，凭证已抹成 [redacted] */
  requestHeaders: string
  responseHeaders: string
  requestBody: string
  responseBody: string
  requestSize: number
  responseSize: number
  createdAt: string
}
