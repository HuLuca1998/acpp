import { useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link, useNavigate, useParams } from "react-router"
import type { DockviewApi } from "dockview-react"

import { DirPicker } from "@/components/dir-picker"
import {
  OrchChatContext,
  type OrchChatValue,
} from "@/components/orchestrator/orch-context"
import { OrchDock } from "@/components/orchestrator/orch-dock"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { useAsyncData } from "@/hooks/use-async-data"
import { useOrchChat } from "@/hooks/use-orch-chat"
import { api } from "@/lib/api"
import { NetworkIcon } from "lucide-react"

/**
 * 编排会话宿主页（adr-006）：/orchestrator/new 是草稿态（首条消息才创建），
 * /orchestrator/:id 是既有会话。面板经 dockview 编排，任务子会话面板由
 * 任务列表点开。
 */
export function OrchestratorChat() {
  const { t } = useTranslation()
  const params = useParams()
  const navigate = useNavigate()
  const isNew = params.id === undefined
  const orchId = isNew ? 0 : Number(params.id)

  const chat = useOrchChat(orchId)
  const { data: agents } = useAsyncData(
    () => api.agents.list().then((res) => res.items),
    []
  )
  const [draft, setDraft] = useState("")
  const [agentId, setAgentId] = useState(0)
  const [cwd, setCwd] = useState("")
  const [cwdPickerOpen, setCwdPickerOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const dockApiRef = useRef<DockviewApi | null>(null)

  const effectiveAgentId = agentId || agents?.[0]?.id || 0
  const draftCwd =
    cwd || agents?.find((a) => a.id === effectiveAgentId)?.cwd || ""

  function submit() {
    const content = draft.trim()
    if (!content) return
    if (isNew) {
      if (!effectiveAgentId || creating) return
      setCreating(true)
      void api.orchestrator
        .create({ agentId: effectiveAgentId, cwd })
        .then(async (session) => {
          await api.orchestrator.send(session.id, content)
          navigate(`/orchestrator/${session.id}`, { replace: true })
        })
        .finally(() => setCreating(false))
      setDraft("")
      return
    }
    setDraft("")
    void chat.send({ content })
  }

  function openTaskPanel(taskId: number) {
    const dock = dockApiRef.current
    if (!dock) return
    const id = `task:${taskId}`
    const existing = dock.getPanel(id)
    if (existing) {
      existing.api.setActive()
      return
    }
    // 打开在任务列表同组：天然成 tab 堆叠，用户再拖去想要的位置。
    dock.addPanel({
      id,
      component: "task",
      params: { taskId },
      position: { referencePanel: "tasks", direction: "within" },
    })
  }

  if (chat.loading) {
    return (
      <div className="mx-auto flex w-full max-w-3xl flex-col gap-3 p-4 lg:p-6">
        <Skeleton className="h-16 w-2/3" />
        <Skeleton className="h-16 w-1/2 self-end" />
        <Skeleton className="h-16 w-3/4" />
      </div>
    )
  }

  if (chat.notFound) {
    return (
      <Empty className="h-full justify-center">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <NetworkIcon />
          </EmptyMedia>
          <EmptyTitle>{t("orch.notFound")}</EmptyTitle>
          <EmptyDescription>{t("orch.notFoundHint")}</EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button render={<Link to="/orchestrator" />}>
            {t("orch.backToList")}
          </Button>
        </EmptyContent>
      </Empty>
    )
  }

  const value: OrchChatValue = {
    isNew,
    chat,
    draft,
    setDraft,
    submit,
    openTaskPanel,
    agentId: effectiveAgentId,
    setAgentId,
    draftCwd,
    openCwdPicker: () => setCwdPickerOpen(true),
  }

  return (
    <OrchChatContext.Provider value={value}>
      <div className="min-h-0 flex-1 p-1.5">
        <OrchDock attachApi={(dockApi) => (dockApiRef.current = dockApi)} />
      </div>

      <DirPicker
        open={cwdPickerOpen}
        onOpenChange={setCwdPickerOpen}
        initialPath={draftCwd || undefined}
        onSelect={setCwd}
      />
    </OrchChatContext.Provider>
  )
}
