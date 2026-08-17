import { useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link, useParams } from "react-router"

import { useChat } from "@/hooks/use-chat"
import { useDraftSession } from "@/hooks/use-draft-session"
import type { ImageAttachment } from "@/types/acp"
import { fileToImageAttachment } from "@/lib/files"
import { DirPicker } from "@/components/dir-picker"
import {
  ChatPanelContext,
  type ChatPanelData,
} from "@/components/workspace/chat-panel-context"
import { WorkspaceDock } from "@/components/workspace/workspace-dock"
import {
  WorkspaceAutoRefresh,
  WorkspaceProvider,
  WorkspaceAskSink,
  WorkspaceReferenceSink,
} from "@/components/workspace/workspace-provider"
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
import { MessagesSquareIcon } from "lucide-react"

/**
 * 会话工作区宿主页（adr-002）：页面层持有会话流与草稿状态，
 * 面板经 dockview 编排；对话面板通过 ChatPanelContext 消费这里的状态，
 * 被拖动重挂载也不丢流。
 */
export function SessionChat() {
  const { t } = useTranslation()
  const params = useParams()
  // /sessions/new 没有 :id —— 草稿态：会话未创建，首条消息落地才建。
  const isNew = params.id === undefined
  const sessionId = isNew ? 0 : Number(params.id)

  const chat = useChat(sessionId)
  const newSession = useDraftSession(isNew, t("sessions.form.defaultModel"))
  const [draft, setDraft] = useState("")

  // 待发送附件：图片（粘贴/选择）与 @ 引用的文件路径。
  const [images, setImages] = useState<ImageAttachment[]>([])
  const [files, setFiles] = useState<string[]>([])
  const [filePickerOpen, setFilePickerOpen] = useState(false)
  const [cwdPickerOpen, setCwdPickerOpen] = useState(false)
  const imageInputRef = useRef<HTMLInputElement>(null)

  // 草稿态**不预设**工作目录：没选就显示占位文案，由后端落到工作区根。
  // 以前会回落到 agent 记录里的 cwd——那是历史残留，不该替用户决定他
  // 这次要在哪儿干活。
  const draftCwd = newSession.cwd.trim() || t("sessions.form.cwdPlaceholder")

  async function addImages(picked: File[]) {
    const attachments = await Promise.all(picked.map(fileToImageAttachment))
    setImages((prev) => [...prev, ...attachments])
  }

  function submit() {
    const content = draft.trim()
    if (!content && images.length === 0 && files.length === 0) return
    const input = { content, images, files }
    if (isNew) {
      if (!newSession.selected || newSession.creating) return
      // 组件跨 /sessions/new → /sessions/:id 复用不重挂，不清会残留到会话页。
      setDraft("")
      setImages([])
      setFiles([])
      void newSession.start(input)
      return
    }
    setDraft("")
    setImages([])
    setFiles([])
    // 一轮进行中：插话不直接发，先排队浮在输入框上方，轮次结束自动发出；
    // 排队条上可「调整方向」立即插入当前轮，或撤回回填输入框。
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
    if (qImages?.length) {
      setImages((prev) => [...prev, ...qImages])
    }
    if (qFiles?.length) {
      setFiles((prev) => [...prev, ...qFiles.filter((f) => !prev.includes(f))])
    }
  }

  /** 「调整方向」：把排队插话立即发出，插进正在跑的轮（steering）。 */
  function steerQueued(id: number) {
    const item = chat.queued.find((q) => q.id === id)
    if (!item) return
    chat.removeQueued(id)
    void chat.send(item.input)
  }

  /** 空态建议芯片：草稿态直接开会话，老会话正常发。 */
  function sendSuggestion(text: string) {
    if (isNew) {
      void newSession.start({ content: text })
    } else {
      void chat.send({ content: text })
    }
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

  // 会话已被删除或 id 无效：给明确的出口，而不是报"连不上 agent"。
  if (chat.notFound) {
    return (
      <Empty className="h-full justify-center">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <MessagesSquareIcon />
          </EmptyMedia>
          <EmptyTitle>{t("errors.sessionNotFound")}</EmptyTitle>
          <EmptyDescription>{t("errors.sessionNotFoundHint")}</EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button render={<Link to="/sessions" />}>
            {t("errors.backToSessions")}
          </Button>
        </EmptyContent>
      </Empty>
    )
  }

  const chatPanelData: ChatPanelData = {
    isNew,
    chat,
    newSession,
    draft,
    setDraft,
    images,
    files,
    removeImage: (i) => setImages((prev) => prev.filter((_, idx) => idx !== i)),
    removeFile: (i) => setFiles((prev) => prev.filter((_, idx) => idx !== i)),
    addImages: (picked) => void addImages(picked),
    submit,
    sendSuggestion,
    recallQueued,
    steerQueued,
    openImagePicker: () => imageInputRef.current?.click(),
    openFilePicker: () => setFilePickerOpen(true),
    openCwdPicker: () => setCwdPickerOpen(true),
    draftCwd,
  }

  return (
    <WorkspaceProvider sessionId={isNew ? 0 : sessionId}>
      <ChatPanelContext.Provider value={chatPanelData}>
        <div className="min-h-0 flex-1 p-1.5">
          <WorkspaceDock />
        </div>
      </ChatPanelContext.Provider>
      {/* turn 结束刷新 git/文件树——agent 改完文件的时刻。 */}
      <WorkspaceAutoRefresh busy={chat.busy} />
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
        initialPath={
          (isNew ? newSession.cwd.trim() : chat.session?.cwd) || undefined
        }
        onSelect={(path) =>
          setFiles((prev) => (prev.includes(path) ? prev : [...prev, path]))
        }
      />

      {/* 草稿态：点状态栏里的工作目录换目录。 */}
      <DirPicker
        open={cwdPickerOpen}
        onOpenChange={setCwdPickerOpen}
        // 没选过就不给起点：后端从工作区根开始（owner 是设置里的那个，
        // 访客是自己的目录），而不是从 agent 记录里的历史 cwd。
        initialPath={newSession.cwd.trim() || undefined}
        onSelect={newSession.setCwd}
      />
    </WorkspaceProvider>
  )
}
