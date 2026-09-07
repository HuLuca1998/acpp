import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { copyText } from "@/lib/clipboard"

import { api } from "@/lib/api"
import type { EnvDependency } from "@/types/acp"
import { AgentIcon } from "@/components/agent-icon"
import { StatusDot } from "@/components/status-dot"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { useAsyncData } from "@/hooks/use-async-data"
import {
  ArrowUpIcon,
  CopyIcon,
  DownloadIcon,
  RefreshCwIcon,
} from "lucide-react"

/** 连接测试的目标：内置两个工具，探测即真实拉起一次 agent。 */
const CONN_TOOLS = ["claude", "codex"] as const

/**
 * 环境体检分区：依赖清单（brew → node/npm → CLI 与 ACP 适配器）逐项检测，
 * 缺失的一键安装；brew 本体要交互式密码，只给可复制的引导命令。
 * 连接测试复用探测链路——探测成功即证明命令、依赖与登录态整条链可用。
 */
export function EnvCheck() {
  const { t } = useTranslation()
  const {
    data: info,
    error,
    setData,
  } = useAsyncData(() => api.system.env(), [])
  const [installing, setInstalling] = useState<string | null>(null)
  const [refreshing, setRefreshing] = useState(false)
  const [failOutput, setFailOutput] = useState<string | null>(null)
  const [testing, setTesting] = useState<string | null>(null)
  const [connResults, setConnResults] = useState<
    Record<string, { ok: boolean; text: string }>
  >({})

  const installedKeys = new Set(
    (info?.deps ?? []).filter((d) => d.installed).map((d) => d.key)
  )

  /** 安装与更新是同一条命令（npm 全局安装即升级），只有文案不同。 */
  async function install(dep: EnvDependency) {
    const updating = dep.outdated === true
    setInstalling(dep.key)
    setFailOutput(null)
    try {
      const res = await api.system.envInstall(dep.key)
      if (res.ok) {
        toast.success(
          updating
            ? t("settingsPage.env.updateDone", {
                name: depName(dep.key),
                // outdated 为真时后端必然给了 latest，兜底只为满足类型。
                version: dep.latest ?? "",
              })
            : t("settingsPage.env.installDone", { name: depName(dep.key) })
        )
      } else {
        toast.error(
          t(
            updating
              ? "settingsPage.env.updateFailed"
              : "settingsPage.env.installFailed",
            { name: depName(dep.key) }
          )
        )
        setFailOutput(res.output)
      }
      // 装完重查一次新版：刚更新的这项要立刻掉出「有新版」状态。
      setData(await api.system.env(true))
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setInstalling(null)
    }
  }

  /**
   * npm 旧安装占着命令名时一键安装必然撞车，只能先在终端清掉——把命令
   * 递到用户手上，比让他自己去查 npm 的 EEXIST 报错强。
   */
  async function copyMigrate(dep: EnvDependency) {
    const ok = await copyText(dep.migrateHint ?? "")
    if (ok) toast.success(t("settingsPage.env.migrateCopied"))
    else toast.error(t("common.copyFailed"))
  }

  async function refresh() {
    setRefreshing(true)
    try {
      setData(await api.system.env(true))
    } finally {
      setRefreshing(false)
    }
  }

  /** 连接测试 = 探测：拉临时会话读能力，成败即连通结论。 */
  async function testConn(name: string) {
    setTesting(name)
    try {
      const agents = await api.agents.list()
      const agent = agents.items.find((a) => a.name === name)
      if (!agent) {
        setConnResults((r) => ({
          ...r,
          [name]: { ok: false, text: t("settingsPage.tool.missing") },
        }))
        return
      }
      const probed = await api.agents.probe(agent.id)
      if (probed.lastError) {
        setConnResults((r) => ({
          ...r,
          [name]: { ok: false, text: probed.lastError! },
        }))
      } else {
        setConnResults((r) => ({
          ...r,
          [name]: {
            ok: true,
            text: t("settingsPage.env.connOk", { count: probed.models.length }),
          },
        }))
      }
    } catch (err) {
      setConnResults((r) => ({
        ...r,
        [name]: { ok: false, text: (err as Error).message },
      }))
    } finally {
      setTesting(null)
    }
  }

  function depName(key: string) {
    return t(`settingsPage.env.deps.${key}` as never, { defaultValue: key })
  }

  if (!info) {
    return (
      <div className="flex flex-col gap-4">
        {error ? (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : (
          <>
            <Skeleton className="h-40 w-full" />
            <Skeleton className="h-32 w-full" />
          </>
        )}
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      {/* 连接测试：真实拉起 agent 验证整条链。 */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">
            {t("settingsPage.env.connTitle")}
          </CardTitle>
          <CardDescription>
            {t("settingsPage.env.connDescription")}
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-1">
          {CONN_TOOLS.map((name) => {
            const result = connResults[name]
            return (
              <div
                key={name}
                className="flex items-center gap-3 rounded-md px-2 py-2 hover:bg-muted"
              >
                <AgentIcon flavor={name} className="size-4 shrink-0" />
                <span className="w-16 text-sm font-medium">{name}</span>
                {result ? (
                  <span
                    className={
                      result.ok
                        ? "min-w-0 flex-1 truncate text-xs text-muted-foreground"
                        : "min-w-0 flex-1 truncate text-xs text-destructive"
                    }
                    title={result.text}
                  >
                    <StatusDot
                      tone={result.ok ? "success" : "destructive"}
                      className="mr-1.5 inline-block"
                    />
                    {result.text}
                  </span>
                ) : (
                  <span className="flex-1" />
                )}
                <Button
                  size="sm"
                  variant="outline"
                  disabled={testing !== null}
                  onClick={() => void testConn(name)}
                >
                  {testing === name ? (
                    <Spinner className="size-3.5" data-icon="inline-start" />
                  ) : null}
                  {t("settingsPage.env.test")}
                </Button>
              </div>
            )
          })}
        </CardContent>
      </Card>

      {/* 依赖清单：按安装链排序，缺哪个装哪个。 */}
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <CardTitle className="text-base">
              {t("settingsPage.env.depsTitle")}
            </CardTitle>
            <Button
              size="sm"
              variant="outline"
              className="ml-auto"
              disabled={refreshing}
              onClick={() => void refresh()}
            >
              {refreshing ? (
                <Spinner className="size-3.5" data-icon="inline-start" />
              ) : (
                <RefreshCwIcon data-icon="inline-start" />
              )}
              {t("settingsPage.env.recheck")}
            </Button>
          </div>
          <CardDescription>
            {t("settingsPage.env.depsDescription")}
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-1">
          {info.deps.map((dep) => (
            <div
              key={dep.key}
              className="flex items-center gap-3 rounded-md px-2 py-2 hover:bg-muted"
            >
              <StatusDot
                tone={
                  !dep.installed
                    ? "destructive"
                    : dep.outdated || dep.migrateHint
                      ? "warning"
                      : "success"
                }
              />
              <span className="w-40 shrink-0 text-sm">{depName(dep.key)}</span>
              {dep.installed ? (
                <span className="flex min-w-0 flex-1 items-baseline gap-1.5 font-mono text-xs text-muted-foreground">
                  {/* 版本串可能很长（适配器会带包名前缀），截当前版本不截新版
                      ——「有新版」正是这一行要传达的信息，不能被省略号吃掉。 */}
                  <span className="truncate" title={dep.path}>
                    {dep.version || dep.path}
                  </span>
                  {dep.migrateHint ? (
                    <span className="shrink-0 text-warning">
                      {t("settingsPage.env.npmLegacy")}
                    </span>
                  ) : dep.outdated ? (
                    <span className="shrink-0 text-warning">
                      → {dep.latest}
                    </span>
                  ) : null}
                </span>
              ) : (
                <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground">
                  {dep.installKind === "bundled"
                    ? t("settingsPage.env.bundledHint")
                    : t("settingsPage.env.missing")}
                </span>
              )}
              {dep.migrateHint ? (
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => void copyMigrate(dep)}
                >
                  <CopyIcon data-icon="inline-start" />
                  {t("settingsPage.env.migrateCopy")}
                </Button>
              ) : (dep.installKind === "auto" && !dep.installed) ||
                dep.outdated ? (
                <Button
                  size="sm"
                  disabled={
                    installing !== null ||
                    (dep.requires !== undefined &&
                      !installedKeys.has(dep.requires))
                  }
                  title={
                    dep.requires && !installedKeys.has(dep.requires)
                      ? t("settingsPage.env.needFirst", {
                          name: depName(dep.requires),
                        })
                      : undefined
                  }
                  onClick={() => void install(dep)}
                >
                  {installing === dep.key ? (
                    <Spinner className="size-3.5" data-icon="inline-start" />
                  ) : dep.outdated ? (
                    <ArrowUpIcon data-icon="inline-start" />
                  ) : (
                    <DownloadIcon data-icon="inline-start" />
                  )}
                  {t(
                    dep.outdated
                      ? installing === dep.key
                        ? "settingsPage.env.updating"
                        : "settingsPage.env.update"
                      : installing === dep.key
                        ? "settingsPage.env.installing"
                        : "settingsPage.env.install"
                  )}
                </Button>
              ) : null}
            </div>
          ))}

          {/* 旧 npm 安装占着命令名：解释归解释，命令在行内那颗按钮上。 */}
          {info.deps.some((d) => d.migrateHint) ? (
            <Alert className="mt-2">
              <AlertDescription>
                {t("settingsPage.env.migrateHint")}
              </AlertDescription>
            </Alert>
          ) : null}

          {/* brew 装不了一键：交互式要密码，给复制命令去终端跑。 */}
          {info.deps.some((d) => !d.installed && d.installKind === "manual") ? (
            <Alert className="mt-2">
              <AlertDescription className="flex flex-col gap-2">
                <span>{t("settingsPage.env.brewManualHint")}</span>
                {info.deps
                  .filter((d) => !d.installed && d.installKind === "manual")
                  .map((d) => (
                    <span key={d.key} className="flex items-center gap-2">
                      <code className="min-w-0 flex-1 truncate rounded bg-muted px-2 py-1 font-mono text-xs">
                        {d.installHint}
                      </code>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => {
                          void copyText(d.installHint ?? "").then((ok) => {
                            if (ok) toast.success(t("settingsPage.env.copied"))
                            else toast.error(t("common.copyFailed"))
                          })
                        }}
                      >
                        <CopyIcon data-icon="inline-start" />
                        {t("settingsPage.env.copy")}
                      </Button>
                    </span>
                  ))}
              </AlertDescription>
            </Alert>
          ) : null}
        </CardContent>
      </Card>

      {failOutput ? (
        <Alert variant="destructive">
          <AlertDescription>
            <pre className="max-h-48 overflow-auto font-mono text-xs whitespace-pre-wrap">
              {failOutput}
            </pre>
          </AlertDescription>
        </Alert>
      ) : null}

      {/* PATH 是「装了却说没装」的第一排查点：GUI 与终端的 PATH 不同。 */}
      <p
        className="truncate font-mono text-xs text-muted-foreground"
        title={info.path}
      >
        {t("settingsPage.env.pathLabel")}: {info.path}
      </p>
    </div>
  )
}
