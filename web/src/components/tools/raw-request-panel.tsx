import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { api } from "@/lib/api"
import {
  buildRequest,
  buildToolCall,
  parseJSON,
  prettyJSON,
} from "@/lib/mcp-tool"
import type { McpInspectResult } from "@/types/acp"
import { SendIcon } from "lucide-react"

/**
 * 自定义 JSON-RPC：直接对着工具面发一条你自己写的消息。
 *
 * 试运行只覆盖 tools/call 一种方法，而握手与清单同样会出问题——
 * agent 说「看不到这些工具」时，第一件事就是自己发一次 tools/list 看
 * 端点到底回了什么。这个框就是为那种时刻准备的。
 *
 * 预填的是当前工具的 tools/call 请求体：从一个能跑的例子改，比对着
 * 空框回忆协议形状快得多。
 */

/** 常用消息模板。四个方法就是我方外壳实现的全部（见 internal/mcp）。 */
const TEMPLATES = [
  {
    key: "initialize",
    label: "tools.raw.templateInitialize",
    build: () => buildRequest("initialize", {}),
  },
  {
    key: "ping",
    label: "tools.raw.templatePing",
    build: () => buildRequest("ping"),
  },
  {
    key: "toolsList",
    label: "tools.raw.templateToolsList",
    build: () => buildRequest("tools/list"),
  },
] as const

export function RawRequestPanel({
  cwd,
  tool,
  args,
  onResult,
}: {
  cwd: string
  tool: string
  args: Record<string, unknown>
  /** 结果交给上层统一渲染：一个工具只有一处结果区，不管是从哪个页签跑的。 */
  onResult: (result: McpInspectResult) => void
}) {
  const { t } = useTranslation()
  // 只在挂载时按当前参数预填一次。**不能**跟着 args 走：那样每敲一个字
  // 参数表单就会把这里正在编辑的请求体覆盖掉。想同步过来有下面那个按钮，
  // 换工具则由父组件的 key 重建整个面板。
  const [text, setText] = useState(() => prettyJSON(buildToolCall(tool, args)))
  const [sending, setSending] = useState(false)

  const parsed = parseJSON(text)
  const valid = parsed !== null

  async function send() {
    if (!valid) return
    setSending(true)
    try {
      onResult(await api.tools.inspect({ cwd, request: parsed }))
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setSending(false)
    }
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-1.5">
        <span className="text-xs text-muted-foreground">
          {t("tools.raw.templates")}
        </span>
        {TEMPLATES.map((tpl) => (
          <Button
            key={tpl.key}
            variant="outline"
            size="sm"
            onClick={() => setText(prettyJSON(tpl.build()))}
          >
            <code className="font-mono text-xs">{t(tpl.label)}</code>
          </Button>
        ))}
        {/* 把参数页签此刻填的值同步过来——覆盖编辑器内容是**用户按的**，
            不是它自己悄悄变的。 */}
        <Button
          variant="outline"
          size="sm"
          onClick={() => setText(prettyJSON(buildToolCall(tool, args)))}
        >
          <code className="font-mono text-xs">
            {t("tools.raw.fillFromForm")}
          </code>
        </Button>
      </div>

      <Textarea
        value={text}
        onChange={(e) => setText(e.target.value)}
        rows={10}
        spellCheck={false}
        className="font-mono text-xs leading-5"
        aria-label={t("tools.raw.editorLabel")}
      />

      <div className="flex items-center gap-2">
        <Button onClick={send} disabled={!valid || sending}>
          <SendIcon data-icon="inline-start" />
          {t("tools.raw.send")}
        </Button>
        {valid ? (
          <span className="text-xs text-muted-foreground">
            {t("tools.raw.hint")}
          </span>
        ) : (
          <span className="text-xs text-destructive">
            {t("tools.raw.invalid")}
          </span>
        )}
      </div>
    </div>
  )
}
