import { useTranslation } from "react-i18next"

import { useAsyncData } from "@/hooks/use-async-data"
import { api } from "@/lib/api"
import { formatBytes, formatDateTime } from "@/lib/format"
import type { ApiLog } from "@/types/apilog"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"

/**
 * 一条请求的详情：请求 / 回复两个页签，各自是头 + 正文。
 *
 * 列表行不带头与正文（省传输），点开时按 id 单独取。JSON 正文顺手排版——
 * 后端存的是线上原文，一行几 KB 的 JSON 没法读。
 */
export function LogDetail({
  id,
  onClose,
}: {
  id: number | null
  onClose: () => void
}) {
  const { t, i18n } = useTranslation()
  const { data, error } = useAsyncData<ApiLog | null>(
    () => (id === null ? Promise.resolve(null) : api.logs.get(id)),
    [id]
  )

  return (
    <Dialog open={id !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="flex max-h-[85vh] flex-col sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle className="font-mono">
            {data
              ? `${data.method} ${data.path}${data.query ? `?${data.query}` : ""}`
              : t("logs.detailTitle", { id: id ?? "" })}
          </DialogTitle>
          {data ? (
            <DialogDescription className="flex flex-wrap gap-x-4 gap-y-1 tabular-nums">
              <span>{formatDateTime(data.createdAt, i18n.language)}</span>
              <span>{data.status}</span>
              <span>{data.durationMs} ms</span>
              <span>{data.identity}</span>
              <span className="font-mono">{data.remoteAddr}</span>
              {data.origin ? (
                <span className="font-mono">{data.origin}</span>
              ) : null}
            </DialogDescription>
          ) : null}
        </DialogHeader>

        {error ? (
          <p className="text-sm text-destructive">{error}</p>
        ) : !data ? (
          <div className="flex flex-col gap-2">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-32 w-full" />
          </div>
        ) : (
          <Tabs defaultValue="request" className="min-h-0 flex-1">
            <TabsList>
              <TabsTrigger value="request">{t("logs.request")}</TabsTrigger>
              <TabsTrigger value="response">{t("logs.response")}</TabsTrigger>
            </TabsList>
            <TabsContent value="request" className="min-h-0 overflow-y-auto">
              <Section
                headers={data.requestHeaders}
                body={data.requestBody}
                size={data.requestSize}
                extra={
                  data.userAgent
                    ? `${t("logs.userAgent")}: ${data.userAgent}`
                    : undefined
                }
              />
            </TabsContent>
            <TabsContent value="response" className="min-h-0 overflow-y-auto">
              <Section
                headers={data.responseHeaders}
                body={data.responseBody}
                size={data.responseSize}
              />
            </TabsContent>
          </Tabs>
        )}
      </DialogContent>
    </Dialog>
  )
}

function Section({
  headers,
  body,
  size,
  extra,
}: {
  headers: string
  body: string
  size: number
  extra?: string
}) {
  const { t } = useTranslation()
  return (
    <div className="flex flex-col gap-3 pt-3">
      <Block title={t("logs.headers")} text={prettyJSON(headers)} />
      {extra ? <p className="text-xs text-muted-foreground">{extra}</p> : null}
      <Block
        title={`${t("logs.body")}${size ? ` · ${formatBytes(size)}` : ""}`}
        text={
          body
            ? prettyJSON(body)
            : size
              ? t("logs.binaryBody", { size: formatBytes(size) })
              : t("logs.emptyBody")
        }
      />
    </div>
  )
}

function Block({ title, text }: { title: string; text: string }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-xs font-medium text-muted-foreground">{title}</span>
      <pre className="max-h-80 overflow-auto rounded-md bg-muted p-3 font-mono text-xs leading-relaxed break-all whitespace-pre-wrap">
        {text}
      </pre>
    </div>
  )
}

/** 能解析成 JSON 就缩进排版，不能（截断过、本来就不是 JSON）原样给。 */
function prettyJSON(text: string): string {
  try {
    return JSON.stringify(JSON.parse(text), null, 2)
  } catch {
    return text
  }
}
