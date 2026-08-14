import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { useSearchParams } from "react-router"
import { toast } from "sonner"

import { api } from "@/lib/api"
import type { SystemInfo } from "@/types/acp"
import { cn } from "@/lib/utils"
import { AgentIcon } from "@/components/agent-icon"
import { DirPicker } from "@/components/dir-picker"
import { AgentToolConfig } from "@/components/settings/agent-tool-config"
import { EnvCheck } from "@/components/settings/env-check"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
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
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import {
  ActivityIcon,
  FolderCogIcon,
  FolderOpenIcon,
  SettingsIcon,
} from "lucide-react"

/** 设置分区：系统 / 环境体检 + 两个内置工具（claude/codex 是产品固定形态，见 adr-005）。 */
const SECTIONS = [
  { key: "system", icon: <SettingsIcon className="size-4" /> },
  { key: "env", icon: <ActivityIcon className="size-4" /> },
  { key: "claude", icon: <AgentIcon flavor="claude" className="size-4" /> },
  { key: "codex", icon: <AgentIcon flavor="codex" className="size-4" /> },
] as const

type SectionKey = (typeof SECTIONS)[number]["key"]

const isSection = (v: string | null): v is SectionKey =>
  SECTIONS.some((s) => s.key === v)

/**
 * 设置面板：左侧分区菜单 + 右侧内容。
 * 系统分区管数据目录；claude/codex 分区是内置工具的配置面（命令、模型、
 * "/" 命令、功能开关）。?section= 支持深链，站内入口（概览卡等）直达分区。
 */
export function Settings() {
  const { t } = useTranslation()
  const [searchParams, setSearchParams] = useSearchParams()
  const paramSection = searchParams.get("section")
  const section: SectionKey = isSection(paramSection) ? paramSection : "system"
  const setSection = (key: SectionKey) =>
    setSearchParams(key === "system" ? {} : { section: key }, { replace: true })
  const [info, setInfo] = useState<SystemInfo | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [target, setTarget] = useState("")
  const [pickerOpen, setPickerOpen] = useState(false)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [migrating, setMigrating] = useState(false)

  useEffect(() => {
    let cancelled = false
    api.system
      .get()
      .then((res) => {
        if (!cancelled) setInfo(res)
      })
      .catch((err: Error) => {
        if (!cancelled) setError(err.message)
      })
    return () => {
      cancelled = true
    }
  }, [])

  async function migrate() {
    setMigrating(true)
    setError(null)
    try {
      const next = await api.system.migrateDataDir(target.trim())
      setInfo(next)
      setTarget("")
      toast.success(t("settingsPage.system.done"))
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setMigrating(false)
      setConfirmOpen(false)
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-4xl gap-6 p-4 lg:p-6">
      {/* 分区菜单 */}
      <nav className="flex w-40 shrink-0 flex-col gap-1">
        {SECTIONS.map(({ key, icon }) => (
          <button
            key={key}
            type="button"
            onClick={() => setSection(key)}
            className={cn(
              "flex items-center gap-2 rounded-md px-3 py-2 text-left text-sm transition-colors",
              section === key
                ? "bg-accent text-accent-foreground"
                : "text-muted-foreground hover:bg-muted hover:text-foreground"
            )}
          >
            {icon}
            {t(`settingsPage.menu.${key}`)}
          </button>
        ))}
      </nav>

      {/* 环境分区：依赖体检 + 连接测试。 */}
      {section === "env" ? (
        <div className="flex min-w-0 flex-1 flex-col">
          <EnvCheck />
        </div>
      ) : section !== "system" ? (
        /* 工具分区：内置 claude / codex 的配置面。 */
        <div className="flex min-w-0 flex-1 flex-col">
          <AgentToolConfig name={section} />
        </div>
      ) : (
      <div className="flex min-w-0 flex-1 flex-col gap-4">
        {error ? (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}

        {info === null ? (
          <Skeleton className="h-48 w-full" />
        ) : (
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                <FolderCogIcon className="size-4" />
                {t("settingsPage.system.title")}
              </CardTitle>
              <CardDescription>
                {t("settingsPage.system.description")}
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              <div className="flex items-center gap-2">
                <span className="shrink-0 text-sm text-muted-foreground">
                  {t("settingsPage.system.current")}
                </span>
                <span className="truncate font-mono text-sm">
                  {info.dataDir}
                </span>
                {info.dataDir === info.defaultDir ? (
                  <Badge variant="secondary" className="shrink-0">
                    {t("settingsPage.system.default")}
                  </Badge>
                ) : null}
              </div>

              {info.pendingDir ? (
                <Alert>
                  <AlertTitle>
                    {t("settingsPage.system.pendingTitle")}
                  </AlertTitle>
                  <AlertDescription className="font-mono">
                    {info.pendingDir}
                  </AlertDescription>
                </Alert>
              ) : null}

              <div className="flex items-center gap-2">
                <Input
                  value={target}
                  onChange={(e) => setTarget(e.target.value)}
                  placeholder={t("settingsPage.system.targetPlaceholder")}
                  className="font-mono text-sm"
                />
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setPickerOpen(true)}
                >
                  <FolderOpenIcon data-icon="inline-start" />
                  {t("settingsPage.system.browse")}
                </Button>
                <Button
                  size="sm"
                  disabled={target.trim() === "" || migrating}
                  onClick={() => setConfirmOpen(true)}
                >
                  {t("settingsPage.system.migrate")}
                </Button>
              </div>
            </CardContent>
          </Card>
        )}
      </div>
      )}

      <DirPicker
        open={pickerOpen}
        onOpenChange={setPickerOpen}
        initialPath={target.trim() || info?.dataDir || undefined}
        onSelect={setTarget}
      />

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t("settingsPage.system.confirmTitle")}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t("settingsPage.system.confirmBody", { dir: target.trim() })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={migrating}>
              {t("settingsPage.system.cancel")}
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={migrating}
              onClick={(e) => {
                e.preventDefault()
                void migrate()
              }}
            >
              {t("settingsPage.system.confirmAction")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
