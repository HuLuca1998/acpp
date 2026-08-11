import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link, useNavigate } from "react-router"

import { api } from "@/lib/api"
import type { Agent } from "@/types/acp"
import { Composer } from "@/components/chat/composer"
import { DirPicker } from "@/components/dir-picker"
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
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  BotIcon,
  FolderIcon,
  FolderSearchIcon,
  MessagesSquareIcon,
  PlusIcon,
  SparklesIcon,
} from "lucide-react"

/** 草稿页的模型选择：跨 agent 的分组清单，选模型即选定 agent。 */
interface ModelChoice {
  key: string
  agentId: number
  /** 空串表示「该 agent 的 runtime 默认模型」（清单未探测到时的兜底项）。 */
  modelId: string
  label: string
  description?: string
}

/** 每个 agent 一组：有探测缓存就列模型，没有就给一条「默认」兜底项。 */
function modelChoicesOf(agent: Agent, fallbackLabel: string): ModelChoice[] {
  if (agent.models.length === 0) {
    return [
      {
        key: `${agent.id}:`,
        agentId: agent.id,
        modelId: "",
        label: fallbackLabel,
      },
    ]
  }
  return agent.models.map((m) => ({
    key: `${agent.id}:${m.id}`,
    agentId: agent.id,
    modelId: m.id,
    label: m.name,
    description: m.description,
  }))
}

/**
 * 草稿会话页：点「新建会话」直接进来，不弹窗、不建会话。
 * 模型在输入框旁选——跨 agent 的分组清单，选哪个模型就用哪个 agent；
 * 首条消息发出时才真正创建，标题由后端从首条消息自动简写。
 */
export function SessionNew() {
  const { t } = useTranslation()
  const navigate = useNavigate()

  const [agents, setAgents] = useState<Agent[] | null>(null)
  const [choiceKey, setChoiceKey] = useState("")
  const [cwd, setCwd] = useState("")
  const [pickerOpen, setPickerOpen] = useState(false)
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
        // 默认选第一个 agent 的第一个模型，多数场景零配置直接开聊。
        const first = res.items[0]
        if (first) {
          setChoiceKey(
            modelChoicesOf(first, t("sessions.form.defaultModel"))[0].key
          )
        }
      })
      .catch((err: Error) => {
        if (!cancelled) setError(err.message)
      })
    return () => {
      cancelled = true
    }
  }, [t])

  const groups = (agents ?? []).map((agent) => ({
    agent,
    choices: modelChoicesOf(agent, t("sessions.form.defaultModel")),
  }))
  const selected = groups
    .flatMap((g) => g.choices)
    .find((c) => c.key === choiceKey)
  const selectedAgent = agents?.find((a) => a.id === selected?.agentId)

  /**
   * 首条消息落地：创建会话 →（选了具体模型则先 open 再应用）→ 发送 →
   * 跳进正式会话页（replace 掉草稿页）。
   */
  async function start(content: string) {
    if (!selected || creating) return
    setCreating(true)
    setError(null)
    try {
      const session = await api.sessions.create({
        agentId: selected.agentId,
        cwd: cwd.trim(),
      })
      if (selected.modelId) {
        await api.sessions.open(session.id)
        await api.sessions.applySettings(session.id, {
          model: selected.modelId,
        })
      }
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
                  disabled={creating || !selected}
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
        disabled={agents === null || !selected}
        placeholder={t("chat.placeholder")}
      >
        {/* 模型选择：跨 agent 的分组清单，选模型即选定 agent。 */}
        {agents !== null && agents.length > 0 ? (
          <Select
            value={choiceKey}
            onValueChange={(v) => {
              if (typeof v === "string" && v) setChoiceKey(v)
            }}
          >
            <SelectTrigger
              size="sm"
              aria-label={t("sessions.form.modelLabel")}
              disabled={creating}
              className="h-7 gap-1 rounded-full border-transparent bg-transparent px-2.5 text-xs text-muted-foreground shadow-none transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97] dark:bg-transparent dark:hover:bg-muted"
            >
              <SparklesIcon className="size-3.5" />
              <SelectValue>
                {selected
                  ? groups.length > 1
                    ? `${selectedAgent?.name} · ${selected.label}`
                    : selected.label
                  : t("sessions.form.modelPlaceholder")}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {groups.map(({ agent, choices }) => (
                <SelectGroup key={agent.id}>
                  <SelectLabel className="flex items-center gap-1.5 text-xs uppercase">
                    <BotIcon className="size-3" />
                    {agent.name}
                  </SelectLabel>
                  {choices.map((choice) => (
                    <SelectItem key={choice.key} value={choice.key}>
                      <div className="flex min-w-0 flex-col">
                        <span>{choice.label}</span>
                        {choice.description ? (
                          <span className="max-w-64 truncate text-xs text-muted-foreground">
                            {choice.description}
                          </span>
                        ) : null}
                      </div>
                    </SelectItem>
                  ))}
                </SelectGroup>
              ))}
            </SelectContent>
          </Select>
        ) : null}

        {/* 工作目录：可留空用默认（~/acpp）；聚焦时展宽，放大镜打开目录选择器。 */}
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
        <button
          type="button"
          aria-label={t("dirPicker.browse")}
          disabled={creating}
          className="flex size-7 items-center justify-center rounded-full text-muted-foreground transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97]"
          onClick={() => setPickerOpen(true)}
        >
          <FolderSearchIcon className="size-3.5" />
        </button>
      </Composer>

      <DirPicker
        open={pickerOpen}
        onOpenChange={setPickerOpen}
        initialPath={cwd.trim() || selectedAgent?.cwd || undefined}
        onSelect={setCwd}
      />
    </div>
  )
}
