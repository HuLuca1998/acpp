import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { copyText } from "@/lib/clipboard"
import { api } from "@/lib/api"
import { parseDbUri } from "@/lib/db-uri"
import { useAsyncData } from "@/hooks/use-async-data"
import type { DataSourceInput } from "@/types/acp"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Skeleton } from "@/components/ui/skeleton"
import { Textarea } from "@/components/ui/textarea"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { CopyIcon } from "lucide-react"

/**
 * URI 导入导出（对应 Navicat 连接对话框左下角的「URI」按钮）。
 *
 * 导入认 Navicat 自己的 `navicat://conn.mysql?Conn.*` 与通用的 `mysql://`
 * 两族；导出两种都给——前者贴回 Navicat 能直接用，后者是 DBeaver、命令行
 * 那边通用的写法。
 *
 * 导出的链接**带真实密码**（用户拍板）：不带密码的链接对方还得再问一次，
 * 等于没省事。代价是那条链接本身就是凭证——界面上标红说清楚。密码在后端
 * 手上，所以导出要现调接口取，还没保存的连接导不了。
 */
export function UriDialog({
  open,
  sourceId,
  onOpenChange,
  onImport,
}: {
  open: boolean
  /** 已保存连接的 id；0 表示还没保存（那时没法导出，密码在后端手上）。 */
  sourceId: number
  onOpenChange: (open: boolean) => void
  /** 导入成功后把解析到的字段合进表单。 */
  onImport: (parsed: Partial<DataSourceInput>) => void
}) {
  const { t } = useTranslation()
  const [text, setText] = useState("")

  function handleImport() {
    const parsed = parseDbUri(text)
    if (!parsed) {
      toast.error(t("db.uriInvalid"))
      return
    }
    onImport(parsed)
    setText("")
    onOpenChange(false)
    toast.success(t("db.uriImported"))
  }

  async function copy(value: string) {
    await copyText(value)
    toast.success(t("db.uriCopied"))
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t("db.uriTitle")}</DialogTitle>
          <DialogDescription>{t("db.uriHint")}</DialogDescription>
        </DialogHeader>

        <Tabs defaultValue="import">
          <TabsList>
            <TabsTrigger value="import">{t("db.uriImport")}</TabsTrigger>
            <TabsTrigger value="export">{t("db.uriExport")}</TabsTrigger>
          </TabsList>

          <TabsContent value="import">
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="uri-input">{t("db.uriPaste")}</FieldLabel>
                <Textarea
                  id="uri-input"
                  rows={4}
                  spellCheck={false}
                  value={text}
                  placeholder="navicat://conn.mysql?Conn.Host=…　或　mysql://root@127.0.0.1:3306/mydb"
                  className="font-mono text-xs"
                  onChange={(e) => setText(e.target.value)}
                />
              </Field>
              <p className="text-xs text-muted-foreground">
                {t("db.uriPlaceholderNote")}
              </p>
            </FieldGroup>
          </TabsContent>

          <TabsContent value="export">
            <ExportPane sourceId={sourceId} onCopy={copy} />
          </TabsContent>
        </Tabs>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          <Button disabled={!text.trim()} onClick={handleImport}>
            {t("db.uriDoImport")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** 导出面：链接**带真实密码**，所以要现从后端取（前端没有已存的密码）。 */
function ExportPane({
  sourceId,
  onCopy,
}: {
  sourceId: number
  onCopy: (value: string) => void
}) {
  const { t } = useTranslation()
  const { data, error } = useAsyncData(
    () => (sourceId ? api.datasources.uri(sourceId) : Promise.resolve(null)),
    [sourceId]
  )

  if (!sourceId) {
    return (
      <p className="p-4 text-sm text-muted-foreground">
        {t("db.uriSaveFirst")}
      </p>
    )
  }
  if (error) return <p className="p-4 text-sm text-destructive">{error}</p>
  if (!data) return <Skeleton className="h-32 w-full" />

  return (
    <FieldGroup>
      <UriRow label="Navicat" value={data.navicat} onCopy={onCopy} />
      <UriRow
        label={t("db.uriStandard")}
        value={data.standard}
        onCopy={onCopy}
      />
      {/* 这条链接就是凭证，值得说重一点。 */}
      <p className="text-xs text-destructive">{t("db.uriHasPassword")}</p>
    </FieldGroup>
  )
}

function UriRow({
  label,
  value,
  onCopy,
}: {
  label: string
  value: string
  onCopy: (value: string) => void
}) {
  return (
    <Field>
      <FieldLabel>{label}</FieldLabel>
      <div className="flex items-start gap-2">
        <code className="min-w-0 flex-1 rounded-lg border border-border bg-muted/40 px-2.5 py-2 font-mono text-xs leading-5 break-all">
          {value}
        </code>
        <Button
          type="button"
          variant="outline"
          size="icon"
          aria-label={label}
          onClick={() => onCopy(value)}
        >
          <CopyIcon />
        </Button>
      </div>
    </Field>
  )
}
