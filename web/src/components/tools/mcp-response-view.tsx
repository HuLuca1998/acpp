import { useTranslation } from "react-i18next"

import { CopyButton } from "@/components/chat/copy-button"
import { SqlResultView } from "@/components/db/sql-result-view"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { parseDbToolOutput } from "@/lib/db-result"
import { prettyJSON, readResponse } from "@/lib/mcp-tool"
import { cn } from "@/lib/utils"
import type { McpInspectResult } from "@/types/acp"
import { AlertTriangleIcon, CheckIcon, XIcon } from "lucide-react"

/**
 * 一次 JSON-RPC 往返的结果视图。
 *
 * 两个页签是刻意的：**结果**是模型真正读到的那段文本（数据库查询还原成
 * 表格），**原始 JSON** 是线上跑的完整响应。调试工具描述看前者，调试协议
 * 看后者，混在一起两边都不好使。
 */
export function McpResponseView({ result }: { result: McpInspectResult }) {
  const { t } = useTranslation()
  const view = readResponse(result.response)

  // 通知类消息（notifications/*）协议上就没有响应。说清楚这件事，
  // 否则一个空白结果区会被当成「请求失败了」。
  if (result.accepted) {
    return (
      <Alert>
        <CheckIcon />
        <AlertTitle>{t("tools.result.accepted")}</AlertTitle>
        <AlertDescription>{t("tools.result.acceptedDesc")}</AlertDescription>
      </Alert>
    )
  }

  const parsed = view.text ? parseDbToolOutput(view.text) : null

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <StatusBadge view={view} />
        <span className="text-xs text-muted-foreground tabular-nums">
          {t("tools.result.elapsed", { ms: result.durationMs })}
        </span>
        <div className="flex-1" />
        <CopyButton text={prettyJSON(result.response)} />
      </div>

      {view.protocolError ? (
        <Alert variant="destructive">
          <XIcon />
          <AlertTitle>{t("tools.result.protocolError")}</AlertTitle>
          <AlertDescription className="font-mono text-xs">
            {view.protocolError}
          </AlertDescription>
        </Alert>
      ) : null}

      <Tabs defaultValue="text">
        <TabsList>
          <TabsTrigger value="text">{t("tools.result.text")}</TabsTrigger>
          <TabsTrigger value="raw">{t("tools.result.raw")}</TabsTrigger>
        </TabsList>

        <TabsContent value="text" className="mt-2">
          {parsed ? (
            <div className="flex flex-col gap-2">
              <Badge variant="outline" className="w-fit font-mono text-[11px]">
                {parsed.source}
              </Badge>
              <SqlResultView results={parsed.results} />
            </div>
          ) : (
            <ResultText text={view.text} isError={view.isError} />
          )}
        </TabsContent>

        <TabsContent value="raw" className="mt-2">
          <ResultText text={prettyJSON(result.response)} />
        </TabsContent>
      </Tabs>
    </div>
  )
}

function StatusBadge({ view }: { view: ReturnType<typeof readResponse> }) {
  const { t } = useTranslation()
  if (view.protocolError) {
    return (
      <Badge variant="destructive" className="gap-1">
        <XIcon className="size-3" />
        {t("tools.result.protocolErrorShort")}
      </Badge>
    )
  }
  if (view.isError) {
    return (
      <Badge variant="outline" className="gap-1 border-warning/40 text-warning">
        <AlertTriangleIcon className="size-3" />
        {t("tools.result.toolError")}
      </Badge>
    )
  }
  return (
    <Badge variant="outline" className="gap-1 border-success/40 text-success">
      <CheckIcon className="size-3" />
      {t("tools.result.ok")}
    </Badge>
  )
}

/** 等宽文本块，自己滚——工具返回动辄几百行，不能把页面顶开。 */
function ResultText({ text, isError }: { text: string; isError?: boolean }) {
  const { t } = useTranslation()
  if (!text) {
    return (
      <p className="text-sm text-muted-foreground">{t("tools.result.empty")}</p>
    )
  }
  return (
    <pre
      className={cn(
        "max-h-96 overflow-auto rounded-lg border bg-muted/40 p-3",
        "font-mono text-xs leading-5 whitespace-pre-wrap",
        isError && "border-warning/40"
      )}
    >
      {text}
    </pre>
  )
}
