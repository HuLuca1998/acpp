import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link, useParams } from "react-router"
import { toast } from "sonner"

import { api } from "@/lib/api"
import type { SkillDetail as SkillDetailData } from "@/types/acp"
import { SkillFiles } from "@/components/skills/skill-files"
import { SkillScripts } from "@/components/skills/skill-scripts"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { ChevronLeftIcon, PuzzleIcon } from "lucide-react"

export function SkillDetail() {
  const { t } = useTranslation()
  const { name = "" } = useParams()
  const [skill, setSkill] = useState<SkillDetailData | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [description, setDescription] = useState("")
  const [body, setBody] = useState("")
  const [saving, setSaving] = useState(false)
  // 文件区改动后驱动脚本区重载：scripts/ 下的文件就是脚本清单的来源。
  const [filesVersion, setFilesVersion] = useState(0)

  useEffect(() => {
    let cancelled = false
    api.skills
      .get(name)
      .then((res) => {
        if (cancelled) return
        setSkill(res)
        setDescription(res.description)
        setBody(res.body)
      })
      .catch((err: Error) => {
        if (!cancelled) setError(err.message)
      })
    return () => {
      cancelled = true
    }
  }, [name])

  const dirty =
    skill !== null &&
    (description !== skill.description || body !== skill.body)

  async function save() {
    if (!skill) return
    setSaving(true)
    try {
      const next = await api.skills.update(skill.name, { description, body })
      setSkill(next)
      setDescription(next.description)
      setBody(next.body)
      toast.success(t("skills.detail.saved"), {
        description: t("skills.effectNote"),
      })
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  async function toggle(enabled: boolean) {
    if (!skill) return
    setSkill({ ...skill, enabled })
    try {
      await api.skills.update(skill.name, { enabled })
      toast.success(
        t(enabled ? "skills.enabledToast" : "skills.disabledToast", {
          name: skill.name,
        }),
        { description: t("skills.effectNote") }
      )
    } catch (err) {
      setSkill({ ...skill, enabled: !enabled })
      toast.error((err as Error).message)
    }
  }

  if (error) {
    return (
      <div className="px-4 py-6 lg:px-6">
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <PuzzleIcon />
            </EmptyMedia>
            <EmptyTitle>{t("common.loadFailed")}</EmptyTitle>
            <EmptyDescription>{error}</EmptyDescription>
          </EmptyHeader>
        </Empty>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6">
      <div className="flex flex-col gap-4 px-4 md:gap-6 lg:px-6">
        <div className="flex items-center gap-3">
          <Button
            size="sm"
            variant="ghost"
            className="text-muted-foreground"
            render={<Link to="/skills" />}
          >
            <ChevronLeftIcon data-icon="inline-start" />
            {t("skills.detail.backToList")}
          </Button>
          <h1 className="font-mono text-base font-medium">{name}</h1>
          {skill && (
            <Switch
              checked={skill.enabled}
              onCheckedChange={toggle}
              aria-label={t("skills.enabled")}
            />
          )}
          <div className="ml-auto flex items-center gap-3">
            {dirty && (
              <span className="text-xs text-warning">
                {t("skills.detail.unsaved")}
              </span>
            )}
            <Button size="sm" disabled={!dirty || saving} onClick={save}>
              {t("common.save")}
            </Button>
          </div>
        </div>

        {skill === null ? (
          <div className="flex flex-col gap-4">
            <Skeleton className="h-24 w-full" />
            <Skeleton className="h-64 w-full" />
          </div>
        ) : (
          <>
            <Card>
              <CardContent>
                <FieldGroup>
                  <Field>
                    <FieldLabel htmlFor="skill-description">
                      {t("skills.create.descriptionLabel")}
                    </FieldLabel>
                    <Textarea
                      id="skill-description"
                      value={description}
                      onChange={(e) => setDescription(e.target.value)}
                      rows={2}
                    />
                    <FieldDescription>
                      {t("skills.create.descriptionHint")}
                    </FieldDescription>
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="skill-body">
                      {t("skills.detail.body")}
                    </FieldLabel>
                    <Textarea
                      id="skill-body"
                      value={body}
                      onChange={(e) => setBody(e.target.value)}
                      placeholder={t("skills.detail.bodyPlaceholder")}
                      className="min-h-72 font-mono text-xs leading-relaxed"
                    />
                  </Field>
                </FieldGroup>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>{t("skills.detail.files")}</CardTitle>
                <CardDescription>{t("skills.detail.filesHint")}</CardDescription>
              </CardHeader>
              <CardContent>
                <SkillFiles
                  skillName={name}
                  onChanged={() => setFilesVersion((v) => v + 1)}
                />
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>{t("skills.detail.scripts")}</CardTitle>
                <CardDescription>
                  {t("skills.detail.scriptsHint")}
                </CardDescription>
              </CardHeader>
              <CardContent>
                <SkillScripts skillName={name} version={filesVersion} />
              </CardContent>
            </Card>
          </>
        )}
      </div>
    </div>
  )
}
