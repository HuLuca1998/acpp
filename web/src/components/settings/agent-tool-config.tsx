import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { api } from "@/lib/api"
import type { Agent, CatalogInput } from "@/types/acp"
import { StatusDot } from "@/components/status-dot"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { RefreshCwIcon, SearchIcon } from "lucide-react"

/**
 * 内置工具（claude / codex）的配置面，住在设置页对应分区里。
 * 记录由后端启动时预置，这里按 name 定位；配置项分四块：
 * 启动命令（可改，保存后自动重探）、功能开关、模型与 "/" 命令的启停取舍。
 */
export function AgentToolConfig({ name }: { name: string }) {
  const { t } = useTranslation()

  const [agent, setAgent] = useState<Agent | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [probing, setProbing] = useState(false)
  const [commandQuery, setCommandQuery] = useState("")
  // 命令编辑是显式保存（其余取舍都是即点即存），草稿独立于 agent 状态。
  const [commandDraft, setCommandDraft] = useState("")
  const [argsDraft, setArgsDraft] = useState("")
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    let cancelled = false
    // 放进微任务，避免在 effect 内同步 setState 触发级联渲染。
    queueMicrotask(() => {
      if (cancelled) return
      setAgent(null)
      setError(null)
      api.agents
        .list()
        .then((res) => {
          if (cancelled) return
          const hit = res.items.find((a) => a.name === name)
          if (!hit) {
            setError(t("settingsPage.tool.missing"))
            return
          }
          setAgent(hit)
          setCommandDraft(hit.command)
          setArgsDraft(hit.args.join(" "))
        })
        .catch((err: Error) => {
          if (!cancelled) setError(err.message)
        })
    })
    return () => {
      cancelled = true
    }
  }, [name, t])

  const probe = useCallback(
    async (id: number) => {
      setProbing(true)
      setError(null)
      try {
        setAgent(await api.agents.probe(id))
      } catch (err) {
        setError((err as Error).message)
      } finally {
        setProbing(false)
      }
    },
    []
  )

  /** 命令/参数保存后自动重探：能力清单跟着新命令走，不留旧缓存。 */
  async function saveCommand() {
    if (!agent) return
    setSaving(true)
    setError(null)
    try {
      const updated = await api.agents.update(agent.id, {
        ...agent,
        command: commandDraft.trim(),
        args: argsDraft.trim() === "" ? [] : argsDraft.trim().split(/\s+/),
      })
      setAgent(updated)
      toast.success(t("settingsPage.tool.saved"))
      void probe(updated.id)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  /** 全部配置取舍走同一条通道：提交补丁、整体替换 agent、统一报错。 */
  async function mutateCatalog(input: CatalogInput) {
    if (!agent) return
    try {
      setAgent(await api.agents.updateCatalog(agent.id, input))
    } catch (err) {
      setError((err as Error).message)
    }
  }

  // 清单最多几十条，直接过滤，不值得 memo。
  const query = commandQuery.trim().toLowerCase()
  const filteredCommands = (agent?.commands ?? []).filter(
    (c) =>
      !query ||
      c.name.toLowerCase().includes(query) ||
      (c.description ?? "").toLowerCase().includes(query)
  )

  if (!agent) {
    return (
      <div className="flex flex-col gap-4">
        {error ? (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : (
          <>
            <Skeleton className="h-24 w-full" />
            <Skeleton className="h-48 w-full" />
          </>
        )}
      </div>
    )
  }

  const enabledModels = agent.models.filter((m) => !m.disabled).length
  const enabledCommands = agent.commands.filter((c) => !c.disabled).length
  const commandDirty =
    commandDraft.trim() !== agent.command ||
    argsDraft.trim() !== agent.args.join(" ")

  return (
    <div className="flex flex-col gap-4">
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      {/* 启动命令：身份状态 + 可编辑的命令与参数。 */}
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <StatusDot
              tone={agent.status === "error" ? "destructive" : "muted"}
            />
            <CardTitle>{agent.name}</CardTitle>
            {agent.flavor ? (
              <Badge variant="secondary" className="uppercase">
                {agent.flavor}
              </Badge>
            ) : null}
            <Button
              size="sm"
              variant="outline"
              className="ml-auto"
              disabled={probing}
              onClick={() => void probe(agent.id)}
            >
              {probing ? (
                <Spinner className="size-3.5" data-icon="inline-start" />
              ) : (
                <RefreshCwIcon data-icon="inline-start" />
              )}
              {t("agents.detail.probe")}
            </Button>
          </div>
          <CardDescription>
            {t("settingsPage.tool.commandHint")}
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="flex items-center gap-2">
            <Input
              value={commandDraft}
              onChange={(e) => setCommandDraft(e.target.value)}
              placeholder={t("settingsPage.tool.commandPlaceholder")}
              className="font-mono text-sm"
            />
            <Input
              value={argsDraft}
              onChange={(e) => setArgsDraft(e.target.value)}
              placeholder={t("settingsPage.tool.argsPlaceholder")}
              className="w-56 font-mono text-sm"
            />
            <Button
              size="sm"
              disabled={saving || !commandDirty || commandDraft.trim() === ""}
              onClick={() => void saveCommand()}
            >
              {saving ? (
                <Spinner className="size-3.5" data-icon="inline-start" />
              ) : null}
              {t("settingsPage.tool.save")}
            </Button>
          </div>
          {agent.lastError ? (
            <p className="text-sm text-destructive">{agent.lastError}</p>
          ) : null}
        </CardContent>
      </Card>

      {/* 功能开关：工具级取舍（如是否允许快速模式）。 */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">
            {t("agents.detail.features")}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <label className="flex cursor-default items-center gap-3 rounded-md px-2 py-1.5 hover:bg-muted">
            <Checkbox
              checked={agent.fastPolicy !== "off"}
              onCheckedChange={(checked) =>
                void mutateCatalog({
                  fastPolicy: checked === true ? "on" : "off",
                })
              }
            />
            <span className="text-sm">{t("agents.detail.fastPolicy")}</span>
            <span className="ml-auto max-w-80 truncate text-xs text-muted-foreground">
              {t("agents.detail.fastPolicyHint")}
            </span>
          </label>
        </CardContent>
      </Card>

      {/* 模型勾选：决定哪些出现在模型下拉里。 */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">
            {t("agents.detail.models")}
          </CardTitle>
          <CardDescription>
            {t("agents.detail.enabledCount", {
              enabled: enabledModels,
              total: agent.models.length,
            })}
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-1">
          {agent.models.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              {t("agents.detail.noData")}
            </p>
          ) : (
            agent.models.map((m) => (
              <label
                key={m.id}
                className="flex cursor-default items-center gap-3 rounded-md px-2 py-1.5 hover:bg-muted"
              >
                <Checkbox
                  checked={!m.disabled}
                  onCheckedChange={(checked) =>
                    void mutateCatalog({
                      models: [{ key: m.id, disabled: checked !== true }],
                    })
                  }
                />
                <span className="text-sm">{m.name}</span>
                <span className="font-mono text-xs text-muted-foreground">
                  {m.id}
                </span>
                {/* 别名：显示在所有模型下拉里，留空用原名。失焦保存。 */}
                <Input
                  defaultValue={m.alias ?? ""}
                  placeholder={t("agents.detail.aliasPlaceholder")}
                  className="ml-auto h-7 w-36 text-xs"
                  onClick={(e) => e.preventDefault()}
                  onBlur={(e) => {
                    const alias = e.target.value.trim()
                    if (alias !== (m.alias ?? "")) {
                      void mutateCatalog({
                        // 必须带上当前 disabled，后端按整条覆盖。
                        models: [
                          { key: m.id, disabled: m.disabled ?? false, alias },
                        ],
                      })
                    }
                  }}
                />
              </label>
            ))
          )}
        </CardContent>
      </Card>

      {/* 命令勾选：决定哪些出现在 "/" 补全里。禁用不影响 agent 侧能力。 */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">
            {t("agents.detail.commands")}
          </CardTitle>
          <CardDescription>
            {t("agents.detail.enabledCount", {
              enabled: enabledCommands,
              total: agent.commands.length,
            })}
            {" · "}
            {t("agents.detail.commandsHint")}
          </CardDescription>
          <div className="flex items-center gap-2 pt-1">
            <div className="relative flex-1">
              <SearchIcon className="absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={commandQuery}
                placeholder={t("agents.detail.searchCommands")}
                className="h-8 pl-8 text-sm"
                onChange={(e) => setCommandQuery(e.target.value)}
              />
            </div>
            <Button
              size="sm"
              variant="outline"
              onClick={() =>
                void mutateCatalog({
                  commands: filteredCommands.map((c) => ({
                    key: c.name,
                    disabled: false,
                  })),
                })
              }
            >
              {t("agents.detail.enableAll")}
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={() =>
                void mutateCatalog({
                  commands: filteredCommands.map((c) => ({
                    key: c.name,
                    disabled: true,
                  })),
                })
              }
            >
              {t("agents.detail.disableAll")}
            </Button>
          </div>
        </CardHeader>
        <CardContent className="flex max-h-96 flex-col gap-0.5 overflow-y-auto">
          {filteredCommands.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              {t("agents.detail.noData")}
            </p>
          ) : (
            filteredCommands.map((c) => (
              <label
                key={c.name}
                className="flex cursor-default items-center gap-3 rounded-md px-2 py-1.5 hover:bg-muted"
              >
                <Checkbox
                  checked={!c.disabled}
                  onCheckedChange={(checked) =>
                    void mutateCatalog({
                      commands: [{ key: c.name, disabled: checked !== true }],
                    })
                  }
                />
                <span className="shrink-0 font-mono text-sm">/{c.name}</span>
                {c.description ? (
                  <span className="truncate text-xs text-muted-foreground">
                    {c.description}
                  </span>
                ) : null}
              </label>
            ))
          )}
        </CardContent>
      </Card>
    </div>
  )
}
