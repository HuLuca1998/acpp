import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { api } from "@/lib/api"
import type { OllamaModel, TitleModelConfig } from "@/types/acp"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import { SparklesIcon, WandSparklesIcon } from "lucide-react"

/**
 * 会话标题模型：把「首句截断」换成模型给的概括。
 *
 * 为什么要本机模型而不是让 agent 顺手起名——claude 与 codex 的自动标题都
 * 长在各自 CLI 层，ACP 通道只送得到「首条消息原文」级别的兜底值，拿不到
 * 真标题。没配置时会话沿用首句派生，一切照常。
 */
export function TitleModelCard() {
  const { t } = useTranslation()
  // 没用 useAsyncData：那个 hook 只管「拉一份数据」，而这里进页面要一次
  // 落三样——已存配置、编辑中的副本，以及按配置里的地址拉回的模型清单。
  const [saved, setSaved] = useState<TitleModelConfig | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [form, setForm] = useState<TitleModelConfig | null>(null)
  const [models, setModels] = useState<OllamaModel[] | null>(null)
  const [modelsError, setModelsError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [preview, setPreview] = useState<string | null>(null)

  const loadModels = useCallback(async (baseUrl: string) => {
    setModels(null)
    try {
      setModels(await api.system.titleModels(baseUrl))
      setModelsError(null)
    } catch (err) {
      setModels([])
      setModelsError((err as Error).message)
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    api.system
      .titleModel()
      .then((cfg) => {
        if (cancelled) return
        setSaved(cfg)
        setForm(cfg)
        return loadModels(cfg.baseUrl)
      })
      .catch((err: Error) => {
        if (!cancelled) setError(err.message)
      })
    return () => {
      cancelled = true
    }
  }, [loadModels])

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    )
  }
  if (!form) return <Skeleton className="h-64 w-full" />

  const dirty =
    saved !== null &&
    (form.enabled !== saved.enabled ||
      form.baseUrl !== saved.baseUrl ||
      form.model !== saved.model)

  async function save() {
    if (!form || saving) return
    setSaving(true)
    try {
      setSaved(await api.system.saveTitleModel(form))
      toast.success(t("settingsPage.titleModel.saved"))
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  // 用表单里的配置当场生成一个，让效果和耗时都看得见——省得保存完
  // 开一条会话去猜它到底行不行。
  async function test() {
    if (!form || testing) return
    setTesting(true)
    setPreview(null)
    try {
      const { title } = await api.system.testTitleModel(form)
      setPreview(title)
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setTesting(false)
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <WandSparklesIcon className="size-4" />
          {t("settingsPage.titleModel.title")}
        </CardTitle>
        <CardDescription>
          {t("settingsPage.titleModel.description")}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <FieldGroup>
          <Field orientation="horizontal">
            <FieldLabel htmlFor="title-model-enabled">
              {t("settingsPage.titleModel.enabledLabel")}
              <FieldDescription>
                {t("settingsPage.titleModel.enabledHint")}
              </FieldDescription>
            </FieldLabel>
            <Switch
              id="title-model-enabled"
              checked={form.enabled}
              onCheckedChange={(v) => setForm({ ...form, enabled: v })}
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="title-model-url">
              {t("settingsPage.titleModel.endpointLabel")}
            </FieldLabel>
            <Input
              id="title-model-url"
              className="font-mono"
              value={form.baseUrl}
              placeholder="http://127.0.0.1:11434"
              onChange={(e) => setForm({ ...form, baseUrl: e.target.value })}
              onBlur={() => void loadModels(form.baseUrl)}
            />
            <FieldDescription>
              {t("settingsPage.titleModel.endpointHint")}
            </FieldDescription>
          </Field>

          <Field>
            <FieldLabel htmlFor="title-model-name">
              {t("settingsPage.titleModel.modelLabel")}
            </FieldLabel>
            <Select
              value={form.model}
              onValueChange={(v) => {
                if (typeof v === "string") setForm({ ...form, model: v })
              }}
            >
              <SelectTrigger id="title-model-name" className="w-full">
                <SelectValue
                  placeholder={t("settingsPage.titleModel.modelPlaceholder")}
                />
              </SelectTrigger>
              <SelectContent>
                {(models ?? []).map((m) => (
                  <SelectItem key={m.name} value={m.name}>
                    <span className="font-mono">{m.name}</span>
                    <span className="text-muted-foreground">
                      {(m.size / 1e9).toFixed(1)} GB
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FieldDescription>
              {modelsError
                ? t("settingsPage.titleModel.modelsFailed")
                : t("settingsPage.titleModel.modelHint")}
            </FieldDescription>
          </Field>

          <Field orientation="horizontal">
            <Button
              variant="outline"
              disabled={!form.model || testing}
              onClick={() => void test()}
            >
              {testing ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <SparklesIcon data-icon="inline-start" />
              )}
              {t("settingsPage.titleModel.test")}
            </Button>
            <Button disabled={!dirty || saving} onClick={() => void save()}>
              {t("settingsPage.titleModel.save")}
            </Button>
          </Field>

          {preview !== null && (
            <Alert>
              <AlertDescription>
                {t("settingsPage.titleModel.preview")}
                <span className="font-medium text-foreground">{preview}</span>
              </AlertDescription>
            </Alert>
          )}
        </FieldGroup>
      </CardContent>
    </Card>
  )
}
