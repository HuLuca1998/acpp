import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link, useNavigate } from "react-router"

import { api } from "@/lib/api"
import type { Agent } from "@/types/acp"
import { Composer } from "@/components/chat/composer"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  BotIcon,
  FolderIcon,
  MessagesSquareIcon,
  PlusIcon,
} from "lucide-react"

/**
 * 草稿会话页：点「新建会话」直接进来，不弹窗、不建会话。
 * agent 与工作目录在输入框旁选（可不选用默认），首条消息发出时才真正创建，
 * 标题由后端从首条消息自动简写。对齐 ChatGPT / Claude 的进入即聊模式。
 */
export function SessionNew() {
  const { t } = useTranslation()
  const navigate = useNavigate()

  const [agents, setAgents] = useState<Agent[] | null>(null)
  const [agentId, setAgentId] = useState("")
  const [cwd, setCwd] = useState("")
  const [draft, setDraft] = useState("")
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    api.agents
      .list()
      .then((res) => {
        if (cancelled) return
        setAgents(res.items)
        // 默认选第一个 agent，多数场景零配置直接开聊。
        if (res.items.length > 0) setAgentId(String(res.items[0].id))
      })
      .catch((err: Error) => {
        if (!cancelled) setError(err.message)
      })
    return () => {
      cancelled = true
    }
  }, [])

  const selectedAgent = agents?.find((a) => String(a.id) === agentId)

  /** 首条消息落地：创建会话 → 发送 → 跳进正式会话页（replace 掉草稿页）。 */
  async function start(content: string) {
    if (!agentId || creating) return
    setCreating(true)
    setError(null)
    try {
      const session = await api.sessions.create({
        agentId: Number(agentId),
        cwd: cwd.trim(),
      })
      await api.sessions.send(session.id, content)
      void navigate(`/sessions/${session.id}`, { replace: true })
    } catch (err) {
      setError((err as Error).message)
      setCreating(false)
    }
  }

  function submit() {
    const content = draft.trim()
    if (!content) return
    void start(content)
  }

  // 一个 agent 都没有：引导先去注册，而不是给一个发不出去的输入框。
  if (agents !== null && agents.length === 0) {
    return (
      <Empty className="h-full justify-center">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <BotIcon />
          </EmptyMedia>
          <EmptyTitle>{t("agents.empty")}</EmptyTitle>
          <EmptyDescription>{t("sessions.form.agentHint")}</EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button render={<Link to="/agents/new" />}>
            <PlusIcon data-icon="inline-start" />
            {t("agents.add")}
          </Button>
        </EmptyContent>
      </Empty>
    )
  }

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      <div className="min-h-0 flex-1 overflow-hidden">
        <Empty className="h-full justify-center">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <MessagesSquareIcon />
            </EmptyMedia>
            <EmptyTitle>{t("chat.empty")}</EmptyTitle>
            <EmptyDescription>{t("chat.emptyHint")}</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            {error ? (
              <Alert variant="destructive" className="mb-2">
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            ) : null}
            <div className="flex flex-wrap justify-center gap-2">
              {(["intro", "changes", "help"] as const).map((key) => (
                <Button
                  key={key}
                  variant="outline"
                  size="sm"
                  className="rounded-full"
                  disabled={creating || !agentId}
                  onClick={() => void start(t(`chat.suggestions.${key}`))}
                >
                  {t(`chat.suggestions.${key}`)}
                </Button>
              ))}
            </div>
          </EmptyContent>
        </Empty>
      </div>

      <Composer
        value={draft}
        onChange={setDraft}
        onSubmit={submit}
        pending={creating}
        disabled={agents === null || !agentId}
        placeholder={t("chat.placeholder")}
      >
        {/* agent 选择：与会话页能力选择器同款的胶囊样式。 */}
        {agents !== null && agents.length > 1 ? (
          <Select
            value={agentId}
            onValueChange={(v) => {
              if (typeof v === "string" && v) setAgentId(v)
            }}
          >
            <SelectTrigger
              size="sm"
              aria-label={t("sessions.form.agentLabel")}
              disabled={creating}
              className="h-7 gap-1 rounded-full border-transparent bg-transparent px-2.5 text-xs text-muted-foreground shadow-none transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97] dark:bg-transparent dark:hover:bg-muted"
            >
              <BotIcon className="size-3.5" />
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
        ) : selectedAgent ? (
          <span className="flex h-7 items-center gap-1 rounded-full px-2.5 text-xs text-muted-foreground">
            <BotIcon className="size-3.5" />
            {selectedAgent.name}
          </span>
        ) : null}

        {/* 工作目录：可留空用 agent 默认；聚焦时展宽。 */}
        <label
          className="flex h-7 items-center gap-1 rounded-full px-2.5 text-xs text-muted-foreground transition-colors duration-150 ease-snappy focus-within:bg-muted focus-within:text-foreground hover:bg-muted"
          title={t("sessions.form.cwdHint")}
        >
          <FolderIcon className="size-3.5 shrink-0" />
          <span className="sr-only">{t("sessions.form.cwdLabel")}</span>
          <input
            value={cwd}
            disabled={creating}
            placeholder={selectedAgent?.cwd || t("sessions.form.cwdPlaceholder")}
            className="w-36 bg-transparent font-mono outline-none transition-[width] duration-200 ease-snappy placeholder:text-muted-foreground/60 focus:w-64"
            onChange={(e) => setCwd(e.target.value)}
          />
        </label>
      </Composer>
    </div>
  )
}
