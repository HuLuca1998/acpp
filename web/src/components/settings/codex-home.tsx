import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { api } from "@/lib/api"
import { formatBytes, formatDateTime } from "@/lib/format"
import { useAsyncData } from "@/hooks/use-async-data"
import type { CodexFile } from "@/types/system"
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
import { Textarea } from "@/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { FolderOpenIcon, InfoIcon } from "lucide-react"

/**
 * codex 的隔离 home：acpp 把 CODEX_HOME 重定向到 <dataDir>/codex-home，
 * 机器级 ~/.codex 完全不在会话视野里。代价是那两个真正要改的文件也跟着
 * 藏起来了，这块就是把它们摆到台面上：
 *
 * - config.toml 是系统配置的**一次性副本**，之后系统那份的改动不再同步
 *   ——给 acpp 的 codex 换模型或 provider，改的就是这一份；
 * - auth.json 软链系统的登录态，改它等于改系统那一份（界面上会说明）。
 */
export function CodexHomeCard() {
  const { t, i18n } = useTranslation()
  const {
    data: home,
    error,
    reload,
  } = useAsyncData(() => api.system.codexHome(), [])
  const [name, setName] = useState("config.toml")
  // 内容连同它属于哪个文件一起存：切文件时不必在 effect 里同步清空，
  // 「还没加载好」就是 loaded.name 与当前选中的不一致。
  const [loaded, setLoaded] = useState<{
    name: string
    saved: string
    draft: string
  } | null>(null)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    let cancelled = false
    api.system
      .codexFile(name)
      .then((res) => {
        if (!cancelled) {
          setLoaded({ name, saved: res.content, draft: res.content })
        }
      })
      .catch((err: Error) => {
        if (!cancelled) toast.error(err.message)
      })
    return () => {
      cancelled = true
    }
  }, [name])

  const file: CodexFile | undefined = home?.files.find((f) => f.name === name)
  const ready = loaded?.name === name
  const dirty = ready && loaded.draft !== loaded.saved

  async function save() {
    if (!ready) return
    setSaving(true)
    try {
      await api.system.saveCodexFile(name, loaded.draft)
      setLoaded({ ...loaded, saved: loaded.draft })
      reload()
      toast.success(t("codexHome.saved"))
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  async function reveal() {
    try {
      await api.system.revealCodexHome()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("codexHome.title")}</CardTitle>
        <CardDescription>{t("codexHome.description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <div className="flex items-center gap-2">
          <code className="truncate rounded bg-muted px-2 py-1 font-mono text-xs text-muted-foreground">
            {home?.dir ?? ""}
          </code>
          <Button
            size="sm"
            variant="outline"
            className="ml-auto shrink-0"
            onClick={reveal}
          >
            <FolderOpenIcon data-icon="inline-start" />
            {t("codexHome.reveal")}
          </Button>
        </div>

        {error ? (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}

        <ToggleGroup
          value={[name]}
          onValueChange={(v) => v[0] && setName(v[0])}
          className="w-fit"
        >
          {(home?.files ?? []).map((f) => (
            <ToggleGroupItem key={f.name} value={f.name} className="font-mono">
              {f.name}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>

        {file ? (
          <p className="text-xs text-muted-foreground">
            {file.exists
              ? t("codexHome.meta", {
                  size: formatBytes(file.size),
                  time: formatDateTime(file.updatedAt, i18n.language),
                })
              : t("codexHome.missing")}
          </p>
        ) : null}

        {file?.symlink ? (
          /* auth.json 是软链：写进去等于改系统那一份登录态，这必须说在前面。 */
          <Alert>
            <InfoIcon />
            <AlertDescription>
              {t("codexHome.symlinkWarning", { target: file.target ?? "" })}
            </AlertDescription>
          </Alert>
        ) : null}

        {ready ? (
          <Textarea
            value={loaded.draft}
            onChange={(e) => setLoaded({ ...loaded, draft: e.target.value })}
            spellCheck={false}
            className="min-h-64 font-mono text-xs leading-relaxed"
          />
        ) : (
          <Skeleton className="h-64 w-full" />
        )}

        <div className="flex items-center gap-3">
          {dirty ? (
            <span className="text-xs text-warning">
              {t("codexHome.unsaved")}
            </span>
          ) : null}
          <Button
            size="sm"
            className="ml-auto"
            disabled={!dirty || saving}
            onClick={save}
          >
            {t("common.save")}
          </Button>
        </div>
        <p className="text-xs text-muted-foreground">
          {t("codexHome.effectNote")}
        </p>
      </CardContent>
    </Card>
  )
}
