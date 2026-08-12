import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { SkillScript, SkillScriptRunResult } from "@/types/acp"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { PlayIcon, TerminalIcon } from "lucide-react"

/** 脚本卡片：按头部元信息渲染参数控件，可传参试运行。version 变化时重载。 */
export function SkillScripts({
  skillName,
  version,
}: {
  skillName: string
  version: number
}) {
  const { t } = useTranslation()
  const [scripts, setScripts] = useState<SkillScript[] | null>(null)

  useEffect(() => {
    let cancelled = false
    api.skills
      .scripts(skillName)
      .then((res) => {
        if (!cancelled) setScripts(res.items)
      })
      .catch((err: Error) => toast.error(err.message))
    return () => {
      cancelled = true
    }
  }, [skillName, version])

  if (scripts === null) {
    return <Skeleton className="h-16 w-full" />
  }
  if (scripts.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        {t("skills.detail.noScripts")}
      </p>
    )
  }
  return (
    <div className="flex flex-col gap-4">
      {scripts.map((script) => (
        <ScriptCard key={script.path} skillName={skillName} script={script} />
      ))}
    </div>
  )
}

function ScriptCard({
  skillName,
  script,
}: {
  skillName: string
  script: SkillScript
}) {
  const { t } = useTranslation()
  const [args, setArgs] = useState<Record<string, string>>({})
  const [opts, setOpts] = useState<Record<string, boolean>>({})
  const [env, setEnv] = useState<Record<string, string>>({})
  const [running, setRunning] = useState(false)
  const [result, setResult] = useState<SkillScriptRunResult | null>(null)

  const hasMeta =
    script.description !== "" ||
    script.usage !== "" ||
    script.args.length + script.opts.length + script.envs.length > 0

  async function run() {
    setRunning(true)
    setResult(null)
    try {
      const res = await api.skills.runScript(skillName, {
        path: script.path,
        args: script.args.map((a) => args[a.name] ?? ""),
        opts: script.opts.filter((o) => opts[o.name]).map((o) => o.name),
        env: Object.fromEntries(
          script.envs
            .map((e) => [e.name, env[e.name] ?? ""] as const)
            .filter(([, v]) => v !== "")
        ),
      })
      setResult(res)
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setRunning(false)
    }
  }

  return (
    <div className="rounded-lg border p-4">
      <div className="flex items-center gap-2">
        <TerminalIcon className="size-4 text-muted-foreground" />
        <span className="font-mono text-xs font-medium">{script.path}</span>
        {script.runnable ? (
          <Button
            size="sm"
            variant="outline"
            className="ml-auto"
            disabled={running}
            onClick={run}
          >
            {running ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <PlayIcon data-icon="inline-start" />
            )}
            {running ? t("skills.detail.running") : t("skills.detail.run")}
          </Button>
        ) : (
          <span className="ml-auto text-xs text-muted-foreground">
            {t("skills.detail.notRunnable")}
          </span>
        )}
      </div>
      {script.description && (
        <p className="mt-1.5 text-sm text-muted-foreground">
          {script.description}
        </p>
      )}
      {script.usage && (
        <p className="mt-1 font-mono text-xs text-muted-foreground">
          {script.usage}
        </p>
      )}
      {!hasMeta && (
        <p className="mt-1.5 text-xs text-muted-foreground">
          {t("skills.detail.noMeta")}
        </p>
      )}

      {(script.args.length > 0 ||
        script.opts.length > 0 ||
        script.envs.length > 0) && (
        <div className="mt-3 grid gap-3 @lg/main:grid-cols-2">
          {script.args.map((arg) => (
            <div key={arg.name} className="flex flex-col gap-1.5">
              <Label
                htmlFor={`${script.path}-${arg.name}`}
                className="font-mono text-xs"
              >
                {arg.name}
              </Label>
              <Input
                id={`${script.path}-${arg.name}`}
                value={args[arg.name] ?? ""}
                onChange={(e) =>
                  setArgs((prev) => ({ ...prev, [arg.name]: e.target.value }))
                }
                placeholder={arg.label}
                className="font-mono text-xs"
              />
            </div>
          ))}
          {script.envs.map((v) => (
            <div key={v.name} className="flex flex-col gap-1.5">
              <Label
                htmlFor={`${script.path}-env-${v.name}`}
                className="font-mono text-xs"
              >
                {v.name}
                <span className="ml-1 font-sans text-muted-foreground">
                  · {t("skills.detail.envLabel")}
                </span>
              </Label>
              <Input
                id={`${script.path}-env-${v.name}`}
                value={env[v.name] ?? ""}
                onChange={(e) =>
                  setEnv((prev) => ({ ...prev, [v.name]: e.target.value }))
                }
                placeholder={v.label}
                className="font-mono text-xs"
              />
            </div>
          ))}
          {script.opts.map((opt) => (
            <Label
              key={opt.name}
              className="flex items-center gap-2 text-xs"
              htmlFor={`${script.path}-opt-${opt.name}`}
            >
              <Checkbox
                id={`${script.path}-opt-${opt.name}`}
                checked={opts[opt.name] ?? false}
                onCheckedChange={(on) =>
                  setOpts((prev) => ({ ...prev, [opt.name]: on === true }))
                }
              />
              <span className="font-mono">--{opt.name}</span>
              {opt.label && (
                <span className="text-muted-foreground">{opt.label}</span>
              )}
            </Label>
          ))}
        </div>
      )}

      {result && <RunResult result={result} />}
    </div>
  )
}

function RunResult({ result }: { result: SkillScriptRunResult }) {
  const { t } = useTranslation()
  return (
    <div className="mt-3 flex flex-col gap-2 border-t pt-3">
      <div className="flex items-center gap-2">
        <Badge
          variant={result.exitCode === 0 ? "secondary" : "destructive"}
          className={cn(result.exitCode === 0 && "text-success")}
        >
          {t("skills.detail.exitCode", { code: result.exitCode })}
        </Badge>
        <span className="text-xs text-muted-foreground tabular-nums">
          {t("skills.detail.duration", { ms: result.durationMs })}
        </span>
        {result.timedOut && (
          <Badge variant="destructive">{t("skills.detail.timedOut")}</Badge>
        )}
        {result.truncated && (
          <Badge variant="secondary">{t("skills.detail.truncated")}</Badge>
        )}
      </div>
      {(["stdout", "stderr"] as const).map(
        (stream) =>
          result[stream] !== "" && (
            <div key={stream}>
              <p className="mb-1 text-xs text-muted-foreground">
                {t(`skills.detail.${stream}`)}
              </p>
              <pre
                className={cn(
                  "max-h-64 overflow-auto rounded-md bg-muted/50 p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap",
                  stream === "stderr" && "text-destructive"
                )}
              >
                {result[stream]}
              </pre>
            </div>
          )
      )}
    </div>
  )
}
