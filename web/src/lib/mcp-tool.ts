import type { McpInputSchema, McpTool } from "@/types/acp"

/**
 * MCP 工具的读法：怎么判断它会不会改数据、怎么拼一条 JSON-RPC 请求、
 * 怎么从响应里把模型真正读到的那段文本取出来。
 *
 * 这些是工具台与调用记录共用的纯函数——两处各解析一遍 JSON-RPC 形状，
 * 迟早会解析出两种结论。
 */

/** 一条 JSON-RPC 请求的 id：页面里每次发送递增，回来能对上号。 */
let nextId = 1

/**
 * 这个工具会不会改数据。看的是 MCP 标准注解而不是工具名——
 * 名字是约定，注解是声明，声明才是能跟着工具走的那个。
 */
export function isDestructive(tool: McpTool): boolean {
  return tool.annotations?.destructiveHint === true
}

/**
 * agent 侧看到的工具全名。claude 的预批清单（allowedTools）用的就是
 * 这个形状，工具调用卡片上显示的也是它。
 */
export function toolFullName(server: string, tool: string): string {
  return `mcp__${server}__${tool}`
}

/** 拼一条 tools/call 请求。试运行与自定义请求发的是同一种消息。 */
export function buildToolCall(
  tool: string,
  args: Record<string, unknown>
): object {
  return {
    jsonrpc: "2.0",
    id: nextId++,
    method: "tools/call",
    params: { name: tool, arguments: args },
  }
}

/** 拼一条无参方法请求（initialize / ping / tools/list）。 */
export function buildRequest(method: string, params?: object): object {
  return { jsonrpc: "2.0", id: nextId++, method, ...(params ? { params } : {}) }
}

/**
 * 按 schema 备一份初始参数：只填有默认值的，其余留空。
 *
 * 不替用户猜必填项的值——一个凭空出现的表名比空输入框更容易被直接按下
 * 运行，而它跑的是别人的库。
 */
export function initialArgs(schema?: McpInputSchema): Record<string, string> {
  const out: Record<string, string> = {}
  for (const [name, prop] of Object.entries(schema?.properties ?? {})) {
    out[name] = prop.default === undefined ? "" : String(prop.default)
  }
  return out
}

/**
 * 把表单里的字符串取值按 schema 转成真实类型的参数对象。
 *
 * **空串一律跳过**，不发 `""`：多数工具的可选参数是「不传就用默认」，
 * 发一个空字符串会被当成「显式指定为空」，两者的行为差得很远。
 */
export function coerceArgs(
  schema: McpInputSchema | undefined,
  values: Record<string, string>
): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const [name, raw] of Object.entries(values)) {
    if (raw === "") continue
    const type = schema?.properties?.[name]?.type
    if (type === "number" || type === "integer") {
      const n = Number(raw)
      out[name] = Number.isNaN(n) ? raw : n
    } else if (type === "boolean") {
      out[name] = raw === "true"
    } else {
      out[name] = raw
    }
  }
  return out
}

/** 参数是不是必填。 */
export function isRequired(schema: McpInputSchema | undefined, name: string) {
  return schema?.required?.includes(name) ?? false
}

/** 一条 JSON-RPC 响应拆开后的样子。 */
export interface McpResponseView {
  /** 模型真正读到的那段文本（tools/call 的 content）。 */
  text: string
  /** 工具级错误：工具跑了但失败了，模型能读到这段并自行决策。 */
  isError: boolean
  /** 协议级错误：请求本身没被受理（方法不存在、参数不合法、端点无效）。 */
  protocolError?: string
  /** 不是 tools/call 的响应（tools/list 等）时，这里是 result 原样。 */
  result?: unknown
}

/**
 * 从 JSON-RPC 响应里取出可读的部分。
 *
 * 刻意把两类错误分开：协议错误是「请求没被受理」，工具错误是「工具跑了
 * 但失败了」——前者是调用方写错了，后者是模型要处理的情况，混成一句
 * 「出错了」等于把最有用的那半句信息删掉。
 */
export function readResponse(response: unknown): McpResponseView {
  const view: McpResponseView = { text: "", isError: false }
  if (!isRecord(response)) return view

  const error = response.error
  if (isRecord(error)) {
    view.protocolError = String(error.message ?? "")
    return view
  }

  const result = response.result
  if (!isRecord(result)) return view
  view.result = result

  const content = result.content
  if (Array.isArray(content)) {
    view.text = content
      .map((part) => (isRecord(part) ? String(part.text ?? "") : ""))
      .join("")
    view.isError = result.isError === true
  }
  return view
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null
}

/** 格式化 JSON 给人看；不是 JSON 就原样回。 */
export function prettyJSON(value: unknown): string {
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

/** 解析用户手写的 JSON，失败回 null（由调用方给出提示，不抛）。 */
export function parseJSON(text: string): unknown | null {
  try {
    return JSON.parse(text)
  } catch {
    return null
  }
}
