import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { CopyButton } from "@/components/chat/copy-button"
import { McpResponseView } from "@/components/tools/mcp-response-view"
import { RawRequestPanel } from "@/components/tools/raw-request-panel"
import { ToolArgForm } from "@/components/tools/tool-arg-form"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { Spinner } from "@/components/ui/spinner"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { api } from "@/lib/api"
import {
  buildToolCall,
  coerceArgs,
  initialArgs,
  isDestructive,
  prettyJSON,
  toolFullName,
} from "@/lib/mcp-tool"
import type { McpInspectResult, McpServer, McpTool } from "@/types/acp"
import { EyeIcon, PencilLineIcon, PlayIcon } from "lucide-react"

/**
 * 工具详情：描述原文、参数表单、试运行，以及直接发原始 JSON-RPC。
 *
 * 描述展示的是**给模型看的那段原文**，不是另写的人类说明——这个页面的
 * 用处之一就是把「模型凭什么决定调不调它」摊开来看。
 *
 * 会改数据的工具（destructiveHint）按下运行前先弹确认，确认框里原样列出
 * 将要发出的参数：`local` 和 `pre` 只差两个字母，手滑的代价是线上库。
 */
export function ToolDetail({
  server,
  tool,
  cwd,
  onCalled,
}: {
  server: McpServer
  tool: McpTool
  cwd: string
  onCalled: () => void
}) {
  const { t } = useTranslation()
  const [values, setValues] = useState<Record<string, string>>(() =>
    initialArgs(tool.inputSchema)
  )
  const [running, setRunning] = useState(false)
  const [result, setResult] = useState<McpInspectResult | null>(null)
  const [confirming, setConfirming] = useState(false)

  const args = coerceArgs(tool.inputSchema, values)
  const writes = isDestructive(tool)

  async function run() {
    setRunning(true)
    try {
      const res = await api.tools.inspect({
        cwd,
        request: buildToolCall(tool.name, args),
      })
      setResult(res)
      onCalled()
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setRunning(false)
    }
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-4">
      <div className="flex flex-col gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <h2 className="font-mono text-base font-semibold">{tool.name}</h2>
          {writes ? (
            <Badge
              variant="outline"
              className="gap-1 border-warning/40 text-warning"
            >
              <PencilLineIcon className="size-3" />
              {t("tools.detail.writes")}
            </Badge>
          ) : (
            <Badge variant="outline" className="gap-1">
              <EyeIcon className="size-3" />
              {t("tools.detail.readOnly")}
            </Badge>
          )}
        </div>
        {/* agent 侧的工具全名：对话里的工具调用卡、claude 的预批清单
            用的都是它，排查「AI 到底调了哪个」时要能对上。 */}
        <div className="flex items-center gap-1">
          <code className="font-mono text-xs text-muted-foreground">
            {toolFullName(server.name, tool.name)}
          </code>
          <CopyButton text={toolFullName(server.name, tool.name)} />
        </div>
      </div>

      <div className="rounded-lg border bg-muted/30 p-3">
        <p className="text-xs font-medium text-muted-foreground">
          {t("tools.detail.description")}
        </p>
        <p className="mt-1 text-sm leading-6 whitespace-pre-wrap">
          {tool.description}
        </p>
      </div>

      <Tabs defaultValue="params">
        <TabsList>
          <TabsTrigger value="params">{t("tools.detail.params")}</TabsTrigger>
          <TabsTrigger value="schema">{t("tools.detail.schema")}</TabsTrigger>
          <TabsTrigger value="raw">{t("tools.detail.rawRequest")}</TabsTrigger>
        </TabsList>

        <TabsContent value="params" className="mt-3 flex flex-col gap-4">
          <ToolArgForm
            schema={tool.inputSchema}
            values={values}
            onChange={(name, value) =>
              setValues((prev) => ({ ...prev, [name]: value }))
            }
          />
          <div className="flex items-center gap-2">
            <Button
              onClick={() => (writes ? setConfirming(true) : run())}
              disabled={running}
            >
              {running ? <Spinner /> : <PlayIcon data-icon="inline-start" />}
              {running ? t("tools.detail.running") : t("tools.detail.run")}
            </Button>
            <span className="text-xs text-muted-foreground">
              {t("tools.detail.runHint")}
            </span>
          </div>
        </TabsContent>

        <TabsContent value="schema" className="mt-3">
          <pre className="max-h-96 overflow-auto rounded-lg border bg-muted/40 p-3 font-mono text-xs leading-5">
            {prettyJSON(tool.inputSchema)}
          </pre>
        </TabsContent>

        <TabsContent value="raw" className="mt-3">
          <RawRequestPanel
            cwd={cwd}
            tool={tool.name}
            args={args}
            onResult={(res) => {
              setResult(res)
              onCalled()
            }}
          />
        </TabsContent>
      </Tabs>

      {result ? (
        <>
          <Separator />
          <McpResponseView result={result} />
        </>
      ) : null}

      <AlertDialog open={confirming} onOpenChange={setConfirming}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t("tools.detail.confirmTitle")}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t("tools.detail.confirmDesc")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          {/* 把将要发出的参数原样摊开——确认框里最该看的就是这个。 */}
          <pre className="max-h-60 overflow-auto rounded-md border bg-muted/40 p-3 font-mono text-xs leading-5 whitespace-pre-wrap">
            {prettyJSON(args)}
          </pre>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                setConfirming(false)
                void run()
              }}
            >
              {t("tools.detail.confirmRun")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
