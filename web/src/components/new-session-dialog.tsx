import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { useNavigate } from "react-router"

import { api } from "@/lib/api"
import type { Agent } from "@/types/acp"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
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

/**
 * 新建会话弹窗：轻量创建流程不切页，从任何入口原地弹出，
 * 创建成功后直接跳进对话页。trigger 传触发元素（Button / SidebarMenuButton 等）。
 */
export function NewSessionDialog({ trigger }: { trigger: React.ReactElement }) {
  const { t } = useTranslation()
  const navigate = useNavigate()

  const [open, setOpen] = useState(false)
  const [agents, setAgents] = useState<Agent[]>([])
  const [agentId, setAgentId] = useState("")
  const [title, setTitle] = useState("")
  const [cwd, setCwd] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // 关闭即重置一次性状态，下次打开是干净表单。
  function handleOpenChange(next: boolean) {
    setOpen(next)
    if (!next) {
      setError(null)
      setSubmitting(false)
    }
  }

  // 打开时才拉 agent 列表。
  useEffect(() => {
    if (!open) return
    let cancelled = false
    api.agents
      .list()
      .then((res) => {
        if (cancelled) return
        setAgents(res.items)
        if (res.items.length === 1) setAgentId(String(res.items[0].id))
      })
      .catch((err: Error) => {
        if (!cancelled) setError(err.message)
      })
    return () => {
      cancelled = true
    }
  }, [open])

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
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger render={trigger} />
      <DialogContent className="sm:max-w-md">
        <form onSubmit={submit} className="contents">
          <DialogHeader>
            <DialogTitle>{t("sessions.form.title")}</DialogTitle>
            <DialogDescription>
              {t("sessions.form.description")}
            </DialogDescription>
          </DialogHeader>

          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="new-session-agent">
                {t("sessions.form.agentLabel")}
              </FieldLabel>
              <Select
                value={agentId}
                onValueChange={(value) => setAgentId(value ?? "")}
              >
                <SelectTrigger id="new-session-agent">
                  {/* 弹层未挂载时 Base UI 取不到 item label，手动渲染当前名。 */}
                  <SelectValue>
                    {selectedAgent?.name ?? t("sessions.form.agentPlaceholder")}
                  </SelectValue>
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
                {selectedAgent
                  ? [selectedAgent.command, ...selectedAgent.args].join(" ")
                  : t("sessions.form.agentHint")}
              </FieldDescription>
            </Field>

            <Field>
              <FieldLabel htmlFor="new-session-title">
                {t("sessions.form.titleLabel")}
              </FieldLabel>
              <Input
                id="new-session-title"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder={t("sessions.form.titlePlaceholder")}
              />
            </Field>

            <Field>
              <FieldLabel htmlFor="new-session-cwd">
                {t("sessions.form.cwdLabel")}
              </FieldLabel>
              <Input
                id="new-session-cwd"
                value={cwd}
                onChange={(e) => setCwd(e.target.value)}
                placeholder={
                  selectedAgent?.cwd || t("sessions.form.cwdPlaceholder")
                }
                className="font-mono"
              />
              <FieldDescription>{t("sessions.form.cwdHint")}</FieldDescription>
            </Field>

            {error ? (
              <Alert variant="destructive">
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            ) : null}
          </FieldGroup>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => setOpen(false)}
            >
              {t("common.cancel")}
            </Button>
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
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
