import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { useNavigate } from "react-router"

import { api } from "@/lib/api"
import type { Agent } from "@/types/acp"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
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
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Spinner } from "@/components/ui/spinner"

export function SessionNew() {
  const { t } = useTranslation()
  const navigate = useNavigate()

  const [agents, setAgents] = useState<Agent[]>([])
  const [agentId, setAgentId] = useState("")
  const [title, setTitle] = useState("")
  const [cwd, setCwd] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    api.agents
      .list()
      .then((res) => {
        if (cancelled) return
        setAgents(res.items)
        // 只有一个 agent 时直接选中，省掉一次点击。
        if (res.items.length === 1) setAgentId(String(res.items[0].id))
      })
      .catch((err: Error) => {
        if (!cancelled) setError(err.message)
      })
    return () => {
      cancelled = true
    }
  }, [])

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!agentId || submitting) return

    setSubmitting(true)
    setError(null)
    try {
      const session = await api.sessions.create({
        agentId: Number(agentId),
        title: title.trim(),
        cwd: cwd.trim(),
      })
      void navigate(`/sessions/${session.id}`)
    } catch (err) {
      setError((err as Error).message)
      setSubmitting(false)
    }
  }

  const selectedAgent = agents.find((a) => String(a.id) === agentId)

  return (
    <div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6">
      <div className="px-4 lg:px-6">
        <Card className="mx-auto w-full max-w-2xl">
          <form onSubmit={submit}>
            <CardHeader>
              <CardTitle>{t("sessions.form.title")}</CardTitle>
              <CardDescription>
                {t("sessions.form.description")}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <FieldGroup>
                <Field>
                  <FieldLabel htmlFor="agent">
                    {t("sessions.form.agentLabel")}
                  </FieldLabel>
                  <Select
                    value={agentId}
                    onValueChange={(value) => setAgentId(value ?? "")}
                  >
                    <SelectTrigger id="agent">
                      <SelectValue
                        placeholder={t("sessions.form.agentPlaceholder")}
                      />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {agents.map((agent) => (
                          <SelectItem key={agent.id} value={String(agent.id)}>
                            {agent.name}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FieldDescription>
                    {agents.length === 0
                      ? t("sessions.form.agentHint")
                      : selectedAgent
                        ? [selectedAgent.command, ...selectedAgent.args].join(
                            " "
                          )
                        : t("sessions.form.agentHint")}
                  </FieldDescription>
                </Field>

                <Field>
                  <FieldLabel htmlFor="title">
                    {t("sessions.form.titleLabel")}
                  </FieldLabel>
                  <Input
                    id="title"
                    value={title}
                    onChange={(e) => setTitle(e.target.value)}
                    placeholder={t("sessions.form.titlePlaceholder")}
                  />
                  <FieldDescription>
                    {t("sessions.form.titleHint")}
                  </FieldDescription>
                </Field>

                <Field>
                  <FieldLabel htmlFor="cwd">
                    {t("sessions.form.cwdLabel")}
                  </FieldLabel>
                  <Input
                    id="cwd"
                    value={cwd}
                    onChange={(e) => setCwd(e.target.value)}
                    placeholder={
                      selectedAgent?.cwd || t("sessions.form.cwdPlaceholder")
                    }
                  />
                  <FieldDescription>
                    {t("sessions.form.cwdHint")}
                  </FieldDescription>
                </Field>

                {error ? (
                  <Alert variant="destructive">
                    <AlertDescription>{error}</AlertDescription>
                  </Alert>
                ) : null}
              </FieldGroup>
            </CardContent>
            <CardFooter>
              <Button type="submit" disabled={!agentId || submitting}>
                {submitting ? (
                  <>
                    <Spinner data-icon="inline-start" />
                    {t("sessions.form.submitting")}
                  </>
                ) : (
                  t("sessions.form.submit")
                )}
              </Button>
            </CardFooter>
          </form>
        </Card>
      </div>
    </div>
  )
}
