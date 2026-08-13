import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { useParams } from "react-router"

import { api } from "@/lib/api"
import type { Agent } from "@/types/acp"
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
 * agent 配置页：展示探测到的基础信息，models / commands 逐条勾选
 * 「是否在本软件里使用」。清单由探测维护，勾选即保存；被禁用的条目
 * 不再出现在模型下拉与 "/" 补全里，agent 侧能力本身不受影响。
 */
export function AgentDetail() {
  const { t } = useTranslation()
  const params = useParams()
  const agentId = Number(params.id)

  const [agent, setAgent] = useState<Agent | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [probing, setProbing] = useState(false)
  const [commandQuery, setCommandQuery] = useState("")

  useEffect(() => {
    let cancelled = false
    api.agents
      .get(agentId)
      .then((a) => {
        if (!cancelled) setAgent(a)
      })
      .catch((err: Error) => {
        if (!cancelled) setError(err.message)
      })
    return () => {
      cancelled = true
    }
  }, [agentId])

  async function probe() {
    setProbing(true)
    setError(null)
    try {
      setAgent(await api.agents.probe(agentId))
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setProbing(false)
    }
  }

  async function toggleModel(id: string, disabled: boolean) {
    try {
      setAgent(
        await api.agents.updateCatalog(agentId, {
          models: [{ key: id, disabled }],
        })
      )
    } catch (err) {
      setError((err as Error).message)
    }
  }

  /** 保存模型别名：必须带上当前 disabled，后端按整条覆盖。 */
  async function renameModel(id: string, disabled: boolean, alias: string) {
    try {
      setAgent(
        await api.agents.updateCatalog(agentId, {
          models: [{ key: id, disabled, alias }],
        })
      )
    } catch (err) {
      setError((err as Error).message)
    }
  }

  async function setFastPolicy(on: boolean) {
    try {
      setAgent(
        await api.agents.updateCatalog(agentId, {
          fastPolicy: on ? "on" : "off",
        })
      )
    } catch (err) {
      setError((err as Error).message)
    }
  }

  async function toggleCommand(name: string, disabled: boolean) {
    try {
      setAgent(
        await api.agents.updateCatalog(agentId, {
          commands: [{ key: name, disabled }],
        })
      )
    } catch (err) {
      setError((err as Error).message)
    }
  }

  /** 全启用 / 全禁用当前过滤出的命令。 */
  async function bulkCommands(disabled: boolean) {
    if (!agent) return
    const targets = filteredCommands.map((c) => ({ key: c.name, disabled }))
    try {
      setAgent(await api.agents.updateCatalog(agentId, { commands: targets }))
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
      <div className="mx-auto flex w-full max-w-3xl flex-col gap-4 p-4 lg:p-6">
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

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-4 p-4 lg:p-6">
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      {/* 基础信息：探测所得的身份与状态。 */}
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
              onClick={() => void probe()}
            >
              {probing ? (
                <Spinner className="size-3.5" data-icon="inline-start" />
              ) : (
                <RefreshCwIcon data-icon="inline-start" />
              )}
              {t("agents.detail.probe")}
            </Button>
          </div>
          <CardDescription className="font-mono">
            {agent.command} {agent.args.join(" ")}
          </CardDescription>
        </CardHeader>
        {agent.lastError ? (
          <CardContent className="text-sm text-destructive">
            {agent.lastError}
          </CardContent>
        ) : null}
      </Card>

      {/* 功能开关：agent 级取舍（如是否允许快速模式）。 */}
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
              onCheckedChange={(checked) => void setFastPolicy(checked === true)}
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
                    void toggleModel(m.id, checked !== true)
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
                      void renameModel(m.id, m.disabled ?? false, alias)
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
              onClick={() => void bulkCommands(false)}
            >
              {t("agents.detail.enableAll")}
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={() => void bulkCommands(true)}
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
                    void toggleCommand(c.name, checked !== true)
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
