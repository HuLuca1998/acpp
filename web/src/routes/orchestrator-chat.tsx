import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Link, useNavigate, useParams } from "react-router"
import { useRef } from "react"

import { DirPicker } from "@/components/dir-picker"
import {
  OrchChatContext,
  type OrchChatValue,
} from "@/components/orchestrator/orch-context"
import { OrchDock } from "@/components/orchestrator/orch-dock"
import {
  WorkspaceAutoRefresh,
  WorkspaceProvider,
  WorkspaceAskSink,
  WorkspaceReferenceSink,
} from "@/components/workspace/workspace-provider"
import { useWorkspace } from "@/components/workspace/workspace-context"
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
import { fileToImageAttachment } from "@/lib/files"
import type { ImageAttachment, SendInput } from "@/types/acp"
import { NetworkIcon } from "lucide-react"

/**
 * 编排会话宿主页（adr-006）：/orchestrator/new 是草稿态（首条消息才创建），
 * /orchestrator/:id 是既有会话。编排是普通会话的升级——完整工作区
 * （文件树/预览/diff/commits/日志/终端）+ 任务列表与任务子会话面板，
 * 数据面经 WorkspaceProvider 的 orchestrator 作用域接入。
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
  const [filePickerOpen, setFilePickerOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const [images, setImages] = useState<ImageAttachment[]>([])
  const [files, setFiles] = useState<string[]>([])
  const imageInputRef = useRef<HTMLInputElement>(null)

  const effectiveAgentId = agentId || agents?.[0]?.id || 0
  const draftCwd =
    cwd || agents?.find((a) => a.id === effectiveAgentId)?.cwd || ""

  async function addImages(picked: File[]) {
    const attachments = await Promise.all(picked.map(fileToImageAttachment))
    setImages((prev) => [...prev, ...attachments])
  }

  function submit() {
    const content = draft.trim()
    if (!content && images.length === 0 && files.length === 0) return
    const input: SendInput = { content, images, files }
    setDraft("")
    setImages([])
    setFiles([])
    if (isNew) {
      if (!effectiveAgentId || creating) return
      setCreating(true)
      void api.orchestrator
        .create({ agentId: effectiveAgentId, cwd })
        .then(async (session) => {
          await api.orchestrator.send(session.id, input)
          navigate(`/orchestrator/${session.id}`, { replace: true })
        })
        .finally(() => setCreating(false))
      return
    }
    // 一轮进行中：插话先排队，轮次结束自动发出（可撤回/立即插入）。
    if (chat.busy) {
      chat.enqueue(input)
    } else {
      void chat.send(input)
    }
  }

  /** 撤回一条排队插话：从队列移除并回填输入框与附件托盘。 */
  function recallQueued(id: number) {
    const item = chat.queued.find((q) => q.id === id)
    if (!item) return
    chat.removeQueued(id)
    const { content, images: qImages, files: qFiles } = item.input
    if (content) {
      setDraft((prev) => (prev ? `${prev}\n${content}` : content))
    }
    if (qImages?.length) setImages((prev) => [...prev, ...qImages])
    if (qFiles?.length) {
      setFiles((prev) => [...prev, ...qFiles.filter((f) => !prev.includes(f))])
    }
  }

  /** 「调整方向」：把排队插话立即发出，插进正在跑的轮。 */
  function steerQueued(id: number) {
    const item = chat.queued.find((q) => q.id === id)
    if (!item) return
    chat.removeQueued(id)
    void chat.send(item.input)
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

  const value: Omit<OrchChatValue, "openTaskPanel"> = {
    isNew,
    chat,
    draft,
    setDraft,
    submit,
    agentId: effectiveAgentId,
    setAgentId,
    draftCwd,
    openCwdPicker: () => setCwdPickerOpen(true),
    images,
    files,
    removeImage: (i) => setImages((prev) => prev.filter((_, idx) => idx !== i)),
    removeFile: (i) => setFiles((prev) => prev.filter((_, idx) => idx !== i)),
    addImages: (picked) => void addImages(picked),
    openImagePicker: () => imageInputRef.current?.click(),
    openFilePicker: () => setFilePickerOpen(true),
    recallQueued,
    steerQueued,
  }

  return (
    <WorkspaceProvider sessionId={isNew ? 0 : orchId} scope={api.orchestrator}>
      <OrchContextBridge value={value}>
        <div className="min-h-0 flex-1 p-1.5">
          <OrchDock />
        </div>
      </OrchContextBridge>
      {/* turn 结束刷新 git/文件树——子代理刚改完文件的时刻。 */}
      <WorkspaceAutoRefresh busy={chat.busy || hasRunningTask(chat)} />
      {/* git 面板右键的「让 AI 分析」：把写好的 prompt 填进输入框——
          只填不发，发不发是用户的决定。 */}
      <WorkspaceAskSink
        onAsk={(prompt) =>
          setDraft((prev) =>
            prev.trim() ? `${prev.trimEnd()}\n\n${prompt}` : prompt
          )
        }
      />
      {/* 工作区右键/预览的「添加到引用」落到 composer 的 @ 引用列表。 */}
      <WorkspaceReferenceSink
        onAdd={(path) =>
          setFiles((prev) => (prev.includes(path) ? prev : [...prev, path]))
        }
      />

      <input
        ref={imageInputRef}
        type="file"
        accept="image/*"
        multiple
        className="hidden"
        onChange={(e) => {
          const picked = Array.from(e.target.files ?? [])
          e.target.value = ""
          if (picked.length > 0) void addImages(picked)
        }}
      />

      <DirPicker
        mode="file"
        open={filePickerOpen}
        onOpenChange={setFilePickerOpen}
        initialPath={(isNew ? cwd : chat.orchSession?.cwd) || undefined}
        onSelect={(path) =>
          setFiles((prev) => (prev.includes(path) ? prev : [...prev, path]))
        }
      />

      <DirPicker
        open={cwdPickerOpen}
        onOpenChange={setCwdPickerOpen}
        initialPath={draftCwd || undefined}
        onSelect={setCwd}
      />
    </WorkspaceProvider>
  )
}

function hasRunningTask(chat: ReturnType<typeof useOrchChat>): boolean {
  return chat.tasks.some((task) => task.state === "running")
}

/**
 * openTaskPanel 需要 dockview api（挂在 WorkspaceProvider 的命令总线上），
 * 所以 OrchChatContext 的完整值在 Provider 内部拼装。
 */
function OrchContextBridge({
  value,
  children,
}: {
  value: Omit<OrchChatValue, "openTaskPanel">
  children: React.ReactNode
}) {
  const ws = useWorkspace()

  const openTaskPanel = (taskId: number) => {
    const dock = ws.getApi()
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

  return (
    <OrchChatContext.Provider value={{ ...value, openTaskPanel }}>
      {children}
    </OrchChatContext.Provider>
  )
}
